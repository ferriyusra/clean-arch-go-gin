package di

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/api"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/handler"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/middleware"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/platform"
	refreshTokenRepo "github.com/ferriyusra/boilerplate-golang-gin/internal/repository/implementations/refresh_token"
	userRepo "github.com/ferriyusra/boilerplate-golang-gin/internal/repository/implementations/user"
	healthSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/health"
	tokenSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
	userSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/user"
)

// minSecretLength is the shortest JWT signing secret accepted in production.
// HS256 keys shorter than the 256-bit digest weaken the signature.
const minSecretLength = 32

// Dev-mode fallback secrets. They exist so `make dev` works with no .env at all;
// NewContainer refuses to start without real secrets when DEV_MODE is false, and
// also refuses if these exact values are supplied.
//
//nolint:gosec // G101: intentional, well-labelled dev-only placeholders, rejected outside DEV_MODE
const (
	devAccessSecret  = "dev-access-secret-DO-NOT-USE-IN-PRODUCTION"
	devRefreshSecret = "dev-refresh-secret-DO-NOT-USE-IN-PRODUCTION"
)

// Container holds all application dependencies
type Container struct {
	Config   *platform.Config
	Router   *gin.Engine
	Logger   *slog.Logger
	DB       *gorm.DB
	Services *Services
}

// Services holds all service layer dependencies
type Services struct {
	Health healthSvc.HealthService
	User   userSvc.UserService
	Token  tokenSvc.TokenService
}

// Handlers holds all HTTP handler dependencies
type Handlers struct {
	Health *handler.HealthHandler
	User   *handler.UserHandler
}

// NewContainer creates and initializes a new dependency container
func NewContainer(cfg *platform.Config) (*Container, error) {
	accessSecret, refreshSecret, err := resolveSecrets(cfg)
	if err != nil {
		return nil, err
	}

	logger := platform.NewLogger(cfg.Auth.DevMode, cfg.Server.LogLevel)

	db, err := setupDatabase(cfg)
	if err != nil {
		return nil, err
	}

	// Initialize repositories
	userRepository := userRepo.NewGORMUserRepository(db)
	refreshTokenRepository := refreshTokenRepo.NewGORMRefreshTokenRepository(db)

	// Initialize token service
	tokenService := tokenSvc.NewTokenService(tokenSvc.TokenConfig{
		AccessTokenSecret:  accessSecret,
		AccessTokenExpiry:  cfg.Auth.AccessTokenExpiry,
		RefreshTokenSecret: refreshSecret,
		RefreshTokenExpiry: cfg.Auth.RefreshTokenExpiry,
		Issuer:             cfg.Auth.JWTIssuer,
	})

	// Initialize services
	services := &Services{
		Health: healthSvc.NewHealthService(),
		User:   userSvc.NewUserService(userRepository, refreshTokenRepository, tokenService),
		Token:  tokenService,
	}

	// Initialize handlers
	readinessChecks := map[string]func(context.Context) error{
		"database": func(ctx context.Context) error { return platform.PingDatabase(ctx, db) },
	}
	handlers := &Handlers{
		Health: handler.NewHealthHandler(services.Health, readinessChecks),
		User:   handler.NewUserHandler(services.User),
	}

	router, err := setupRouter(cfg, logger)
	if err != nil {
		return nil, err
	}

	api.SetupRoutes(router, api.RouterDeps{
		UserHandler:     handlers.User,
		HealthHandler:   handlers.Health,
		TokenService:    services.Token,
		AuthRateLimiter: middleware.NewRateLimiter(cfg.RateLimit.LoginAttempts, cfg.RateLimit.LoginWindow),
	})

	return &Container{
		Config:   cfg,
		Router:   router,
		Logger:   logger,
		DB:       db,
		Services: services,
	}, nil
}

// resolveSecrets validates the JWT secrets, falling back to fixed dev values only
// when DEV_MODE is on. Production must supply its own, long enough to be safe.
func resolveSecrets(cfg *platform.Config) (accessSecret, refreshSecret string, err error) {
	accessSecret = cfg.Auth.JWTAccessSecret
	refreshSecret = cfg.Auth.JWTRefreshSecret

	if cfg.Auth.DevMode {
		if accessSecret == "" {
			accessSecret = devAccessSecret
		}
		if refreshSecret == "" {
			refreshSecret = devRefreshSecret
		}
		return accessSecret, refreshSecret, nil
	}

	if accessSecret == "" || refreshSecret == "" {
		return "", "", fmt.Errorf("JWT_ACCESS_SECRET and JWT_REFRESH_SECRET must be set when DEV_MODE is false")
	}
	if len(accessSecret) < minSecretLength || len(refreshSecret) < minSecretLength {
		return "", "", fmt.Errorf("JWT secrets must be at least %d characters long", minSecretLength)
	}
	if accessSecret == refreshSecret {
		return "", "", fmt.Errorf("JWT_ACCESS_SECRET and JWT_REFRESH_SECRET must differ, otherwise a refresh token is accepted as an access token")
	}
	if accessSecret == devAccessSecret || refreshSecret == devRefreshSecret {
		return "", "", fmt.Errorf("refusing to start with the built-in dev JWT secrets when DEV_MODE is false")
	}

	return accessSecret, refreshSecret, nil
}

// setupDatabase reuses a pre-injected handle when present (tests do this),
// otherwise opens a connection and applies migrations.
func setupDatabase(cfg *platform.Config) (*gorm.DB, error) {
	db := cfg.Database.Gorm
	if db == nil {
		var err error
		db, err = platform.InitializeDatabase(cfg)
		if err != nil {
			return nil, fmt.Errorf("initializing database: %w", err)
		}
		cfg.Database.Gorm = db
	}

	if cfg.Database.AutoMigrate {
		if err := platform.Migrate(db); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// setupRouter builds the Gin engine and the global middleware chain.
func setupRouter(cfg *platform.Config, logger *slog.Logger) (*gin.Engine, error) {
	if cfg.Auth.DevMode {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// gin.New rather than gin.Default: the default logger writes unstructured
	// lines, and we install our own slog-based logger and recovery below.
	r := gin.New()

	// Without this Gin trusts every proxy, letting any caller set X-Forwarded-For
	// and present whatever client IP it likes to the rate limiter.
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		return nil, fmt.Errorf("setting trusted proxies: %w", err)
	}

	handler.RegisterValidationTagNames()

	r.Use(middleware.RequestID())
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Recovery(logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins: cfg.Auth.AllowedOrigins,
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Content-Type", "Authorization", middleware.RequestIDHeader},
		ExposeHeaders: []string{
			middleware.RequestIDHeader,
		},
		// Tokens travel in the Authorization header, not cookies, so the browser
		// never needs to send credentials cross-origin.
		AllowCredentials: false,
	}))

	return r, nil
}
