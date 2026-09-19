package user

import (
	"context"
	"fmt"
	"sync"

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

	// A missing user and a wrong password produce the same error *and* cost the
	// same time. Returning early here would skip bcrypt entirely, and the
	// difference between a ~60ms reply and an instant one is enough to map which
	// addresses are registered, no matter how identical the message is.
	if user == nil {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(req.Password))
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

// dummyPasswordHash is what an unknown email is compared against, purely to
// spend the same work a real comparison would.
//
// It is computed once on first use rather than at init so that process startup
// does not pay for it, and so a failure here cannot panic the program: bcrypt
// cannot realistically fail on a fixed short input at the default cost, and if
// it somehow did, a nil hash makes the comparison return quickly with an error
// rather than taking the service down.
var dummyPasswordHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword(
		[]byte("timing-equalisation-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return nil
	}
	return hash
})
