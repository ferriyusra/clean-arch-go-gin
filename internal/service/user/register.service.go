package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// Register creates a new user account and issues its first token pair.
func (s *userService) Register(ctx context.Context, req *request.RegisterUserRequest) (*response.RegisterResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Check if user already exists
	existingUser, err := s.userRepository.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("checking existing user: %w", err))
	}
	if existingUser != nil {
		return nil, apperr.ErrUserAlreadyExists
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("hashing password: %w", err))
	}

	// Create user entity
	userEntity := entity.UserEntity{
		ID:       uuid.New(),
		Email:    req.Email,
		Password: hashedPassword,
		Name:     req.Name,
	}

	user := response.GetUser{
		ID:    userEntity.ID,
		Email: userEntity.Email,
		Name:  userEntity.Name,
	}

	// The account row and its first refresh token are written together.
	// Without the transaction, a failure while storing the token left an
	// account that exists but has no session, whose email is already taken by
	// the unique index, so the owner could neither sign in nor register again.
	var accessToken, refreshToken string
	err = s.txManager.WithinTx(ctx, func(ctx context.Context) error {
		if _, createErr := s.userRepository.Create(ctx, userEntity); createErr != nil {
			return apperr.Internal(fmt.Errorf("creating user: %w", createErr))
		}

		var issueErr error
		accessToken, refreshToken, issueErr = s.issueTokens(ctx, user)
		return issueErr
	})
	if err != nil {
		return nil, err
	}

	return &response.RegisterResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}
