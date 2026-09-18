package user

import (
	"context"
	"time"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/interfaces"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/google/uuid"
)

// UserService defines the interface for user operations
type UserService interface {
	Register(ctx context.Context, req *request.RegisterUserRequest) (*response.RegisterResponse, error)
	Login(ctx context.Context, req *request.LoginRequest) (*response.LoginResponse, error)
	Refresh(ctx context.Context, refreshToken string) (*response.RefreshResponse, error)
	GetUser(ctx context.Context, userID string) (*response.GetUser, error)
	StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenStr string, expiresAt time.Time) error
	RevokeRefreshTokens(ctx context.Context, userID uuid.UUID) error
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
