package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/interfaces"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
)

// UserService defines the interface for user operations.
//
// Register and Login mint and persist the token pair themselves: issuing
// credentials is business logic, so it belongs here rather than in the handler,
// whose only job is to move the tokens into cookies.
type UserService interface {
	Register(ctx context.Context, req *request.RegisterUserRequest) (*response.RegisterResponse, error)
	Login(ctx context.Context, req *request.LoginRequest) (*response.LoginResponse, error)
	Refresh(ctx context.Context, refreshToken string) (*response.RefreshResponse, error)
	GetUser(ctx context.Context, userID string) (*response.GetUser, error)
	StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenStr string, expiresAt time.Time) error
	RevokeRefreshTokens(ctx context.Context, userID uuid.UUID) error
	PurgeExpiredRefreshTokens(ctx context.Context) (int64, error)
}

// userService is the concrete implementation of UserService
type userService struct {
	userRepository         interfaces.UserRepository
	refreshTokenRepository interfaces.RefreshTokenRepository
	tokenService           token.TokenService
	txManager              interfaces.TxManager
	refreshTokenTTL        time.Duration
	// now is swappable so tests can reason about expiry without sleeping.
	now func() time.Time
}

// NewUserService creates a new instance of UserService.
//
// refreshTokenTTL must match the refresh token's JWT expiry so the database row
// and the token itself expire together.
func NewUserService(
	userRepository interfaces.UserRepository,
	refreshTokenRepository interfaces.RefreshTokenRepository,
	tokenService token.TokenService,
	txManager interfaces.TxManager,
	refreshTokenTTL time.Duration,
) UserService {
	return &userService{
		userRepository:         userRepository,
		refreshTokenRepository: refreshTokenRepository,
		tokenService:           tokenService,
		txManager:              txManager,
		refreshTokenTTL:        refreshTokenTTL,
		now:                    time.Now,
	}
}

// issueTokens mints an access/refresh pair for user and records the refresh
// token so it can later be revoked.
func (s *userService) issueTokens(ctx context.Context, user response.GetUser) (accessToken, refreshToken string, err error) {
	accessToken, err = s.tokenService.GenerateAccessToken(user.ID, user.Email, user.Name)
	if err != nil {
		return "", "", apperr.Internal(fmt.Errorf("generating access token: %w", err))
	}

	refreshToken, err = s.tokenService.GenerateRefreshToken(user.ID)
	if err != nil {
		return "", "", apperr.Internal(fmt.Errorf("generating refresh token: %w", err))
	}

	expiresAt := s.now().Add(s.refreshTokenTTL)
	if err = s.StoreRefreshToken(ctx, user.ID, refreshToken, expiresAt); err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}
