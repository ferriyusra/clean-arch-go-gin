package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
)

// storeRefreshToken persists the hash of a refresh token.
// The raw token is returned to the client but never written to the database.
func (s *userService) storeRefreshToken(ctx context.Context, userID uuid.UUID, rawToken string) error {
	record := entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: token.HashToken(rawToken),
		ExpiresAt: time.Now().Add(s.tokenService.RefreshTokenExpiry()),
	}
	if err := s.refreshTokenRepository.Create(ctx, record); err != nil {
		return fmt.Errorf("storing refresh token: %w", err)
	}
	return nil
}
