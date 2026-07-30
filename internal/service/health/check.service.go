package health

import (
	"context"
	"time"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

// Check performs a basic health check
func (s *healthService) Check(ctx context.Context) (*response.HealthStatus, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return &response.HealthStatus{
		Status:  "ok",
		Message: "Service is healthy",
		Details: map[string]interface{}{
			"timestamp": time.Now().Format(time.RFC3339),
		},
	}, nil
}
