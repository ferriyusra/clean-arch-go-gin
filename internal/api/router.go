package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/handler"
	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/csrf"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
)

// SetupRoutes configures all API routes.
//
// Everything hangs off /api/v1. There are deliberately no /api/... aliases
// alongside it: this service has no external consumers yet, so the entire cost
// of the move is a one-line change to the base URL in the client, while running
// two live surfaces is a maintenance tax paid on every route, forever — two
// paths to test, two to document, and a slow drift as someone fixes one and
// forgets the other. A compatibility layer is worth adding the day there is
// something to be compatible with.
func SetupRoutes(
	r *gin.Engine,
	messageHandler *handler.MessageHandler,
	counterHandler *handler.CounterHandler,
	userHandler *handler.UserHandler,
	tokenService token.TokenService,
	csrfService csrf.CSRFService,
	// authLimiter guards the credential endpoints. It is passed in rather than
	// built here so the policy stays with the rest of the configuration.
	authLimiter gin.HandlerFunc,
) {
	api := r.Group("/api/v1")

	// Public routes (no authentication required)
	api.GET("/message", messageHandler.GetMessage)

	// Auth routes (public)
	// Credential endpoints carry their own, much tighter budget: a global limit
	// generous enough for normal browsing is generous enough to guess passwords.
	api.POST("/auth/register", authLimiter, userHandler.Register)
	api.POST("/auth/login", authLimiter, userHandler.Login)
	api.POST("/auth/refresh", authLimiter, middleware.CSRFMiddleware(csrfService), userHandler.Refresh)
	api.GET("/csrf", userHandler.GetCSRFToken)

	// Protected routes (require authentication)
	protected := api.Group("")
	protected.Use(middleware.AuthMiddleware(tokenService))

	// Auth protected endpoints
	protected.GET("/auth/me", userHandler.GetMe)

	// Listing sessions is a read, so it carries no CSRF middleware: the token
	// exists to stop a third-party page performing an action as the user, and
	// there is no action here to perform.
	protected.GET("/auth/sessions", userHandler.ListSessions)

	// Logout requires auth + CSRF protection (it's a POST request)
	protected.POST("/auth/logout", middleware.CSRFMiddleware(csrfService), userHandler.Logout)

	// Account lifecycle. Both change state, so both take CSRF.
	protected.PATCH("/auth/password", middleware.CSRFMiddleware(csrfService), userHandler.ChangePassword)
	protected.DELETE("/auth/me", middleware.CSRFMiddleware(csrfService), userHandler.DeleteAccount)

	// Counter endpoints (protected)
	protected.GET("/counter", counterHandler.GetCounter)

	// Counter POST requires auth + CSRF protection
	protected.POST("/counter", middleware.CSRFMiddleware(csrfService), counterHandler.IncrementCounter)
}

// SetupHealthRoutes configures health check routes.
//
// /api/health keeps the original behaviour of the single endpoint (readiness),
// while the explicit /live and /ready paths are what an orchestrator should be
// pointed at.
//
// These stay UNVERSIONED while everything else moved to /api/v1, and the
// asymmetry is deliberate rather than an oversight. A health endpoint is a
// contract with the orchestrator, not with an API client: it is wired into
// liveness and readiness probes, load balancer checks and uptime monitors, all
// of which live outside this repository. Versioning it would mean that
// releasing /api/v2 requires editing every one of those, in lockstep, or the
// platform starts killing healthy pods. The endpoints also have no payload to
// evolve — a status and a dependency map — so there is nothing a version would
// buy in return.
func SetupHealthRoutes(r *gin.Engine, healthHandler *handler.HealthHandler) {
	api := r.Group("/api")
	api.GET("/health", healthHandler.Ready)
	api.GET("/health/live", healthHandler.Check)
	api.GET("/health/ready", healthHandler.Ready)
}

// SetupFallbacks makes unmatched routes and methods return the same envelope as
// every other response, instead of gin's bare 404/405 with an empty body.
func SetupFallbacks(r *gin.Engine) {
	r.NoRoute(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusNotFound, response.Err("Route not found").
			WithCorrelation(middleware.GetRequestID(c), middleware.GetTraceID(c)))
	})
	r.NoMethod(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusMethodNotAllowed, response.Err("Method not allowed").
			WithCorrelation(middleware.GetRequestID(c), middleware.GetTraceID(c)))
	})
}
