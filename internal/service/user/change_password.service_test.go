package user

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

const (
	currentPassword = "the-old-password"
	newPassword     = "a-brand-new-password"
)

// accountFor builds a user whose password hash really is a bcrypt hash of
// currentPassword, so the comparison under test is the genuine one.
func accountFor(t *testing.T, id uuid.UUID) *entity.UserEntity {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(currentPassword), bcrypt.MinCost)
	testutil.NoError(t, err)

	return &entity.UserEntity{
		ID:       id,
		Email:    "test@example.com",
		Name:     "Test User",
		Password: hash,
	}
}

func changeRequest() *request.ChangePasswordRequest {
	return &request.ChangePasswordRequest{
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
	}
}

// TestChangePasswordRevokesEverySessionAndReissuesOne is the whole point of the
// endpoint. The order is load-bearing: the revocation has to happen before the
// new pair is issued, or the pair that was just minted is deleted with the rest
// and the caller is signed out of the device they just used.
func TestChangePasswordRevokesEverySessionAndReissuesOne(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()
	user := accountFor(t, userID)

	var storedHash string

	gomock.InOrder(
		deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(user, nil),
		deps.users.EXPECT().Update(gomock.Any(), userID, gomock.Any()).Return(nil),
		deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil),
		deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, row entity.RefreshTokenEntity) error {
				storedHash = row.TokenHash
				return nil
			}),
	)

	result, err := deps.service.ChangePassword(context.Background(), userID, changeRequest())

	testutil.NoError(t, err)
	testutil.True(t, result.AccessToken != "", "an access token is issued")
	testutil.True(t, result.RefreshToken != "", "a refresh token is issued")

	// The caller stays signed in: the token they get back actually validates.
	claims, err := deps.tokens.ValidateAccessToken(result.AccessToken)
	testutil.NoError(t, err)
	testutil.Equal(t, claims.UserID, userID, "user id in the new access token")

	// And the surviving session is the new one, stored as a digest.
	testutil.Equal(t, storedHash, token.Hash(result.RefreshToken), "the stored digest matches the issued token")
}

// TestChangePasswordWritesABcryptHashNotThePassword guards the obvious
// catastrophe: the new password reaching the database in the clear.
func TestChangePasswordWritesABcryptHashNotThePassword(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()

	var written []byte
	deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(accountFor(t, userID), nil)
	deps.users.EXPECT().Update(gomock.Any(), userID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, updated entity.UserEntity) error {
			written = updated.Password
			return nil
		})
	deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil)
	deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

	_, err := deps.service.ChangePassword(context.Background(), userID, changeRequest())
	testutil.NoError(t, err)

	testutil.True(t, string(written) != newPassword, "the password is not written in the clear")
	testutil.NoError(t, bcrypt.CompareHashAndPassword(written, []byte(newPassword)))
}

// TestChangePasswordUpdatesOnlyThePassword pins that the update carries the new
// hash and nothing else. GORM's Updates skips zero-valued struct fields, so an
// empty Email here must not blank the stored one — and a future field added to
// the literal would silently do exactly that.
func TestChangePasswordUpdatesOnlyThePassword(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()

	var updated entity.UserEntity
	deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(accountFor(t, userID), nil)
	deps.users.EXPECT().Update(gomock.Any(), userID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, u entity.UserEntity) error {
			updated = u
			return nil
		})
	deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil)
	deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

	_, err := deps.service.ChangePassword(context.Background(), userID, changeRequest())
	testutil.NoError(t, err)

	testutil.Equal(t, updated.Email, "", "email is left untouched")
	testutil.Equal(t, updated.Name, "", "name is left untouched")
	testutil.Equal(t, updated.ID, uuid.Nil, "the id is not overwritten")
}

func TestChangePasswordRejectsAWrongCurrentPassword(t *testing.T) {
	tests := []struct {
		name string
		// expect stages the lookup; the returned password is what the request
		// below is checked against.
		expect  func(t *testing.T, deps *testDeps, userID uuid.UUID)
		given   string
		wantErr error
	}{
		{
			name: "the current password does not match",
			expect: func(t *testing.T, deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(accountFor(t, userID), nil)
			},
			given:   "not-the-current-password",
			wantErr: apperr.ErrInvalidCredentials,
		},
		{
			// A valid token for an account that is gone must not become an
			// existence oracle: same answer as a wrong password.
			name: "the account no longer exists",
			expect: func(_ *testing.T, deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(nil, nil)
			},
			given:   currentPassword,
			wantErr: apperr.ErrInvalidCredentials,
		},
		{
			name: "the lookup fails",
			expect: func(_ *testing.T, deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).
					Return(nil, errors.New("connection refused"))
			},
			given:   currentPassword,
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			userID := uuid.New()
			tt.expect(t, deps, userID)

			req := changeRequest()
			req.CurrentPassword = tt.given

			_, err := deps.service.ChangePassword(context.Background(), userID, req)

			testutil.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestChangePasswordReportsWriteFailures: a half-applied change is the one
// outcome that must not be reported as success, because it is the outcome where
// the password changed but the stolen sessions survived.
func TestChangePasswordReportsWriteFailures(t *testing.T) {
	tests := []struct {
		name   string
		expect func(deps *testDeps, userID uuid.UUID)
	}{
		{
			name: "the update fails",
			expect: func(deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().Update(gomock.Any(), userID, gomock.Any()).
					Return(errors.New("write failed"))
			},
		},
		{
			name: "the revocation fails",
			expect: func(deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().Update(gomock.Any(), userID, gomock.Any()).Return(nil)
				deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).
					Return(errors.New("delete failed"))
			},
		},
		{
			name: "storing the new session fails",
			expect: func(deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().Update(gomock.Any(), userID, gomock.Any()).Return(nil)
				deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil)
				deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
					Return(errors.New("insert failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			userID := uuid.New()

			deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(accountFor(t, userID), nil)
			tt.expect(deps, userID)

			_, err := deps.service.ChangePassword(context.Background(), userID, changeRequest())

			testutil.ErrorIs(t, err, apperr.ErrInternal)
		})
	}
}

func TestChangePasswordContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := deps.service.ChangePassword(ctx, uuid.New(), changeRequest())

	testutil.ErrorIs(t, err, context.Canceled)
}
