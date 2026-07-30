package user

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/repository/mock"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
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
