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

func TestStoreRefreshToken(t *testing.T) {
	userID := uuid.New()
	expiresAt := time.Now().Add(testRefreshTTL)

	tests := []struct {
		name      string
		createErr error
		wantErr   error
	}{
		{
			name: "persists the token",
		},
		{
			name:      "reports a write failure as internal",
			createErr: errors.New("insert failed"),
			wantErr:   apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)

			// Capture the row so the test asserts what is written, not merely
			// that something was.
			var stored entity.RefreshTokenEntity
			deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, token entity.RefreshTokenEntity) error {
					stored = token
					return tt.createErr
				})

			err := deps.service.StoreRefreshToken(context.Background(), userID, "the-token", expiresAt)

			if tt.wantErr != nil {
				testutil.ErrorIs(t, err, tt.wantErr)
				return
			}

			testutil.NoError(t, err)
			testutil.Equal(t, stored.UserID, userID, "user id")
			testutil.Equal(t, stored.TokenHash, token.Hash("the-token"), "the digest, not the token, is stored")
			testutil.True(t, stored.TokenHash != "the-token", "the raw token is never persisted")
			testutil.Equal(t, stored.ExpiresAt, expiresAt, "expiry")
			testutil.True(t, stored.ID != uuid.Nil, "row is given an id")
		})
	}
}

func TestStoreRefreshTokenContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := deps.service.StoreRefreshToken(ctx, uuid.New(), "the-token", time.Now())

	testutil.ErrorIs(t, err, context.Canceled)
}
