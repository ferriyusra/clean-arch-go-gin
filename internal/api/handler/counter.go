package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/service/counter"
)

// CounterHandler handles counter-related HTTP requests
type CounterHandler struct {
	service counter.CounterService
}

// NewCounterHandler creates a new instance of CounterHandler
func NewCounterHandler(svc counter.CounterService) *CounterHandler {
	return &CounterHandler{
		service: svc,
	}
}

// GetCounter handles GET /api/v1/counter requests
func (h *CounterHandler) GetCounter(c *gin.Context) {
	value, err := h.service.GetCounter(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}

	OK(c, http.StatusOK, "Counter retrieved", value)
}

// IncrementCounter handles POST /api/v1/counter requests
func (h *CounterHandler) IncrementCounter(c *gin.Context) {
	value, err := h.service.IncrementCounter(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}

	OK(c, http.StatusOK, "Counter incremented", value)
}
