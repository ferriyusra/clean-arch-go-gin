package user

import (
	"context"
	"errors"
	"testing"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/mock"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"
)

func TestLogin(t *testing.T) {
	// Hash a password for testing
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	testUserID := uuid.New()

	tests := []struct {
		name               string
		request            *request.LoginRequest
		mockFindByEmail    *entity.UserEntity
		mockFindByEmailErr error
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
			expectedErrorMsg:   "invalid email or password",
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
			expectedErrorMsg:   "invalid email or password",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Setup mock repositories
			mockRepo := mock.NewMockUserRepository(ctrl)
			mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)

			// Setup FindByEmail expectation
			mockRepo.EXPECT().
				FindByEmail(gomock.Any(), tt.request.Email).
				Return(tt.mockFindByEmail, tt.mockFindByEmailErr).
				Times(1)

			// Setup token service
			tokenConfig := token.TokenConfig{
				AccessTokenSecret:  "test-access-secret",
				RefreshTokenSecret: "test-refresh-secret",
			}
			tokenSvc := token.NewTokenService(tokenConfig)

			// Create service with mocked repositories
			svc := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

			// Call the method being tested
			result, err := svc.Login(context.Background(), tt.request)

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
				if result != nil {
					if result.User.Email != tt.request.Email {
						t.Errorf("expected email %s, got %s", tt.request.Email, result.User.Email)
					}
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

	// Create a cancelled context
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
