package user

import (
	"testing"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/mock"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"go.uber.org/mock/gomock"
)

func TestNewUserService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockUserRepository(ctrl)
	mockRefreshTokenRepo := mock.NewMockRefreshTokenRepository(ctrl)
	tokenConfig := token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		RefreshTokenSecret: "test-refresh-secret",
	}
	tokenSvc := token.NewTokenService(tokenConfig)

	service := NewUserService(mockRepo, mockRefreshTokenRepo, tokenSvc)

	if service == nil {
		t.Errorf("expected non-nil service, got nil")
	}

	// Verify service is a UserService
	_, ok := service.(*userService)
	if !ok {
		t.Errorf("expected *userService, got %T", service)
	}
}
