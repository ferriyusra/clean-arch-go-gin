package api

import (
	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/handler"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/middleware"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
)

// RouterDeps holds everything SetupRoutes needs to register the API.
type RouterDeps struct {
	UserHandler   *handler.UserHandler
	HealthHandler *handler.HealthHandler
	TokenService  token.TokenService
	// AuthRateLimiter throttles the unauthenticated auth endpoints per client IP.
	AuthRateLimiter *middleware.RateLimiter
}

// SetupRoutes configures all API routes
func SetupRoutes(r *gin.Engine, deps RouterDeps) {
	api := r.Group("/api")

	// Health probes — no auth, no rate limit, so orchestrators can always poll.
	api.GET("/health", deps.HealthHandler.Check)
	api.GET("/health/ready", deps.HealthHandler.Ready)

	// Auth routes (public). Rate limited because they accept credentials and are
	// the obvious target for brute-force and credential-stuffing attempts.
	auth := api.Group("/auth")
	auth.Use(deps.AuthRateLimiter.Middleware())
	auth.POST("/register", deps.UserHandler.Register)
	auth.POST("/login", deps.UserHandler.Login)
	auth.POST("/refresh", deps.UserHandler.Refresh)

	// Protected routes (require a valid access token)
	protected := api.Group("/auth")
	protected.Use(middleware.AuthMiddleware(deps.TokenService))
	protected.GET("/me", deps.UserHandler.GetMe)
	protected.POST("/logout", deps.UserHandler.Logout)
}
