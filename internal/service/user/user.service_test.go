package user

import (
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/mock"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

const testRefreshTTL = 7 * 24 * time.Hour

// testDeps bundles what every user-service test needs, so each test file states
// only the expectations that matter to it.
type testDeps struct {
	users         *mock.MockUserRepository
	refreshTokens *mock.MockRefreshTokenRepository
	tokens        token.TokenService
	service       UserService
}

// newTestDeps wires the service against mocked repositories and a real token
// service. The token service is genuine on purpose: it is pure, deterministic
// and has no I/O, so mocking it would only assert that the mock was called.
func newTestDeps(t *testing.T) *testDeps {
	t.Helper()

	ctrl := gomock.NewController(t)
	users := mock.NewMockUserRepository(ctrl)
	refreshTokens := mock.NewMockRefreshTokenRepository(ctrl)
	tokens := token.NewTokenService(token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: testRefreshTTL,
	})

	return &testDeps{
		users:         users,
		refreshTokens: refreshTokens,
		tokens:        tokens,
		service:       NewUserService(users, refreshTokens, tokens, testRefreshTTL),
	}
}

func TestNewUserService(t *testing.T) {
	deps := newTestDeps(t)

	if deps.service == nil {
		t.Fatalf("expected non-nil service, got nil")
	}

	concrete, ok := deps.service.(*userService)
	if !ok {
		t.Fatalf("expected *userService, got %T", deps.service)
	}

	testutil.Equal(t, concrete.refreshTokenTTL, testRefreshTTL, "refresh token TTL")
}
