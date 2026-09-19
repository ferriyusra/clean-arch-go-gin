package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
)

// Refresh generates a new access token from a refresh token.
//
// The refresh token must be both a valid JWT and a live row in the database;
// the row is what makes revocation (logout) possible.
// Refresh exchanges a refresh token for a new token pair.
//
// The presented token must be a valid JWT *and* have a live row in the
// database. The row is what makes revocation possible, and rotating it on every
// use is what bounds the damage of a stolen token.
func (s *userService) Refresh(ctx context.Context, refreshToken string) (*response.RefreshResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	claims, err := s.tokenService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, apperr.ErrInvalidRefreshToken.WithCause(err)
	}

	tokenHash := token.Hash(refreshToken)

	stored, err := s.refreshTokenRepository.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("verifying refresh token: %w", err))
	}

	if stored == nil {
		return nil, s.handleReuse(ctx, claims.UserID)
	}

	if stored.ExpiresAt.Before(s.now()) {
		// The row is dead weight either way, so drop it rather than leave it
		// for the janitor.
		if delErr := s.refreshTokenRepository.DeleteByTokenHash(ctx, tokenHash); delErr != nil {
			logging.FromContext(ctx).Warn("removing expired refresh token",
				"user_id", stored.UserID.String(), "error", delErr.Error())
		}
		return nil, apperr.ErrRefreshTokenExpired
	}

	// Rotate. The old token is invalidated before the new one is issued, so a
	// crash in between costs the user a re-login rather than leaving two live
	// tokens for one session.
	if err := s.refreshTokenRepository.DeleteByTokenHash(ctx, tokenHash); err != nil {
		return nil, apperr.Internal(fmt.Errorf("rotating refresh token: %w", err))
	}

	accessToken, newRefreshToken, err := s.issueTokens(ctx, response.GetUser{
		ID:    claims.UserID,
		Email: claims.Email,
		Name:  claims.Name,
	})
	if err != nil {
		return nil, err
	}

	return &response.RefreshResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
	}, nil
}

// handleReuse deals with a refresh token that verifies as a JWT but has no row.
//
// There are two ways to get here: the token was revoked by a logout, or it was
// already rotated and this is a replay. They are indistinguishable from here,
// and a replay is the dangerous one, so both are treated as theft and every
// session for the user is ended.
//
// The cost of the false positive is real but small: a client that fires two
// refreshes with the same token, or retries after a logout, gets logged out and
// signs in again. The cost of the false negative is an attacker keeping a
// stolen session indefinitely.
func (s *userService) handleReuse(ctx context.Context, userID uuid.UUID) error {
	log := logging.FromContext(ctx)
	log.Warn("refresh token reuse detected, revoking every session for the user",
		"user_id", userID.String())

	if err := s.RevokeRefreshTokens(ctx, userID); err != nil {
		// Report the original condition to the client either way: the token is
		// refused regardless of whether the cleanup succeeded.
		log.Error("revoking token family after refresh token reuse",
			"user_id", userID.String(), "error", err.Error())
	}

	return apperr.ErrRefreshTokenRevoked
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
