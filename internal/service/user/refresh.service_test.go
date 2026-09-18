package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestRefresh(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name string
		// token is built per-case because a valid one has to be signed by the
		// same service under test.
		token   func(deps *testDeps) string
		expect  func(deps *testDeps, token string)
		wantErr error
	}{
		{
			name: "issues a new access token for a live refresh token",
			token: func(deps *testDeps) string {
				tokenStr, err := deps.tokens.GenerateRefreshToken(userID)
				testutil.NoError(t, err)
				return tokenStr
			},
			expect: func(deps *testDeps, tokenStr string) {
				deps.refreshTokens.EXPECT().FindByToken(gomock.Any(), tokenStr).
					Return(&entity.RefreshTokenEntity{
						ID:        uuid.New(),
						UserID:    userID,
						Token:     tokenStr,
						ExpiresAt: time.Now().Add(time.Hour),
					}, nil)
			},
		},
		{
			name:    "rejects a malformed token",
			token:   func(*testDeps) string { return "not-a-jwt" },
			expect:  func(*testDeps, string) {},
			wantErr: apperr.ErrInvalidRefreshToken,
		},
		{
			name: "rejects a token that is no longer stored (revoked by logout)",
			token: func(deps *testDeps) string {
				tokenStr, err := deps.tokens.GenerateRefreshToken(userID)
				testutil.NoError(t, err)
				return tokenStr
			},
			expect: func(deps *testDeps, tokenStr string) {
				deps.refreshTokens.EXPECT().FindByToken(gomock.Any(), tokenStr).Return(nil, nil)
			},
			wantErr: apperr.ErrRefreshTokenRevoked,
		},
		{
			name: "rejects a stored token past its expiry",
			token: func(deps *testDeps) string {
				tokenStr, err := deps.tokens.GenerateRefreshToken(userID)
				testutil.NoError(t, err)
				return tokenStr
			},
			expect: func(deps *testDeps, tokenStr string) {
				deps.refreshTokens.EXPECT().FindByToken(gomock.Any(), tokenStr).
					Return(&entity.RefreshTokenEntity{
						ID:        uuid.New(),
						UserID:    userID,
						Token:     tokenStr,
						ExpiresAt: time.Now().Add(-time.Hour),
					}, nil)
			},
			wantErr: apperr.ErrRefreshTokenExpired,
		},
		{
			name: "reports a lookup failure as internal",
			token: func(deps *testDeps) string {
				tokenStr, err := deps.tokens.GenerateRefreshToken(userID)
				testutil.NoError(t, err)
				return tokenStr
			},
			expect: func(deps *testDeps, tokenStr string) {
				deps.refreshTokens.EXPECT().FindByToken(gomock.Any(), tokenStr).
					Return(nil, errors.New("database error"))
			},
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			tokenStr := tt.token(deps)
			tt.expect(deps, tokenStr)

			result, err := deps.service.Refresh(context.Background(), tokenStr)

			if tt.wantErr != nil {
				testutil.ErrorIs(t, err, tt.wantErr)
				return
			}

			testutil.NoError(t, err)
			if result == nil {
				t.Fatalf("expected a result")
			}
			testutil.True(t, result.AccessToken != "", "a new access token is issued")

			// The token must actually validate, not merely be non-empty.
			claims, err := deps.tokens.ValidateAccessToken(result.AccessToken)
			testutil.NoError(t, err)
			testutil.Equal(t, claims.UserID, userID, "user id in the new access token")
		})
	}
}

// TestRefreshRejectsAnAccessTokenPresentedAsRefresh guards the separation of the
// two signing secrets.
func TestRefreshRejectsAnAccessTokenPresentedAsRefresh(t *testing.T) {
	deps := newTestDeps(t)

	accessToken, err := deps.tokens.GenerateAccessToken(uuid.New(), "test@example.com", "Test User")
	testutil.NoError(t, err)

	_, err = deps.service.Refresh(context.Background(), accessToken)

	testutil.ErrorIs(t, err, apperr.ErrInvalidRefreshToken)
}

func TestRefreshContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := deps.service.Refresh(ctx, "any-token")

	testutil.ErrorIs(t, err, context.Canceled)
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestGetUser(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name    string
		userID  string
		expect  func(deps *testDeps)
		wantErr error
	}{
		{
			name:   "returns the user",
			userID: userID.String(),
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(&entity.UserEntity{
					ID:    userID,
					Email: "test@example.com",
					Name:  "Test User",
				}, nil)
			},
		},
		{
			name:    "rejects an id that is not a UUID",
			userID:  "not-a-uuid",
			expect:  func(*testDeps) {},
			wantErr: apperr.ErrInvalidUserID,
		},
		{
			name:   "reports a missing user as not found",
			userID: userID.String(),
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(nil, nil)
			},
			wantErr: apperr.ErrUserNotFound,
		},
		{
			name:   "reports a lookup failure as internal, not as not found",
			userID: userID.String(),
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).
					Return(nil, errors.New("database error"))
			},
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			tt.expect(deps)

			result, err := deps.service.GetUser(context.Background(), tt.userID)

			if tt.wantErr != nil {
				testutil.ErrorIs(t, err, tt.wantErr)
				return
			}

			testutil.NoError(t, err)
			if result == nil {
				t.Fatalf("expected a result")
			}
			testutil.Equal(t, result.ID, userID, "user id")
		})
	}
}

func TestGetUserContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := deps.service.GetUser(ctx, uuid.New().String())

	testutil.ErrorIs(t, err, context.Canceled)
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}
