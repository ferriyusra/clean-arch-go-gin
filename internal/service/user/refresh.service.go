package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/google/uuid"
)

// Refresh generates a new access token from a refresh token
func (s *userService) Refresh(ctx context.Context, refreshToken string) (*response.RefreshResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Validate refresh token JWT
	claims, err := s.tokenService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	// Verify refresh token exists in database (not revoked)
	storedToken, err := s.refreshTokenRepository.FindByToken(ctx, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("verifying refresh token: %w", err)
	}
	if storedToken == nil {
		return nil, errors.New("refresh token has been revoked")
	}
	if storedToken.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("refresh token has expired")
	}

	// Generate new access token
	accessToken, err := s.tokenService.GenerateAccessToken(claims.UserID, claims.Email, claims.Name)
	if err != nil {
		return nil, fmt.Errorf("generating access token: %w", err)
	}

	return &response.RefreshResponse{
		Message: accessToken,
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
		return nil, errors.New("invalid user id")
	}

	user, err := s.userRepository.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	return &response.GetUser{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
	}, nil
}
