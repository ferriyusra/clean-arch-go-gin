package user

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

// dummyPasswordHash is a valid bcrypt digest of a value nobody can log in with.
// It exists purely to give the "no such user" path the same cost as a real
// password check, so response timing does not reveal which emails exist.
var dummyPasswordHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// Login authenticates a user, generates auth tokens, and returns the full auth response.
func (s *userService) Login(ctx context.Context, req *request.LoginRequest) (*response.LoginResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	user, err := s.userRepository.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("finding user by email: %w", err)
	}

	if user == nil {
		// Run a throwaway comparison so a missing account takes about as long as
		// a wrong password, instead of answering fast enough to enumerate emails.
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(req.Password))
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword(user.Password, []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	accessToken, err := s.tokenService.GenerateAccessToken(user.ID, user.Email, user.Name)
	if err != nil {
		return nil, fmt.Errorf("generating access token: %w", err)
	}

	refreshToken, err := s.tokenService.GenerateRefreshToken(user.ID)
	if err != nil {
		return nil, fmt.Errorf("generating refresh token: %w", err)
	}

	if err := s.storeRefreshToken(ctx, user.ID, refreshToken); err != nil {
		return nil, err
	}

	return &response.LoginResponse{
		User: response.GetUser{
			ID:    user.ID,
			Email: user.Email,
			Name:  user.Name,
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}
