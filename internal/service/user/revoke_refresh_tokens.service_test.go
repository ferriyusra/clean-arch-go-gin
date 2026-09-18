package user

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestRevokeRefreshTokens(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name      string
		deleteErr error
		wantErr   error
	}{
		{
			name: "deletes every token for the user",
		},
		{
			// Deleting nothing is success: logging out twice must not fail.
			name:      "reports a delete failure as internal",
			deleteErr: errors.New("delete failed"),
			wantErr:   apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			deps.refreshTokens.EXPECT().DeleteByUserID(gomock.Any(), userID).Return(tt.deleteErr)

			err := deps.service.RevokeRefreshTokens(context.Background(), userID)

			if tt.wantErr != nil {
				testutil.ErrorIs(t, err, tt.wantErr)
				return
			}
			testutil.NoError(t, err)
		})
	}
}

func TestRevokeRefreshTokensContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := deps.service.RevokeRefreshTokens(ctx, uuid.New())

	testutil.ErrorIs(t, err, context.Canceled)
}
