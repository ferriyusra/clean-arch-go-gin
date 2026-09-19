package counter

import (
	"context"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// IncrementCounter increments the counter and returns the new value as a response
func (s *counterService) IncrementCounter(ctx context.Context) (*response.GetCounter, error) {
	value, err := s.repo.IncrementCounter(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("incrementing counter: %w", err))
	}
	return &response.GetCounter{
		Value: value,
	}, nil
}
