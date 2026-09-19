package di

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api"
	"github.com/ferriyusra/clean-arch-go-gin/internal/api/handler"
	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/observability"
	"github.com/ferriyusra/clean-arch-go-gin/internal/platform"
	"github.com/ferriyusra/clean-arch-go-gin/internal/tracing"

	counterRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/counter"
	messageRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/message"
	refreshTokenRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/refresh_token"
	txRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/tx"
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

// tracerShutdownTimeout bounds the final span flush so a wedged collector
// cannot hold up process exit.
const tracerShutdownTimeout = 5 * time.Second

// adminShutdownTimeout bounds the admin listener's drain. It is short on
// purpose: an in-flight CPU profile can run for 30s, and nobody's deploy should
// wait on one.
const adminShutdownTimeout = 2 * time.Second

// Container holds all application dependencies
type Container struct {
	Config   *platform.Config
	Router   *gin.Engine
	Services *Services
	Logger   *slog.Logger

	// AdminServer serves /metrics and /debug/pprof on their own listener,
	// separate from the public router. It is nil when both are disabled, which
	// is the default, so every use site has to nil-check it.
	AdminServer *http.Server

	db             *gorm.DB
	tracerShutdown tracing.ShutdownFunc
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

	// Tracing is installed before the router so that otelgin picks up the real
	// tracer provider rather than the no-op one it would capture otherwise.
	tracerShutdown, err := tracing.Init(context.Background(), tracing.Config{
		Enabled:        cfg.Tracing.Enabled,
		ServiceName:    cfg.Tracing.ServiceName,
		ServiceVersion: cfg.Tracing.ServiceVersion,
		Environment:    cfg.Tracing.Environment,
		Exporter:       cfg.Tracing.Exporter,
		Endpoint:       cfg.Tracing.Endpoint,
		Insecure:       cfg.Tracing.Insecure,
		SampleRatio:    cfg.Tracing.SampleRatio,
	})
	if err != nil {
		return nil, fmt.Errorf("initializing tracing: %w", err)
	}

	accessSecret := orDefault(cfg.Auth.JWTAccessSecret, devAccessSecret)
	refreshSecret := orDefault(cfg.Auth.JWTRefreshSecret, devRefreshSecret)
	csrfSecret := orDefault(cfg.Auth.CSRFSecret, devCSRFSecret)

	// Database: reuse an injected handle when one is supplied (tests do this),
	// otherwise open and own the connection.
	db := cfg.Database.Gorm
	ownsDB := false
	if db == nil {
		var dbErr error
		db, dbErr = platform.InitializeDatabase(cfg)
		if dbErr != nil {
			return nil, fmt.Errorf("initializing database: %w", dbErr)
		}
		cfg.Database.Gorm = db
		ownsDB = true
	}

	if cfg.Database.AutoMigrate {
		if err := platform.Migrate(db); err != nil {
			return nil, fmt.Errorf("migrating database: %w", err)
		}
	} else {
		logger.Info("startup migrations are disabled; run them as a separate step")
	}

	// Repositories
	counterRepository := counterRepo.NewGORMCounterRepository(db)
	messageRepository := messageRepo.NewGORMMessageRepository(db)
	userRepository := userRepo.NewGORMUserRepository(db)
	refreshTokenRepository := refreshTokenRepo.NewGORMRefreshTokenRepository(db)
	txManager := txRepo.NewGORMTxManager(db)

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
			txManager,
			cfg.Auth.RefreshTokenTTL,
		),
		Token: tokenService,
		CSRF:  csrfSvc.NewCSRFService(csrfSecret, cfg.Auth.CSRFTokenTTL),
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

	// Metrics are only collected when they are going to be served; building the
	// registry regardless would install the Go and process collectors for
	// nothing.
	var metrics *observability.Metrics
	if cfg.Observability.MetricsEnabled {
		metrics = observability.NewMetrics()
	}

	router := newRouter(cfg, logger, metrics)
	api.SetupRoutes(router, handlers.Message, handlers.Counter, handlers.User,
		services.Token, services.CSRF, authRateLimiter(cfg))
	api.SetupHealthRoutes(router, handlers.Health)
	api.SetupFallbacks(router)

	// The admin listener is deliberately not part of the public router: /metrics
	// publishes operational detail and pprof hands out heap dumps, so neither
	// should be reachable wherever the API is. Config.Validate already refuses
	// to bind pprof to a non-loopback address outside DEV_MODE.
	adminServer := observability.NewAdminServer(observability.Config{
		MetricsEnabled: cfg.Observability.MetricsEnabled,
		PprofEnabled:   cfg.Observability.PprofEnabled,
		Host:           cfg.Observability.AdminHost,
		Port:           cfg.Observability.AdminPort,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		IdleTimeout:    cfg.Server.IdleTimeout,
	}, metrics)

	return &Container{
		Config:         cfg,
		Router:         router,
		Services:       services,
		Logger:         logger,
		AdminServer:    adminServer,
		db:             db,
		ownsDB:         ownsDB,
		tracerShutdown: tracerShutdown,
	}, nil
}

// newRouter builds the engine and its middleware chain.
//
// Order is load-bearing: RequestID runs first so everything downstream (the
// recovery handler included) has a correlated logger, and Recovery wraps the
// rest so a panic still produces the standard envelope.
func newRouter(cfg *platform.Config, logger *slog.Logger, metrics *observability.Metrics) *gin.Engine {
	if cfg.Auth.DevMode {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// gin.New rather than gin.Default: the default engine installs gin's own
	// text logger and recovery, both of which are replaced here.
	r := gin.New()

	// gin leaves this off, which quietly makes the NoMethod handler dead code:
	// a DELETE to a GET-only path would answer 404 "Route not found" instead of
	// 405, telling a client the path does not exist when the problem is the verb.
	r.HandleMethodNotAllowed = true

	// An empty list means "trust no proxy", so ClientIP reports the direct peer
	// rather than a spoofable X-Forwarded-For value.
	_ = r.SetTrustedProxies(cfg.Security.TrustedProxies)

	// Tracing goes first: the span has to exist before RequestID can adopt its
	// trace id, and before any later middleware can be attributed to it.
	if cfg.Tracing.Enabled {
		r.Use(otelgin.Middleware(cfg.Tracing.ServiceName,
			otelgin.WithFilter(func(req *http.Request) bool {
				// Health probes run every few seconds and would bury the real
				// traffic in the trace store.
				return !strings.HasPrefix(req.URL.Path, "/api/health")
			}),
		))
	}

	r.Use(middleware.RequestID(logger))

	// Metrics go OUTSIDE Recovery, not inside it. A panic unwinds through every
	// c.Next() above it, and the middleware records after its c.Next() with no
	// defer, so from inside Recovery a panicked request would be counted zero
	// times — the metrics would be blind to the worst failure the service has.
	// From outside, Recovery returns normally and the real 500 is observed.
	//
	// A defer inside the middleware would not fix it: the defer would run during
	// unwinding, before Recovery writes the 500, and record a 200.
	if metrics != nil {
		r.Use(metrics.Middleware())
	}

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
		r.Use(middleware.RateLimit(middleware.NewIPRateLimiter(middleware.RateLimitConfig{
			RPS:   cfg.Security.RateLimitRPS,
			Burst: cfg.Security.RateLimitBurst,
		})))
	}

	r.Use(middleware.Timeout(cfg.Server.RequestTimeout))

	return r
}

// Close releases resources the container owns. Safe to call more than once.
// Close releases the resources the container owns. Safe to call more than once.
//
// The tracer is flushed before anything else: spans are batched, so exiting
// without this drops whatever has not been exported yet, which usually includes
// the spans for the requests that prompted the shutdown.
func (c *Container) Close() error {
	if c == nil {
		return nil
	}

	var errs []error

	// The admin listener goes first: it is the least important thing running and
	// the most likely to be holding an open scrape or a 30-second profile.
	if c.AdminServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), adminShutdownTimeout)
		err := c.AdminServer.Shutdown(ctx)
		cancel()

		// The field is deliberately NOT set to nil: StartAdmin's goroutine may
		// still be running, and writing here while it reads would be a data race.
		// Shutdown is already idempotent, so a second Close is harmless.
		//
		// A deadline overrun is the expected case, not a fault: the timeout is
		// short on purpose so a deploy never waits on an in-flight 30s profile.
		// Reporting it as an error would put a scary line in the logs of every
		// normal shutdown that happened to overlap a scrape.
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			errs = append(errs, fmt.Errorf("shutting down the admin listener: %w", err))
		}
	}

	if c.tracerShutdown != nil {
		ctx, cancel := context.WithTimeout(context.Background(), tracerShutdownTimeout)
		defer cancel()

		if err := c.tracerShutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutting down tracing: %w", err))
		}
		c.tracerShutdown = nil
	}

	// An injected database belongs to whoever injected it, so it is left alone.
	if c.ownsDB {
		c.ownsDB = false
		if err := platform.CloseDatabase(c.db); err != nil {
			errs = append(errs, fmt.Errorf("closing database: %w", err))
		}
	}

	return errors.Join(errs...)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// StartJanitor begins the background sweep of expired refresh tokens and
// returns immediately.
//
// The goroutine exits when ctx is cancelled, which is what the shutdown signal
// does, so it needs no separate stop channel and cannot outlive the process.
func (c *Container) StartJanitor(ctx context.Context) {
	interval := c.Config.Auth.RefreshTokenPurgeInterval
	if interval <= 0 {
		c.Logger.Info("expired refresh token sweep is disabled")
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			// Sweeping on start as well as on the tick matters: a service that
			// is redeployed more often than the interval would otherwise never
			// get around to it.
			c.purgeExpiredRefreshTokens(ctx)

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (c *Container) purgeExpiredRefreshTokens(ctx context.Context) {
	removed, err := c.Services.User.PurgeExpiredRefreshTokens(ctx)
	if err != nil {
		// A failed sweep is not worth taking the service down for; the rows are
		// inert either way and the next tick will try again.
		c.Logger.Error("sweeping expired refresh tokens", "error", err.Error())
		return
	}
	if removed > 0 {
		c.Logger.Info("swept expired refresh tokens", "removed", removed)
	}
}

// authRateLimiter builds the throttle for the credential endpoints.
//
// It is a separate limiter from the global one, with its own budget, so that
// normal API traffic cannot use up the allowance that is meant to make password
// guessing slow. When rate limiting is switched off entirely, this becomes a
// pass-through rather than a second policy that quietly stays on.
func authRateLimiter(cfg *platform.Config) gin.HandlerFunc {
	if !cfg.Security.RateLimitEnabled {
		return func(c *gin.Context) { c.Next() }
	}

	return middleware.RateLimit(middleware.NewIPRateLimiter(middleware.RateLimitConfig{
		RPS:   cfg.Security.AuthRateLimitRPS,
		Burst: cfg.Security.AuthRateLimitBurst,
	}))
}

// StartAdmin begins serving the metrics and pprof listener and returns
// immediately. It is a no-op when neither signal is enabled.
//
// A failure here is logged, not fatal: the admin listener is a diagnostic aid,
// and refusing to serve traffic because a metrics port is already taken would
// turn an observability problem into an outage. Close shuts it down.
func (c *Container) StartAdmin() {
	if c.AdminServer == nil {
		c.Logger.Debug("metrics and pprof are disabled; no admin listener")
		return
	}

	// Everything the goroutine needs is captured here rather than read from c
	// inside it. Close runs concurrently with this goroutine, and reading
	// c.AdminServer there would be a data race — and worse, if Close won, a nil
	// dereference that panics the process during shutdown.
	srv := c.AdminServer
	logger := c.Logger
	addr := srv.Addr
	metricsOn := c.Config.Observability.MetricsEnabled
	pprofOn := c.Config.Observability.PprofEnabled

	go func() {
		logger.Info("admin listener starting",
			"addr", addr,
			"metrics", metricsOn,
			"pprof", pprofOn,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("admin listener", "addr", addr, "error", err.Error())
		}
	}()
}
