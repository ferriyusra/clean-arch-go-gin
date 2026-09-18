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
// To introduce versioning later, change the base group to r.Group("/api/v1")
// and keep this function's body untouched.
func SetupRoutes(
	r *gin.Engine,
	messageHandler *handler.MessageHandler,
	counterHandler *handler.CounterHandler,
	userHandler *handler.UserHandler,
	tokenService token.TokenService,
	csrfService csrf.CSRFService,
) {
	api := r.Group("/api")

	// Public routes (no authentication required)
	api.GET("/message", messageHandler.GetMessage)

	// Auth routes (public)
	api.POST("/auth/register", userHandler.Register)
	api.POST("/auth/login", userHandler.Login)
	api.POST("/auth/refresh", middleware.CSRFMiddleware(csrfService), userHandler.Refresh)
	api.GET("/csrf", userHandler.GetCSRFToken)

	// Protected routes (require authentication)
	protected := api.Group("")
	protected.Use(middleware.AuthMiddleware(tokenService))

	// Auth protected endpoints
	protected.GET("/auth/me", userHandler.GetMe)

	// Logout requires auth + CSRF protection (it's a POST request)
	protected.POST("/auth/logout", middleware.CSRFMiddleware(csrfService), userHandler.Logout)

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
