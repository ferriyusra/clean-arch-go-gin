package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/mock"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func TestRefresh(t *testing.T) {
	testUserID := uuid.New()
	tokenConfig := token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}
	tokenSvc := token.NewTokenService(tokenConfig)

	// Generate a valid refresh token for testing
	validRefreshToken, _ := tokenSvc.GenerateRefreshToken(testUserID)

	tests := []struct {
		name               string
		refreshToken       string
		mockFindByToken    *entity.RefreshTokenEntity
		mockFindByTokenErr error
		setupMock          bool
		expectedError      bool
		expectedErrorMsg   string
	}{
		{
			name:         "should refresh token successfully",
			refreshToken: validRefreshToken,
			mockFindByToken: &entity.RefreshTokenEntity{
				ID:        uuid.New(),
				UserID:    testUserID,
				Token:     validRefreshToken,
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			},
			mockFindByTokenErr: nil,
			setupMock:          true,
			expectedError:      false,
		},
		{
			name:             "should return error when refresh token is invalid JWT",
			refreshToken:     "invalid-token",
			setupMock:        false,
			expectedError:    true,
			expectedErrorMsg: "invalid refresh token",
		},
		{
			name:             "should return error when refresh token is empty",
			refreshToken:     "",
			setupMock:        false,
			expectedError:    true,
			expectedErrorMsg: "invalid refresh token",
		},
		{
			name:               "should return error when refresh token not found in DB (revoked)",
			refreshToken:       validRefreshToken,
			mockFindByToken:    nil,
			mockFindByTokenErr: nil,
			setupMock:          true,
			expectedError:      true,
			expectedErrorMsg:   "refresh token has been revoked",
		},
		{
			name:         "should return error when refresh token is expired in DB",
			refreshToken: validRefreshToken,
			mockFindByToken: &entity.RefreshTokenEntity{
				ID:        uuid.New(),
				UserID:    testUserID,
				Token:     validRefreshToken,
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			},
			mockFindByTokenErr: nil,
			setupMock:          true,
			expectedError:      true,
			expectedErrorMsg:   "refresh token has expired",
		},
		{
			name:               "should return error when repository fails",
			refreshToken:       validRefreshToken,
			mockFindByToken:    nil,
			mockFindByTokenErr: errors.New("database error"),
			setupMock:          true,
			expectedError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Setup mock repositories
			mockRepo := mock.NewMockUserRepository(ctrl)
			mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)

			// Setup FindByToken expectation only when JWT is valid
			if tt.setupMock {
				mockRefreshTokenRepo.EXPECT().
					FindByToken(gomock.Any(), tt.refreshToken).
					Return(tt.mockFindByToken, tt.mockFindByTokenErr).
					Times(1)
			}

			// Create service with same token config
			svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

			// Call refresh
			result, err := svc.Refresh(context.Background(), tt.refreshToken)

			// Assert results
			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error message '%s', got '%s'", tt.expectedErrorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected non-nil result")
				}
				if result != nil && result.Message == "" {
					t.Errorf("expected non-empty message (access token)")
				}
			}
		})
	}
}

func TestRefreshContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockUserRepository(ctrl)
	mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)
	tokenConfig := token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		RefreshTokenSecret: "test-refresh-secret",
	}
	tokenSvc := token.NewTokenService(tokenConfig)

	svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := svc.Refresh(ctx, "some-token")

	if err == nil {
		t.Errorf("expected context.Canceled error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestGetUser(t *testing.T) {
	testUserID := uuid.New()

	tests := []struct {
		name             string
		userIDString     string
		mockFindByID     *entity.UserEntity
		mockFindByIDErr  error
		expectedError    bool
		expectedErrorMsg string
	}{
		{
			name:         "should get user successfully",
			userIDString: testUserID.String(),
			mockFindByID: &entity.UserEntity{
				ID:    testUserID,
				Email: "test@example.com",
				Name:  "Test User",
			},
			mockFindByIDErr: nil,
			expectedError:   false,
		},
		{
			name:             "should return error when user id is invalid",
			userIDString:     "invalid-uuid",
			mockFindByID:     nil,
			expectedError:    true,
			expectedErrorMsg: "invalid user id",
		},
		{
			name:             "should return error when user not found",
			userIDString:     testUserID.String(),
			mockFindByID:     nil,
			mockFindByIDErr:  nil,
			expectedError:    true,
			expectedErrorMsg: "user not found",
		},
		{
			name:            "should return error when repository fails",
			userIDString:    testUserID.String(),
			mockFindByID:    nil,
			mockFindByIDErr: errors.New("database error"),
			expectedError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Setup mock repositories
			mockRepo := mock.NewMockUserRepository(ctrl)
			mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)

			// Setup FindByID expectation only if valid UUID
			if _, err := uuid.Parse(tt.userIDString); err == nil {
				mockRepo.EXPECT().
					FindByID(gomock.Any(), gomock.Any()).
					Return(tt.mockFindByID, tt.mockFindByIDErr).
					Times(1)
			}

			// Setup token service
			tokenConfig := token.TokenConfig{
				AccessTokenSecret:  "test-access-secret",
				RefreshTokenSecret: "test-refresh-secret",
			}
			tokenSvc := token.NewTokenService(tokenConfig)

			// Create service
			svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

			// Call getuser
			result, err := svc.GetUser(context.Background(), tt.userIDString)

			// Assert results
			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error message '%s', got '%s'", tt.expectedErrorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected non-nil result")
				}
			}
		})
	}
}

func TestGetUserContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockUserRepository(ctrl)
	mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)
	tokenConfig := token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		RefreshTokenSecret: "test-refresh-secret",
	}
	tokenSvc := token.NewTokenService(tokenConfig)

	svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := svc.GetUser(ctx, uuid.New().String())

	if err == nil {
		t.Errorf("expected context.Canceled error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}
