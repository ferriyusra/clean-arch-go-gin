package user

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// Login authenticates a user and issues a fresh token pair.
func (s *userService) Login(ctx context.Context, req *request.LoginRequest) (*response.LoginResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Find user by email
	user, err := s.userRepository.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("finding user by email: %w", err))
	}

	// A missing user and a wrong password deliberately produce the same error,
	// so the response cannot be used to enumerate registered addresses.
	if user == nil {
		return nil, apperr.ErrInvalidCredentials
	}

	if err = bcrypt.CompareHashAndPassword(user.Password, []byte(req.Password)); err != nil {
		return nil, apperr.ErrInvalidCredentials
	}

	authenticated := response.GetUser{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
	}

	accessToken, refreshToken, err := s.issueTokens(ctx, authenticated)
	if err != nil {
		return nil, err
	}

	return &response.LoginResponse{
		User:         authenticated,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}
