package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

// Register creates a new user account and returns user info.
func (s *userService) Register(ctx context.Context, req *request.RegisterUserRequest) (*response.RegisterResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	existingUser, err := s.userRepository.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("checking existing user: %w", err)
	}
	if existingUser != nil {
		return nil, ErrEmailAlreadyRegistered
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	userEntity := entity.UserEntity{
		ID:       uuid.New(),
		Email:    req.Email,
		Password: hashedPassword,
		Name:     req.Name,
	}

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
