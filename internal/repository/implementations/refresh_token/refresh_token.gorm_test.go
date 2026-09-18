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

func newToken(userID uuid.UUID, value string) entity.RefreshTokenEntity {
	return entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     value,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
}

func TestRefreshTokenRepositoryCreateAndFind(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	userID := uuid.New()
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "token-a")))

	found, err := repo.FindByToken(ctx, "token-a")
	testutil.NoError(t, err)
	if found == nil {
		t.Fatalf("expected to find the token")
	}
	testutil.Equal(t, found.UserID, userID, "user id")
}

func TestRefreshTokenRepositoryMissingTokenIsNotAnError(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)

	found, err := repo.FindByToken(context.Background(), "never-issued")
	testutil.NoError(t, err)
	if found != nil {
		t.Errorf("expected nil token, got %+v", found)
	}
}

// TestRefreshTokenRepositoryDeleteByUserIDEndsEverySession is what makes logout
// mean "log out everywhere".
func TestRefreshTokenRepositoryDeleteByUserIDEndsEverySession(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	userID := uuid.New()
	otherUser := uuid.New()

	testutil.NoError(t, repo.Create(ctx, newToken(userID, "laptop")))
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "phone")))
	testutil.NoError(t, repo.Create(ctx, newToken(otherUser, "someone-else")))

	testutil.NoError(t, repo.DeleteByUserID(ctx, userID))

	for _, revoked := range []string{"laptop", "phone"} {
		found, err := repo.FindByToken(ctx, revoked)
		testutil.NoError(t, err)
		if found != nil {
			t.Errorf("token %q should have been revoked", revoked)
		}
	}

	// Another user's session must survive.
	survivor, err := repo.FindByToken(ctx, "someone-else")
	testutil.NoError(t, err)
	if survivor == nil {
		t.Errorf("another user's token must not be revoked")
	}
}

// TestRefreshTokenRepositoryDeleteIsIdempotent matters because logout may be
// called twice, and the second call must not fail.
func TestRefreshTokenRepositoryDeleteIsIdempotent(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	userID := uuid.New()
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "token-a")))

	testutil.NoError(t, repo.DeleteByUserID(ctx, userID))
	testutil.NoError(t, repo.DeleteByUserID(ctx, userID))
	testutil.NoError(t, repo.DeleteByToken(ctx, "never-existed"))
}

func TestRefreshTokenRepositoryDeleteByToken(t *testing.T) {
	db := testutil.NewDB(t)
	repo := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	userID := uuid.New()
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "laptop")))
	testutil.NoError(t, repo.Create(ctx, newToken(userID, "phone")))

	testutil.NoError(t, repo.DeleteByToken(ctx, "laptop"))

	gone, err := repo.FindByToken(ctx, "laptop")
	testutil.NoError(t, err)
	if gone != nil {
		t.Errorf("expected the token to be deleted")
	}

	kept, err := repo.FindByToken(ctx, "phone")
	testutil.NoError(t, err)
	if kept == nil {
		t.Errorf("deleting one session must not end the others")
	}
}
