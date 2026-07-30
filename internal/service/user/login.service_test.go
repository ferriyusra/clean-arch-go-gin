package user

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/repository/mock"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
)

func TestLogin(t *testing.T) {
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	testUserID := uuid.New()

	tests := []struct {
		name               string
		request            *request.LoginRequest
		mockFindByEmail    *entity.UserEntity
		mockFindByEmailErr error
		mockStoreTokenErr  error
		expectedError      bool
		expectedErrorMsg   string
	}{
		{
			name: "should login user successfully",
			request: &request.LoginRequest{
				Email:    "test@example.com",
				Password: "password123",
			},
			mockFindByEmail: &entity.UserEntity{
				ID:       testUserID,
				Email:    "test@example.com",
				Password: hashedPassword,
				Name:     "Test User",
			},
			mockFindByEmailErr: nil,
			mockStoreTokenErr:  nil,
			expectedError:      false,
		},
		{
			name: "should return error when user not found",
			request: &request.LoginRequest{
				Email:    "nonexistent@example.com",
				Password: "password123",
			},
			mockFindByEmail:    nil,
			mockFindByEmailErr: nil,
			expectedError:      true,
			expectedErrorMsg:   ErrInvalidCredentials.Error(),
		},
		{
			name: "should return error when password is wrong",
			request: &request.LoginRequest{
				Email:    "test@example.com",
				Password: "wrongpassword",
			},
			mockFindByEmail: &entity.UserEntity{
				ID:       testUserID,
				Email:    "test@example.com",
				Password: hashedPassword,
				Name:     "Test User",
			},
			mockFindByEmailErr: nil,
			expectedError:      true,
			expectedErrorMsg:   ErrInvalidCredentials.Error(),
		},
		{
			name: "should return error when repository fails",
			request: &request.LoginRequest{
				Email:    "test@example.com",
				Password: "password123",
			},
			mockFindByEmail:    nil,
			mockFindByEmailErr: errors.New("database error"),
			expectedError:      true,
		},
		{
			name: "should return error when storing refresh token fails",
			request: &request.LoginRequest{
				Email:    "test@example.com",
				Password: "password123",
			},
			mockFindByEmail: &entity.UserEntity{
				ID:       testUserID,
				Email:    "test@example.com",
				Password: hashedPassword,
				Name:     "Test User",
			},
			mockFindByEmailErr: nil,
			mockStoreTokenErr:  errors.New("token store failed"),
			expectedError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRepo := mock.NewMockUserRepository(ctrl)
			mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)

			mockRepo.EXPECT().
				FindByEmail(gomock.Any(), tt.request.Email).
				Return(tt.mockFindByEmail, tt.mockFindByEmailErr).
				Times(1)

			// Token storage is called only on successful credential check
			if tt.mockFindByEmailErr == nil && tt.mockFindByEmail != nil && tt.expectedErrorMsg != ErrInvalidCredentials.Error() {
				mockRefreshTokenRepo.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(tt.mockStoreTokenErr).
					Times(1)
			}

			tokenConfig := token.TokenConfig{
				AccessTokenSecret:  "test-access-secret",
				RefreshTokenSecret: "test-refresh-secret",
			}
			tokenSvc := token.NewTokenService(tokenConfig)

			svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

			result, err := svc.Login(context.Background(), tt.request)

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
					t.Fatal("expected non-nil result")
				}
				if result.User.Email != tt.request.Email {
					t.Errorf("expected email %s, got %s", tt.request.Email, result.User.Email)
				}
				if result.AccessToken == "" {
					t.Error("expected non-empty access token")
				}
				if result.RefreshToken == "" {
					t.Error("expected non-empty refresh token")
				}
			}
		})
	}
}

func TestLoginContextCancellation(t *testing.T) {
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

	req := &request.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	result, err := svc.Login(ctx, req)

	if err == nil {
		t.Errorf("expected context.Canceled error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}
