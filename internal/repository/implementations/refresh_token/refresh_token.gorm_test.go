package refresh_token_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	refreshRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/refresh_token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// newToken builds a stored token. The hash stands in for a real digest; the
// repository only ever sees hashes, never tokens.
func newToken(userID uuid.UUID, hash string, expiresAt time.Time) entity.RefreshTokenEntity {
	return entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: expiresAt,
	}
}

func future() time.Time { return time.Now().Add(24 * time.Hour) }

func TestRefreshTokenRepositoryCreateAndFind(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	userID := uuid.New()
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "hash-a", future())))

	found, err := repo.FindByTokenHash(ctx, "hash-a")
	testutil.NoError(t, err)
	if found == nil {
		t.Fatalf("expected to find the token")
	}
	testutil.Equal(t, found.UserID, userID, "user id")
}

func TestRefreshTokenRepositoryMissingTokenIsNotAnError(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)

	found, err := repo.FindByTokenHash(context.Background(), "never-issued")
	testutil.NoError(t, err)
	if found != nil {
		t.Errorf("expected nil token, got %+v", found)
	}
}

// TestRefreshTokenRepositoryRejectsADuplicateHash exercises the unique index.
// Two rows for one digest would make rotation ambiguous: deleting one would
// leave the other live.
func TestRefreshTokenRepositoryRejectsADuplicateHash(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	testutil.NoError(t, repo.Create(ctx, newToken(uuid.New(), "hash-a", future())))

	err := repo.Create(ctx, newToken(uuid.New(), "hash-a", future()))
	testutil.Error(t, err, "a second row with the same hash")
}

// TestRefreshTokenRepositoryDeletesForReal guards the removal of
// gorm.DeletedAt. A soft delete would leave a revoked credential in the table
// and keep its slot in the unique index, so the same digest could never be
// issued again and the row could be restored.
func TestRefreshTokenRepositoryDeletesForReal(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	testutil.NoError(t, repo.Create(ctx, newToken(uuid.New(), "hash-a", future())))
	testutil.NoError(t, repo.DeleteByTokenHash(ctx, "hash-a"))

	var remaining int64
	testutil.NoError(t, db.Unscoped().Model(&entity.RefreshTokenEntity{}).
		Where("token_hash = ?", "hash-a").Count(&remaining).Error)
	testutil.Equal(t, remaining, int64(0), "rows left after delete, including soft-deleted")

	// The digest is free again, which a soft delete would have prevented.
	testutil.NoError(t, repo.Create(ctx, newToken(uuid.New(), "hash-a", future())))
}

func TestRefreshTokenRepositoryDeleteByUserIDEndsEverySession(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	userID := uuid.New()
	otherUser := uuid.New()

	testutil.NoError(t, repo.Create(ctx, newToken(userID, "laptop", future())))
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "phone", future())))
	testutil.NoError(t, repo.Create(ctx, newToken(otherUser, "someone-else", future())))

	testutil.NoError(t, repo.DeleteByUserID(ctx, userID))

	for _, revoked := range []string{"laptop", "phone"} {
		found, err := repo.FindByTokenHash(ctx, revoked)
		testutil.NoError(t, err)
		if found != nil {
			t.Errorf("token %q should have been revoked", revoked)
		}
	}

	survivor, err := repo.FindByTokenHash(ctx, "someone-else")
	testutil.NoError(t, err)
	if survivor == nil {
		t.Errorf("another user session must not be revoked")
	}
}

// Logout can be called twice, and rotation can race a logout, so deleting
// something that is already gone has to succeed.
func TestRefreshTokenRepositoryDeleteIsIdempotent(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	userID := uuid.New()
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "hash-a", future())))

	testutil.NoError(t, repo.DeleteByUserID(ctx, userID))
	testutil.NoError(t, repo.DeleteByUserID(ctx, userID))
	testutil.NoError(t, repo.DeleteByTokenHash(ctx, "never-existed"))
}

func TestRefreshTokenRepositoryDeleteExpired(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	now := time.Now()
	userID := uuid.New()

	testutil.NoError(t, repo.Create(ctx, newToken(userID, "stale-1", now.Add(-48*time.Hour))))
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "stale-2", now.Add(-time.Minute))))
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "live", now.Add(time.Hour))))

	removed, err := repo.DeleteExpired(ctx, now)
	testutil.NoError(t, err)
	testutil.Equal(t, removed, int64(2), "rows removed")

	live, err := repo.FindByTokenHash(ctx, "live")
	testutil.NoError(t, err)
	if live == nil {
		t.Errorf("an unexpired token must survive the sweep")
	}

	// Sweeping again removes nothing, so a janitor on a tick is harmless.
	removed, err = repo.DeleteExpired(ctx, now)
	testutil.NoError(t, err)
	testutil.Equal(t, removed, int64(0), "rows removed on the second sweep")
}
