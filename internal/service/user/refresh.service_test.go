package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/repository/mock"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
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
	validTokenHash := token.HashToken(validRefreshToken)

	tests := []struct {
		name          string
		refreshToken  string
		setupMocks    func(repo *mock.MockRefreshTokenRepository)
		expectedError error
		// expectAnyError covers internal failures whose exact error is wrapped.
		expectAnyError bool
	}{
		{
			name:         "should refresh token successfully",
			refreshToken: validRefreshToken,
			setupMocks: func(repo *mock.MockRefreshTokenRepository) {
				repo.EXPECT().
					FindByTokenHash(gomock.Any(), validTokenHash).
					Return(&entity.RefreshTokenEntity{
						ID:        uuid.New(),
						UserID:    testUserID,
						TokenHash: validTokenHash,
						ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
					}, nil).
					Times(1)
				repo.EXPECT().
					DeleteByTokenHash(gomock.Any(), validTokenHash).
					Return(nil).
					Times(1)
				repo.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(nil).
					Times(1)
			},
		},
		{
			name:          "should return error when refresh token is invalid JWT",
			refreshToken:  "invalid-token",
			setupMocks:    func(repo *mock.MockRefreshTokenRepository) {},
			expectedError: ErrInvalidRefreshToken,
		},
		{
			name:          "should return error when refresh token is empty",
			refreshToken:  "",
			setupMocks:    func(repo *mock.MockRefreshTokenRepository) {},
			expectedError: ErrInvalidRefreshToken,
		},
		{
			name:         "should revoke the whole token family when a rotated token is reused",
			refreshToken: validRefreshToken,
			setupMocks: func(repo *mock.MockRefreshTokenRepository) {
				repo.EXPECT().
					FindByTokenHash(gomock.Any(), validTokenHash).
					Return(nil, nil).
					Times(1)
				// Reuse of an already-rotated token means it was likely stolen, so
				// every token for the user must be dropped.
				repo.EXPECT().
					DeleteByUserID(gomock.Any(), testUserID).
					Return(nil).
					Times(1)
			},
			expectedError: ErrRefreshTokenRevoked,
		},
		{
			name:         "should return error when refresh token is expired in DB",
			refreshToken: validRefreshToken,
			setupMocks: func(repo *mock.MockRefreshTokenRepository) {
				repo.EXPECT().
					FindByTokenHash(gomock.Any(), validTokenHash).
					Return(&entity.RefreshTokenEntity{
						ID:        uuid.New(),
						UserID:    testUserID,
						TokenHash: validTokenHash,
						ExpiresAt: time.Now().Add(-1 * time.Hour),
					}, nil).
					Times(1)
				repo.EXPECT().
					DeleteByTokenHash(gomock.Any(), validTokenHash).
					Return(nil).
					Times(1)
			},
			expectedError: ErrRefreshTokenExpired,
		},
		{
			name:         "should return error when repository fails",
			refreshToken: validRefreshToken,
			setupMocks: func(repo *mock.MockRefreshTokenRepository) {
				repo.EXPECT().
					FindByTokenHash(gomock.Any(), validTokenHash).
					Return(nil, errors.New("database error")).
					Times(1)
			},
			expectAnyError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRepo := mock.NewMockUserRepository(ctrl)
			mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)
			tt.setupMocks(mockRefreshTokenRepo)

			svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

			result, err := svc.Refresh(context.Background(), tt.refreshToken)

			if tt.expectedError != nil || tt.expectAnyError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.expectedError != nil && !errors.Is(err, tt.expectedError) {
					t.Errorf("expected error %v, got %v", tt.expectedError, err)
				}
				if result != nil {
					t.Errorf("expected nil result on error, got %v", result)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatalf("expected non-nil result")
			}
			if result.AccessToken == "" {
				t.Errorf("expected non-empty access token")
			}
			if result.RefreshToken == "" {
				t.Errorf("expected non-empty refresh token")
			}
		})
	}
}

// TestRefreshStoresOnlyTokenHash locks in that the raw refresh token never reaches
// the database — only its SHA-256 digest does.
func TestRefreshStoresOnlyTokenHash(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testUserID := uuid.New()
	tokenSvc := token.NewTokenService(token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})
	validRefreshToken, err := tokenSvc.GenerateRefreshToken(testUserID)
	if err != nil {
		t.Fatalf("generating refresh token: %v", err)
	}
	validTokenHash := token.HashToken(validRefreshToken)

	mockRepo := mock.NewMockUserRepository(ctrl)
	mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)

	mockRefreshTokenRepo.EXPECT().
		FindByTokenHash(gomock.Any(), validTokenHash).
		Return(&entity.RefreshTokenEntity{
			ID:        uuid.New(),
			UserID:    testUserID,
			TokenHash: validTokenHash,
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		}, nil).
		Times(1)
	mockRefreshTokenRepo.EXPECT().
		DeleteByTokenHash(gomock.Any(), validTokenHash).
		Return(nil).
		Times(1)

	var stored entity.RefreshTokenEntity
	mockRefreshTokenRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, record entity.RefreshTokenEntity) error {
			stored = record
			return nil
		}).
		Times(1)

	svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

	result, err := svc.Refresh(context.Background(), validRefreshToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stored.TokenHash == result.RefreshToken {
		t.Error("stored the raw refresh token instead of its hash")
	}
	if stored.TokenHash != token.HashToken(result.RefreshToken) {
		t.Errorf("stored hash %q does not match hash of issued token", stored.TokenHash)
	}
	if len(stored.TokenHash) != 64 {
		t.Errorf("expected a 64-char SHA-256 hex digest, got %d chars", len(stored.TokenHash))
	}
	if stored.UserID != testUserID {
		t.Errorf("expected user id %s, got %s", testUserID, stored.UserID)
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := svc.Refresh(ctx, "some-token")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestGetUser(t *testing.T) {
	testUserID := uuid.New()

	tests := []struct {
		name            string
		userIDString    string
		mockFindByID    *entity.UserEntity
		mockFindByIDErr error
		expectedError   error
		expectAnyError  bool
	}{
		{
			name:         "should get user successfully",
			userIDString: testUserID.String(),
			mockFindByID: &entity.UserEntity{
				ID:    testUserID,
				Email: "test@example.com",
				Name:  "Test User",
			},
		},
		{
			name:          "should return error when user id is invalid",
			userIDString:  "invalid-uuid",
			expectedError: ErrInvalidUserID,
		},
		{
			name:          "should return error when user not found",
			userIDString:  testUserID.String(),
			expectedError: ErrUserNotFound,
		},
		{
			name:            "should return error when repository fails",
			userIDString:    testUserID.String(),
			mockFindByIDErr: errors.New("database error"),
			expectAnyError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRepo := mock.NewMockUserRepository(ctrl)
			mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)

			if _, err := uuid.Parse(tt.userIDString); err == nil {
				mockRepo.EXPECT().
					FindByID(gomock.Any(), gomock.Any()).
					Return(tt.mockFindByID, tt.mockFindByIDErr).
					Times(1)
			}

			tokenConfig := token.TokenConfig{
				AccessTokenSecret:  "test-access-secret",
				RefreshTokenSecret: "test-refresh-secret",
			}
			tokenSvc := token.NewTokenService(tokenConfig)

			svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

			result, err := svc.GetUser(context.Background(), tt.userIDString)

			if tt.expectedError != nil || tt.expectAnyError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.expectedError != nil && !errors.Is(err, tt.expectedError) {
					t.Errorf("expected error %v, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatalf("expected non-nil result")
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := svc.GetUser(ctx, uuid.New().String())

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}
