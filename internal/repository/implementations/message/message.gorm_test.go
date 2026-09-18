package message_test

import (
	"context"
	"testing"

	messageRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/message"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestMessageRepositoryReturnsTheSeededMessage(t *testing.T) {
	db := testutil.NewDB(t)
	repo := messageRepo.NewGORMMessageRepository(db)

	message, err := repo.GetMessage(context.Background(), "default")
	testutil.NoError(t, err)
	if message == nil {
		t.Fatalf("expected the seeded default message")
	}
	testutil.True(t, *message != "", "message has content")
}

// A missing key is (nil, nil), matching the user repository's convention.
func TestMessageRepositoryMissingKeyIsNotAnError(t *testing.T) {
	db := testutil.NewDB(t)
	repo := messageRepo.NewGORMMessageRepository(db)

	message, err := repo.GetMessage(context.Background(), "no-such-key")
	testutil.NoError(t, err)
	if message != nil {
		t.Errorf("expected nil message, got %q", *message)
	}
}

func TestMessageRepositoryHonoursContextCancellation(t *testing.T) {
	db := testutil.NewDB(t)
	repo := messageRepo.NewGORMMessageRepository(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.GetMessage(ctx, "default")
	testutil.ErrorIs(t, err, context.Canceled)
}
