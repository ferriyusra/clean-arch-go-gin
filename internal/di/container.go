package di

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api"
	"github.com/ferriyusra/clean-arch-go-gin/internal/api/handler"
	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/platform"

	counterRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/counter"
	messageRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/message"
	refreshTokenRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/refresh_token"
	userRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/user"

	counterSvc "github.com/ferriyusra/clean-arch-go-gin/internal/service/counter"
	csrfSvc "github.com/ferriyusra/clean-arch-go-gin/internal/service/csrf"
	healthSvc "github.com/ferriyusra/clean-arch-go-gin/internal/service/health"
	messageSvc "github.com/ferriyusra/clean-arch-go-gin/internal/service/message"
	tokenSvc "github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	userSvc "github.com/ferriyusra/clean-arch-go-gin/internal/service/user"
)

// Dev-mode stand-ins. Config.Validate rejects these paths outright when
// DEV_MODE is false, so they can never reach a real deployment.
const (
	devAccessSecret  = "dev-access-secret-DO-NOT-USE-IN-PRODUCTION"
	devRefreshSecret = "dev-refresh-secret-DO-NOT-USE-IN-PRODUCTION"
	devCSRFSecret    = "dev-csrf-secret-DO-NOT-USE-IN-PRODUCTION"
)

// Container holds all application dependencies
type Container struct {
	Config   *platform.Config
	Router   *gin.Engine
	Services *Services
	Logger   *slog.Logger

	db *gorm.DB
	// ownsDB is false when the database was injected through the config (as
	// tests do), in which case Close must not shut it down.
	ownsDB bool
}

// Services holds all service layer dependencies
type Services struct {
	Message messageSvc.MessageService
	Health  healthSvc.HealthService
	Counter counterSvc.CounterService
	User    userSvc.UserService
	Token   tokenSvc.TokenService
	CSRF    csrfSvc.CSRFService
}

// Handlers holds all HTTP handler dependencies
type Handlers struct {
	Message *handler.MessageHandler
	Health  *handler.HealthHandler
	Counter *handler.CounterHandler
	User    *handler.UserHandler
}

// NewContainer creates and initializes a new dependency container.
//
// It is the single wiring point: config validation, the middleware chain, the
// database, repositories, services, handlers and routes are all assembled here
// and nowhere else.
func NewContainer(cfg *platform.Config, logger *slog.Logger) (*Container, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	accessSecret := orDefault(cfg.Auth.JWTAccessSecret, devAccessSecret)
	refreshSecret := orDefault(cfg.Auth.JWTRefreshSecret, devRefreshSecret)
	csrfSecret := orDefault(cfg.Auth.CSRFSecret, devCSRFSecret)

	// Database: reuse an injected handle when one is supplied (tests do this),
	// otherwise open and own the connection.
	db := cfg.Database.Gorm
	ownsDB := false
	if db == nil {
		var err error
		db, err = platform.InitializeDatabase(cfg)
		if err != nil {
			return nil, fmt.Errorf("initializing database: %w", err)
		}
		cfg.Database.Gorm = db
		ownsDB = true
	}

	if err := platform.Migrate(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	// Repositories
	counterRepository := counterRepo.NewGORMCounterRepository(db)
	messageRepository := messageRepo.NewGORMMessageRepository(db)
	userRepository := userRepo.NewGORMUserRepository(db)
	refreshTokenRepository := refreshTokenRepo.NewGORMRefreshTokenRepository(db)

	// Services
	tokenService := tokenSvc.NewTokenService(tokenSvc.TokenConfig{
		AccessTokenSecret:  accessSecret,
		AccessTokenExpiry:  cfg.Auth.AccessTokenTTL,
		RefreshTokenSecret: refreshSecret,
		RefreshTokenExpiry: cfg.Auth.RefreshTokenTTL,
	})

	services := &Services{
		Message: messageSvc.NewMessageService(messageRepository),
		Health:  healthSvc.NewHealthService(),
		Counter: counterSvc.NewCounterService(counterRepository),
		User: userSvc.NewUserService(
			userRepository,
			refreshTokenRepository,
			tokenService,
			cfg.Auth.RefreshTokenTTL,
		),
		Token: tokenService,
		CSRF:  csrfSvc.NewCSRFService(csrfSecret),
	}

	// Handlers
	handlers := &Handlers{
		Message: handler.NewMessageHandler(services.Message),
		Health: handler.NewHealthHandler(services.Health, handler.DependencyChecks{
			"database": platform.PingCheck(db),
		}),
		Counter: handler.NewCounterHandler(services.Counter),
		User: handler.NewUserHandler(services.User, services.CSRF, handler.CookieConfig{
			AccessTTL:  cfg.Auth.AccessTokenTTL,
			RefreshTTL: cfg.Auth.RefreshTokenTTL,
			Secure:     !cfg.Auth.DevMode,
		}),
	}

	router := newRouter(cfg, logger)
	api.SetupRoutes(router, handlers.Message, handlers.Counter, handlers.User, services.Token, services.CSRF)
	api.SetupHealthRoutes(router, handlers.Health)
	api.SetupFallbacks(router)

	return &Container{
		Config:   cfg,
		Router:   router,
		Services: services,
		Logger:   logger,
		db:       db,
		ownsDB:   ownsDB,
	}, nil
}

// newRouter builds the engine and its middleware chain.
//
// Order is load-bearing: RequestID runs first so everything downstream (the
// recovery handler included) has a correlated logger, and Recovery wraps the
// rest so a panic still produces the standard envelope.
func newRouter(cfg *platform.Config, logger *slog.Logger) *gin.Engine {
	if cfg.Auth.DevMode {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// gin.New rather than gin.Default: the default engine installs gin's own
	// text logger and recovery, both of which are replaced here.
	r := gin.New()

	// An empty list means "trust no proxy", so ClientIP reports the direct peer
	// rather than a spoofable X-Forwarded-For value.
	_ = r.SetTrustedProxies(cfg.Security.TrustedProxies)

	r.Use(middleware.RequestID(logger))
	r.Use(middleware.Recovery())
	r.Use(middleware.AccessLog())
	r.Use(middleware.SecurityHeaders(cfg.Auth.DevMode))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.Auth.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-CSRF-Token", middleware.RequestIDHeader},
		ExposeHeaders:    []string{middleware.RequestIDHeader},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	r.Use(middleware.BodyLimit(cfg.Security.MaxRequestBody))

	if cfg.Security.RateLimitEnabled {
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			RPS:   cfg.Security.RateLimitRPS,
			Burst: cfg.Security.RateLimitBurst,
		}))
	}

	r.Use(middleware.Timeout(cfg.Server.RequestTimeout))

	return r
}

// Close releases resources the container owns. Safe to call more than once.
func (c *Container) Close() error {
	if c == nil || !c.ownsDB {
		return nil
	}
	c.ownsDB = false
	return platform.CloseDatabase(c.db)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
