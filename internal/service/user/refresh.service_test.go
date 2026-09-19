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
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// issuedRefreshToken mints a refresh token and returns it with the digest the
// repository would be asked for.
func issuedRefreshToken(t *testing.T, deps *testDeps, userID uuid.UUID) (string, string) {
	t.Helper()

	tokenStr, err := deps.tokens.GenerateRefreshToken(userID)
	testutil.NoError(t, err)

	return tokenStr, token.Hash(tokenStr)
}

func liveRow(userID uuid.UUID, hash string) *entity.RefreshTokenEntity {
	return &entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

// TestRefreshRotatesTheTokenPair is the core of the rotation design: the token
// that was presented must be gone afterwards, and a different one issued.
func TestRefreshRotatesTheTokenPair(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()
	presented, hash := issuedRefreshToken(t, deps, userID)

	var storedHash string
	gomock.InOrder(
		deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).
			Return(liveRow(userID, hash), nil),
		// The old row goes before the new one is written, so a crash in between
		// costs a re-login rather than leaving two live tokens for one session.
		deps.refreshTokens.EXPECT().DeleteByTokenHash(gomock.Any(), hash).Return(nil),
		deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, row entity.RefreshTokenEntity) error {
				storedHash = row.TokenHash
				return nil
			}),
	)

	result, err := deps.service.Refresh(context.Background(), presented)

	testutil.NoError(t, err)
	if result == nil {
		t.Fatalf("expected a result")
	}
	testutil.True(t, result.AccessToken != "", "an access token is issued")
	testutil.True(t, result.RefreshToken != "", "a refresh token is issued")
	testutil.True(t, result.RefreshToken != presented, "the refresh token is a new one")
	testutil.Equal(t, storedHash, token.Hash(result.RefreshToken), "the new token is the one stored")

	// The new access token has to actually validate, not merely be non-empty.
	claims, err := deps.tokens.ValidateAccessToken(result.AccessToken)
	testutil.NoError(t, err)
	testutil.Equal(t, claims.UserID, userID, "user id in the new access token")
}

// TestRefreshStoresOnlyADigest is a security regression test. A database dump
// must not contain anything that can be replayed against the API.
func TestRefreshStoresOnlyADigest(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()
	presented, hash := issuedRefreshToken(t, deps, userID)

	var stored entity.RefreshTokenEntity
	deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).Return(liveRow(userID, hash), nil)
	deps.refreshTokens.EXPECT().DeleteByTokenHash(gomock.Any(), hash).Return(nil)
	deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, row entity.RefreshTokenEntity) error {
			stored = row
			return nil
		})

	result, err := deps.service.Refresh(context.Background(), presented)
	testutil.NoError(t, err)

	testutil.Equal(t, len(stored.TokenHash), 64, "a SHA-256 digest in hex")
	testutil.True(t, stored.TokenHash != result.RefreshToken, "the raw token is not what is stored")
	testutil.True(t, stored.TokenHash != presented, "the presented token is not what is stored")
}

// TestRefreshReuseRevokesEverySession covers the case that makes rotation worth
// having: a token that verifies but has no row was either revoked or already
// rotated. The second is a replay, so both are treated as theft.
func TestRefreshReuseRevokesEverySession(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()
	presented, hash := issuedRefreshToken(t, deps, userID)

	deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).Return(nil, nil)
	deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil)

	_, err := deps.service.Refresh(context.Background(), presented)

	testutil.ErrorIs(t, err, apperr.ErrRefreshTokenRevoked)
}

// Even when the revocation sweep fails, the token itself must still be refused.
func TestRefreshReuseStillRefusesWhenRevocationFails(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()
	presented, hash := issuedRefreshToken(t, deps, userID)

	deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).Return(nil, nil)
	deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).
		Return(errors.New("database down"))

	_, err := deps.service.Refresh(context.Background(), presented)

	testutil.ErrorIs(t, err, apperr.ErrRefreshTokenRevoked)
}

func TestRefreshRejectsBadTokens(t *testing.T) {
	tests := []struct {
		name    string
		token   func(deps *testDeps, userID uuid.UUID) string
		expect  func(deps *testDeps, userID uuid.UUID, hash string)
		wantErr error
	}{
		{
			name:    "a malformed token never reaches the database",
			token:   func(*testDeps, uuid.UUID) string { return "not-a-jwt" },
			expect:  func(*testDeps, uuid.UUID, string) {},
			wantErr: apperr.ErrInvalidRefreshToken,
		},
		{
			name: "an expired row is refused and cleaned up",
			token: func(deps *testDeps, userID uuid.UUID) string {
				tokenStr, _ := issuedRefreshToken(t, deps, userID)
				return tokenStr
			},
			expect: func(deps *testDeps, userID uuid.UUID, hash string) {
				deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).
					Return(&entity.RefreshTokenEntity{
						ID:        uuid.New(),
						UserID:    userID,
						TokenHash: hash,
						ExpiresAt: time.Now().Add(-time.Hour),
					}, nil)
				deps.refreshTokens.EXPECT().DeleteByTokenHash(gomock.Any(), hash).Return(nil)
			},
			wantErr: apperr.ErrRefreshTokenExpired,
		},
		{
			name: "a lookup failure is internal, not a revoked token",
			token: func(deps *testDeps, userID uuid.UUID) string {
				tokenStr, _ := issuedRefreshToken(t, deps, userID)
				return tokenStr
			},
			expect: func(deps *testDeps, userID uuid.UUID, hash string) {
				deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).
					Return(nil, errors.New("database error"))
			},
			wantErr: apperr.ErrInternal,
		},
		{
			name: "a rotation failure does not issue a new pair",
			token: func(deps *testDeps, userID uuid.UUID) string {
				tokenStr, _ := issuedRefreshToken(t, deps, userID)
				return tokenStr
			},
			expect: func(deps *testDeps, userID uuid.UUID, hash string) {
				deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).
					Return(liveRow(userID, hash), nil)
				deps.refreshTokens.EXPECT().DeleteByTokenHash(gomock.Any(), hash).
					Return(errors.New("delete failed"))
			},
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			userID := uuid.New()

			presented := tt.token(deps, userID)
			tt.expect(deps, userID, token.Hash(presented))

			result, err := deps.service.Refresh(context.Background(), presented)

			testutil.ErrorIs(t, err, tt.wantErr)
			if result != nil {
				t.Errorf("expected nil result on error, got %+v", result)
			}
		})
	}
}

// The two signing secrets must not be interchangeable, or a stolen access token
// could be traded for a full session.
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

// GetUser lives in refresh.service.go, so its tests live here too.
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
