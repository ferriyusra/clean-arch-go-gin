package health

import (
	"context"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

// HealthService defines the interface for health checks
type HealthService interface {
	Check(ctx context.Context) (*response.HealthStatus, error)
	CheckWithDependencies(ctx context.Context, checks map[string]func(context.Context) error) (*response.HealthStatus, error)
}

// healthService is the concrete implementation of HealthService
type healthService struct{}

// NewHealthService creates a new instance of HealthService
func NewHealthService() HealthService {
	return &healthService{}
}
