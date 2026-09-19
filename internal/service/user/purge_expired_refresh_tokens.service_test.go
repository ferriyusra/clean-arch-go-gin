package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestPurgeExpiredRefreshTokens(t *testing.T) {
	tests := []struct {
		name       string
		removed    int64
		deleteErr  error
		wantErr    error
		wantResult int64
	}{
		{name: "reports how many rows went", removed: 7, wantResult: 7},
		{name: "an empty sweep is success", removed: 0, wantResult: 0},
		{
			name:      "a failure is internal",
			deleteErr: errors.New("database down"),
			wantErr:   apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)

			// Pin the clock so the assertion is about the cutoff being "now",
			// not about however long the test took to reach this line.
			now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
			deps.service.(*userService).now = func() time.Time { return now }

			deps.refreshTokens.EXPECT().DeleteExpired(gomock.Any(), now).
				Return(tt.removed, tt.deleteErr)

			removed, err := deps.service.PurgeExpiredRefreshTokens(context.Background())

			if tt.wantErr != nil {
				testutil.ErrorIs(t, err, tt.wantErr)
				return
			}

			testutil.NoError(t, err)
			testutil.Equal(t, removed, tt.wantResult, "rows removed")
		})
	}
}

func TestPurgeExpiredRefreshTokensContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := deps.service.PurgeExpiredRefreshTokens(ctx)

	testutil.ErrorIs(t, err, context.Canceled)
}
