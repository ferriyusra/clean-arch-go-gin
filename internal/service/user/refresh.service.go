package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// Refresh generates a new access token from a refresh token.
//
// The refresh token must be both a valid JWT and a live row in the database;
// the row is what makes revocation (logout) possible.
func (s *userService) Refresh(ctx context.Context, refreshToken string) (*response.RefreshResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Validate refresh token JWT
	claims, err := s.tokenService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, apperr.ErrInvalidRefreshToken.WithCause(err)
	}

	// Verify refresh token exists in database (not revoked)
	storedToken, err := s.refreshTokenRepository.FindByToken(ctx, refreshToken)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("verifying refresh token: %w", err))
	}
	if storedToken == nil {
		return nil, apperr.ErrRefreshTokenRevoked
	}
	if storedToken.ExpiresAt.Before(time.Now()) {
		return nil, apperr.ErrRefreshTokenExpired
	}

	// Generate new access token
	accessToken, err := s.tokenService.GenerateAccessToken(claims.UserID, claims.Email, claims.Name)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("generating access token: %w", err))
	}

	return &response.RefreshResponse{
		AccessToken: accessToken,
	}, nil
}

// GetUser retrieves user information by ID
func (s *userService) GetUser(ctx context.Context, userID string) (*response.GetUser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, apperr.ErrInvalidUserID.WithCause(err)
	}

	user, err := s.userRepository.FindByID(ctx, id)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("finding user by id: %w", err))
	}
	if user == nil {
		return nil, apperr.ErrUserNotFound
	}

	return &response.GetUser{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
	}, nil
}
