package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/health"
)

// DependencyChecks maps a dependency name to its probe.
type DependencyChecks map[string]func(context.Context) error

// HealthHandler handles health check requests.
//
// Liveness and readiness are separate on purpose: an orchestrator must be able
// to tell "the process is wedged, restart it" apart from "the process is fine
// but its database is briefly unreachable, stop routing traffic here".
type HealthHandler struct {
	service health.HealthService
	checks  DependencyChecks
}

// NewHealthHandler creates a new instance of HealthHandler
func NewHealthHandler(svc health.HealthService, checks DependencyChecks) *HealthHandler {
	return &HealthHandler{
		service: svc,
		checks:  checks,
	}
}

// Check handles GET /api/health/live: is the process running at all?
//
// It deliberately touches no dependency, so a database blip cannot trigger a
// restart loop.
func (h *HealthHandler) Check(c *gin.Context) {
	status, err := h.service.Check(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}

	OK(c, http.StatusOK, "Service is healthy", status)
}

// Ready handles GET /api/health and GET /api/health/ready: can this instance
// serve traffic right now? Responds 503 when a dependency is down.
func (h *HealthHandler) Ready(c *gin.Context) {
	status, err := h.service.CheckWithDependencies(c.Request.Context(), h.checks)
	if err != nil {
		Fail(c, err)
		return
	}

	if status.Status != "ok" {
		// This 503 is written directly rather than through Fail, because the
		// body carries the per-dependency detail an operator needs and Fail
		// deliberately reduces an error to a message. It still gets the
		// correlation ids: a failing readiness probe is exactly when someone
		// wants to find the matching log line and trace.
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, response.APIResponse{
			Success: false,
			Message: status.Message,
			Data:    status,
		}.WithCorrelation(middleware.GetRequestID(c), middleware.GetTraceID(c)))
		return
	}

	OK(c, http.StatusOK, "Service is ready", status)
}
