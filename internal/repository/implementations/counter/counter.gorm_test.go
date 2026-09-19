package counter_test

import (
	"context"
	"testing"

	counterRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/counter"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// TestCounterRepositoryStartsFromTheSeededRow also covers platform.Migrate's
// seeding: without it, GetCounter would find no row at all.
func TestCounterRepositoryStartsFromTheSeededRow(t *testing.T) {
	db := testutil.NewDB(t)
	repo := counterRepo.NewGORMCounterRepository(db)

	value, err := repo.GetCounter(context.Background())
	testutil.NoError(t, err)
	testutil.Equal(t, value, 0, "seeded counter value")
}

func TestCounterRepositoryIncrementPersists(t *testing.T) {
	db := testutil.NewDB(t)
	repo := counterRepo.NewGORMCounterRepository(db)
	ctx := context.Background()

	for want := 1; want <= 3; want++ {
		got, err := repo.IncrementCounter(ctx)
		testutil.NoError(t, err)
		testutil.Equal(t, got, want, "value returned by increment")
	}

	// The increment must be durable, not just reflected in the return value.
	stored, err := repo.GetCounter(ctx)
	testutil.NoError(t, err)
	testutil.Equal(t, stored, 3, "value read back after three increments")
}

func TestCounterRepositoryHonoursContextCancellation(t *testing.T) {
	db := testutil.NewDB(t)
	repo := counterRepo.NewGORMCounterRepository(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.IncrementCounter(ctx)
	testutil.ErrorIs(t, err, context.Canceled)
}
