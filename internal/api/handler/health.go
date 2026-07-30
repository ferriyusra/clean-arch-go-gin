package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/health"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	service health.HealthService
	checks  map[string]func(context.Context) error
}

// NewHealthHandler creates a new instance of HealthHandler.
//
// checks holds the dependency probes reported by the readiness endpoint (the
// database, and whatever else the service cannot serve traffic without). Pass nil
// to expose liveness only.
func NewHealthHandler(svc health.HealthService, checks map[string]func(context.Context) error) *HealthHandler {
	return &HealthHandler{
		service: svc,
		checks:  checks,
	}
}

// Check handles GET /api/health — a liveness probe that answers as long as the
// process is up. It deliberately touches no dependencies, so an orchestrator does
// not restart the process just because the database is briefly unreachable.
func (h *HealthHandler) Check(c *gin.Context) {
	status, err := h.service.Check(c.Request.Context())
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response.OK("Service is healthy", status))
}

// Ready handles GET /api/health/ready — a readiness probe that verifies every
// dependency and answers 503 when any of them is down, so a broken instance is
// pulled out of the load balancer rotation.
func (h *HealthHandler) Ready(c *gin.Context) {
	status, err := h.service.CheckWithDependencies(c.Request.Context(), h.checks)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	if status.Status != "ok" {
		c.JSON(http.StatusServiceUnavailable, response.APIResponse{
			Success: false,
			Message: status.Message,
			Data:    status,
		})
		return
	}

	c.JSON(http.StatusOK, response.OK(status.Message, status))
}
