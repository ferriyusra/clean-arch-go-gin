package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Register creates a new user account and returns user info (tokens set via cookies in handler)
func (s *userService) Register(ctx context.Context, req *request.RegisterUserRequest) (*response.RegisterResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Check if user already exists
	existingUser, err := s.userRepository.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("checking existing user: %w", err)
	}
	if existingUser != nil {
		return nil, errors.New("user already exists")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	// Create user entity
	userEntity := entity.UserEntity{
		ID:       uuid.New(),
		Email:    req.Email,
		Password: hashedPassword,
		Name:     req.Name,
	}

	// Save to repository
	_, err = s.userRepository.Create(ctx, userEntity)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return &response.RegisterResponse{
		User: response.GetUser{
			ID:    userEntity.ID,
			Email: userEntity.Email,
			Name:  userEntity.Name,
		},
	}, nil
}
