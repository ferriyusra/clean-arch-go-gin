package user

import (
	"context"

	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/repository/interfaces"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
)

// UserService defines the interface for user operations
type UserService interface {
	Register(ctx context.Context, req *request.RegisterUserRequest) (*response.RegisterResponse, error)
	Login(ctx context.Context, req *request.LoginRequest) (*response.LoginResponse, error)
	Refresh(ctx context.Context, refreshToken string) (*response.RefreshResponse, error)
	GetUser(ctx context.Context, userID string) (*response.GetUser, error)
	RevokeRefreshTokens(ctx context.Context, userID uuid.UUID) error
	PurgeExpiredRefreshTokens(ctx context.Context) (int64, error)
}

// userService is the concrete implementation of UserService
type userService struct {
	userRepository         interfaces.UserRepository
	refreshTokenRepository interfaces.RefreshTokenRepository
	tokenService           token.TokenService
}

// NewUserService creates a new instance of UserService
func NewUserService(
	userRepository interfaces.UserRepository,
	refreshTokenRepository interfaces.RefreshTokenRepository,
	tokenService token.TokenService,
) UserService {
	return &userService{
		userRepository:         userRepository,
		refreshTokenRepository: refreshTokenRepository,
		tokenService:           tokenService,
	}
}
