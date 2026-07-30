package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	tokenSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
)

// Refresh validates a refresh token, rotates it, and returns a new access token
// and refresh token.
func (s *userService) Refresh(ctx context.Context, refreshToken string) (*response.RefreshResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	claims, err := s.tokenService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}

	tokenHash := tokenSvc.HashToken(refreshToken)
	storedToken, err := s.refreshTokenRepository.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("verifying refresh token: %w", err)
	}
	if storedToken == nil {
		// The signature is valid but the token is not on record, so it was
		// already rotated or revoked. A second use of a rotated token means the
		// token was likely captured, so drop the whole family and force re-login.
		if err := s.refreshTokenRepository.DeleteByUserID(ctx, claims.UserID); err != nil {
			return nil, fmt.Errorf("revoking reused refresh token family: %w", err)
		}
		return nil, ErrRefreshTokenRevoked
	}
	if storedToken.ExpiresAt.Before(time.Now()) {
		if err := s.refreshTokenRepository.DeleteByTokenHash(ctx, tokenHash); err != nil {
			return nil, fmt.Errorf("removing expired refresh token: %w", err)
		}
		return nil, ErrRefreshTokenExpired
	}

	accessToken, err := s.tokenService.GenerateAccessToken(claims.UserID, claims.Email, claims.Name)
	if err != nil {
		return nil, fmt.Errorf("generating access token: %w", err)
	}

	// Rotate refresh token — revoke the old one and issue a new one
	if err := s.refreshTokenRepository.DeleteByTokenHash(ctx, tokenHash); err != nil {
		return nil, fmt.Errorf("revoking old refresh token: %w", err)
	}

	newRefreshToken, err := s.tokenService.GenerateRefreshToken(claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("generating refresh token: %w", err)
	}

	if err := s.storeRefreshToken(ctx, claims.UserID, newRefreshToken); err != nil {
		return nil, err
	}

	return &response.RefreshResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
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
		return nil, ErrInvalidUserID
	}

	user, err := s.userRepository.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("finding user by id: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	return &response.GetUser{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
	}, nil
}
