package user

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// TestDeleteAccountRemovesTheUserAndEverySession pins the order: the sessions
// go before the account row. The reverse order would, if the transaction were
// ever removed, leave live credentials pointing at an account that is gone.
func TestDeleteAccountRemovesTheUserAndEverySession(t *testing.T) {
	deps := newTestDeps(t)
	userID := uuid.New()

	gomock.InOrder(
		deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(accountFor(t, userID), nil),
		deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil),
		deps.users.EXPECT().Delete(gomock.Any(), userID).Return(nil),
	)

	testutil.NoError(t, deps.service.DeleteAccount(context.Background(), userID))
}

func TestDeleteAccountFailures(t *testing.T) {
	tests := []struct {
		name    string
		expect  func(t *testing.T, deps *testDeps, userID uuid.UUID)
		wantErr error
	}{
		{
			// Deleting an account that is already gone is a 404, not a silent
			// success: the client asked about something that is not there.
			name: "the account does not exist",
			expect: func(_ *testing.T, deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(nil, nil)
			},
			wantErr: apperr.ErrUserNotFound,
		},
		{
			name: "the lookup fails",
			expect: func(_ *testing.T, deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).
					Return(nil, errors.New("connection refused"))
			},
			wantErr: apperr.ErrInternal,
		},
		{
			name: "revoking the sessions fails",
			expect: func(t *testing.T, deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(accountFor(t, userID), nil)
				deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).
					Return(errors.New("delete failed"))
			},
			wantErr: apperr.ErrInternal,
		},
		{
			name: "deleting the user fails",
			expect: func(t *testing.T, deps *testDeps, userID uuid.UUID) {
				deps.users.EXPECT().FindByID(gomock.Any(), userID).Return(accountFor(t, userID), nil)
				deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil)
				deps.users.EXPECT().Delete(gomock.Any(), userID).Return(errors.New("delete failed"))
			},
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			userID := uuid.New()
			tt.expect(t, deps, userID)

			err := deps.service.DeleteAccount(context.Background(), userID)

			testutil.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestDeleteAccountContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := deps.service.DeleteAccount(ctx, uuid.New())

	testutil.ErrorIs(t, err, context.Canceled)
}

// TestADeletedAccountCanNeitherLogInNorRefresh is the behavioural consequence
// of DeleteAccount, asserted where it is visible: at the service boundary.
//
// The two halves fail for different reasons, and both matter. Login fails
// because the user row is soft-deleted and every lookup the application makes
// skips it, so FindByEmail reports nothing and Login gives the same answer it
// gives an unknown address. Refresh fails because the refresh-token rows were
// really deleted, so a token that still verifies as a JWT — and it will, for
// the rest of its week-long validity — has no row, which the reuse path treats
// as theft and refuses.
func TestADeletedAccountCanNeitherLogInNorRefresh(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		deps := newTestDeps(t)

		// What the repository returns for a soft-deleted row.
		deps.users.EXPECT().FindByEmail(gomock.Any(), "deleted@example.com").Return(nil, nil)

		_, err := deps.service.Login(context.Background(), &request.LoginRequest{
			Email:    "deleted@example.com",
			Password: currentPassword,
		})

		testutil.ErrorIs(t, err, apperr.ErrInvalidCredentials)
	})

	t.Run("refresh", func(t *testing.T) {
		deps := newTestDeps(t)
		userID := uuid.New()
		presented, hash := issuedRefreshToken(t, deps, userID)

		// The rows went with the account, so the lookup misses.
		deps.refreshTokens.EXPECT().FindByTokenHash(gomock.Any(), hash).Return(nil, nil)
		// And the reuse path tries to revoke a family that is already empty,
		// which succeeds and changes nothing.
		deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(nil)

		_, err := deps.service.Refresh(context.Background(), presented)

		testutil.ErrorIs(t, err, apperr.ErrRefreshTokenRevoked)
	})
}
