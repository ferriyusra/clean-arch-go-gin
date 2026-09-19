package counter

import (
	"context"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// GetCounter returns the current counter value using the response model
func (s *counterService) GetCounter(ctx context.Context) (*response.GetCounter, error) {
	value, err := s.repo.GetCounter(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("reading counter: %w", err))
	}
	return &response.GetCounter{
		Value: value,
	}, nil
}
