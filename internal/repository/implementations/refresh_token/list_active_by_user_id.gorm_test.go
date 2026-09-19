package refresh_token_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	refreshRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/refresh_token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestRefreshTokenRepositoryListActiveByUserID(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	now := time.Now()
	userID := uuid.New()
	otherUser := uuid.New()

	// Three live sessions for the user, one expired, one belonging to someone
	// else. Created at distinct instants so "newest first" is well defined.
	live := []struct {
		hash      string
		createdAt time.Time
	}{
		{"oldest", now.Add(-3 * time.Hour)},
		{"middle", now.Add(-2 * time.Hour)},
		{"newest", now.Add(-1 * time.Hour)},
	}
	for _, row := range live {
		record := newToken(userID, row.hash, now.Add(24*time.Hour))
		record.CreatedAt = row.createdAt
		testutil.NoError(t, db.Create(&record).Error)
	}

	expired := newToken(userID, "expired", now.Add(-time.Minute))
	testutil.NoError(t, db.Create(&expired).Error)
	testutil.NoError(t, repo.Create(ctx, newToken(otherUser, "someone-else", now.Add(24*time.Hour))))

	t.Run("returns only the user's unexpired rows, newest first", func(t *testing.T) {
		found, err := repo.ListActiveByUserID(ctx, userID, now, 10, 0)
		testutil.NoError(t, err)
		testutil.Equal(t, len(found), 3, "row count")

		hashes := make([]string, len(found))
		for i, row := range found {
			hashes[i] = row.TokenHash
		}
		testutil.DeepEqual(t, hashes, []string{"newest", "middle", "oldest"}, "order")
	})

	t.Run("honours limit and offset", func(t *testing.T) {
		page, err := repo.ListActiveByUserID(ctx, userID, now, 2, 0)
		testutil.NoError(t, err)
		testutil.Equal(t, len(page), 2, "first page size")
		testutil.Equal(t, page[0].TokenHash, "newest", "first row of page one")

		page, err = repo.ListActiveByUserID(ctx, userID, now, 2, 2)
		testutil.NoError(t, err)
		testutil.Equal(t, len(page), 1, "second page size")
		testutil.Equal(t, page[0].TokenHash, "oldest", "first row of page two")
	})

	t.Run("a user with no sessions gets an empty slice, not an error", func(t *testing.T) {
		found, err := repo.ListActiveByUserID(ctx, uuid.New(), now, 10, 0)
		testutil.NoError(t, err)
		testutil.Equal(t, len(found), 0, "row count")
	})

	t.Run("counts the same rows the listing returns", func(t *testing.T) {
		total, err := repo.CountActiveByUserID(ctx, userID, now)
		testutil.NoError(t, err)
		testutil.Equal(t, total, int64(3), "active session count")

		// The expired row is excluded from both, which is what keeps the total
		// consistent with the page.
		otherTotal, err := repo.CountActiveByUserID(ctx, otherUser, now)
		testutil.NoError(t, err)
		testutil.Equal(t, otherTotal, int64(1), "another user's count")
	})
}

// TestRefreshTokenRepositoryListRespectsTheExpiryInstant pins that "active" is
// evaluated against the caller's clock, not the database's: passing an instant
// in the future must make every row expired.
func TestRefreshTokenRepositoryListRespectsTheExpiryInstant(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	now := time.Now()
	userID := uuid.New()
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "live-for-an-hour", now.Add(time.Hour))))

	found, err := repo.ListActiveByUserID(ctx, userID, now, 10, 0)
	testutil.NoError(t, err)
	testutil.Equal(t, len(found), 1, "sessions live now")

	found, err = repo.ListActiveByUserID(ctx, userID, now.Add(2*time.Hour), 10, 0)
	testutil.NoError(t, err)
	testutil.Equal(t, len(found), 0, "sessions live two hours from now")

	total, err := repo.CountActiveByUserID(ctx, userID, now.Add(2*time.Hour))
	testutil.NoError(t, err)
	testutil.Equal(t, total, int64(0), "count two hours from now")
}

func TestRefreshTokenRepositoryListHonoursContextCancellation(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.ListActiveByUserID(ctx, uuid.New(), time.Now(), 10, 0)
	testutil.ErrorIs(t, err, context.Canceled)

	_, err = repo.CountActiveByUserID(ctx, uuid.New(), time.Now())
	testutil.ErrorIs(t, err, context.Canceled)
}
