package tx_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	refreshRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/refresh_token"
	txRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/tx"
	userRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/user"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// These run against a real database on purpose. A mocked transaction manager
// can only show that a function was called; whether the rollback actually
// removes the row is a property of the database.

func newUser(email string) entity.UserEntity {
	return entity.UserEntity{
		ID:       uuid.New(),
		Email:    email,
		Password: []byte("hashed"),
		Name:     "Test User",
	}
}

func countUsers(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var count int64
	testutil.NoError(t, db.Model(&entity.UserEntity{}).Count(&count).Error)
	return count
}

func TestWithinTxCommitsOnSuccess(t *testing.T) {
	db := testutil.NewDB(t)
	manager := txRepo.NewGORMTxManager(db)
	users := userRepo.NewGORMUserRepository(db)

	err := manager.WithinTx(context.Background(), func(ctx context.Context) error {
		_, createErr := users.Create(ctx, newUser("committed@example.com"))
		return createErr
	})

	testutil.NoError(t, err)
	testutil.Equal(t, countUsers(t, db), int64(1), "users after a successful unit of work")
}

func TestWithinTxRollsBackOnError(t *testing.T) {
	db := testutil.NewDB(t)
	manager := txRepo.NewGORMTxManager(db)
	users := userRepo.NewGORMUserRepository(db)

	wantErr := errors.New("something went wrong afterwards")

	err := manager.WithinTx(context.Background(), func(ctx context.Context) error {
		if _, createErr := users.Create(ctx, newUser("rolled-back@example.com")); createErr != nil {
			return createErr
		}
		return wantErr
	})

	testutil.ErrorIs(t, err, wantErr)
	testutil.Equal(t, countUsers(t, db), int64(0), "users after a failed unit of work")

	// The address has to be free again, or the rollback would have left the
	// unique index claimed by a row that no longer exists.
	_, createErr := users.Create(context.Background(), newUser("rolled-back@example.com"))
	testutil.NoError(t, createErr)
}

// TestRegisterRollsBackTheAccountWhenTheTokenFails is the scenario the
// transaction exists for. Before it, a failure here left an account that could
// not sign in and whose email was permanently taken.
func TestRegisterRollsBackTheAccountWhenTheTokenFails(t *testing.T) {
	db := testutil.NewDB(t)
	manager := txRepo.NewGORMTxManager(db)
	users := userRepo.NewGORMUserRepository(db)
	tokens := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	// Occupy the digest so the second write inside the unit of work collides.
	testutil.NoError(t, tokens.Create(ctx, entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		TokenHash: "already-taken",
		ExpiresAt: time.Now().Add(time.Hour),
	}))

	err := manager.WithinTx(ctx, func(ctx context.Context) error {
		if _, createErr := users.Create(ctx, newUser("register@example.com")); createErr != nil {
			return createErr
		}
		return tokens.Create(ctx, entity.RefreshTokenEntity{
			ID:        uuid.New(),
			UserID:    uuid.New(),
			TokenHash: "already-taken",
			ExpiresAt: time.Now().Add(time.Hour),
		})
	})

	testutil.Error(t, err, "the colliding token insert")
	testutil.Equal(t, countUsers(t, db), int64(0), "the account must not survive a failed registration")
}

// TestWithinTxReusesAnOuterTransaction matters more than it looks: sqlite
// allows one writer, so opening a second transaction from inside the first
// would block until the deadline rather than fail loudly.
func TestWithinTxReusesAnOuterTransaction(t *testing.T) {
	db := testutil.NewDB(t)
	manager := txRepo.NewGORMTxManager(db)
	users := userRepo.NewGORMUserRepository(db)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := manager.WithinTx(ctx, func(outer context.Context) error {
		if _, createErr := users.Create(outer, newUser("outer@example.com")); createErr != nil {
			return createErr
		}

		return manager.WithinTx(outer, func(inner context.Context) error {
			_, createErr := users.Create(inner, newUser("inner@example.com"))
			return createErr
		})
	})

	testutil.NoError(t, err)
	testutil.Equal(t, countUsers(t, db), int64(2), "both writes commit together")
}

// A nested failure must take the outer work down with it, because the inner
// call joined the outer transaction rather than starting its own.
func TestWithinTxNestedFailureRollsBackEverything(t *testing.T) {
	db := testutil.NewDB(t)
	manager := txRepo.NewGORMTxManager(db)
	users := userRepo.NewGORMUserRepository(db)

	err := manager.WithinTx(context.Background(), func(outer context.Context) error {
		if _, createErr := users.Create(outer, newUser("outer@example.com")); createErr != nil {
			return createErr
		}

		return manager.WithinTx(outer, func(context.Context) error {
			return errors.New("inner failed")
		})
	})

	testutil.Error(t, err, "the nested failure")
	testutil.Equal(t, countUsers(t, db), int64(0), "the outer write is rolled back too")
}

// Repositories called outside any WithinTx must still work: most reads are not
// part of a unit of work.
func TestRepositoriesWorkWithoutATransaction(t *testing.T) {
	db := testutil.NewDB(t)
	users := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	_, err := users.Create(ctx, newUser("plain@example.com"))
	testutil.NoError(t, err)

	found, err := users.FindByEmail(ctx, "plain@example.com")
	testutil.NoError(t, err)
	if found == nil {
		t.Fatalf("expected to find the user written outside a transaction")
	}
}

// TestChangePasswordRollsBackWhenTheReissueFails is the atomicity proof for the
// password change, made against a real transaction.
//
// The service tests cannot make it: they wire testutil.PassthroughTx, which
// runs the closure with no transaction at all, so they can only show that an
// error propagates — not that the password write was undone. Since the whole
// argument for wrapping the change is "a state where the password has changed
// but the old sessions survive must not exist", that state deserves a test
// against a database that can actually roll back.
//
// The failure is injected the way it would really happen: the reissued refresh
// token collides with a digest already in the table.
func TestChangePasswordRollsBackWhenTheReissueFails(t *testing.T) {
	db := testutil.NewDB(t)
	manager := txRepo.NewGORMTxManager(db)
	users := userRepo.NewGORMUserRepository(db)
	tokens := refreshRepo.NewGORMRefreshTokenRepository(db)
	ctx := context.Background()

	user := newUser("rollback-pw@example.com")
	originalPassword := user.Password
	userID, err := users.Create(ctx, user)
	testutil.NoError(t, err)

	// A live session, plus an occupied digest for the reissue to collide with.
	testutil.NoError(t, tokens.Create(ctx, entity.RefreshTokenEntity{
		ID: uuid.New(), UserID: *userID,
		TokenHash: "original-session", ExpiresAt: time.Now().Add(time.Hour),
	}))
	testutil.NoError(t, tokens.Create(ctx, entity.RefreshTokenEntity{
		ID: uuid.New(), UserID: uuid.New(),
		TokenHash: "collides", ExpiresAt: time.Now().Add(time.Hour),
	}))

	// The same three steps ChangePassword performs, in the same order.
	txErr := manager.WithinTx(ctx, func(ctx context.Context) error {
		if updateErr := users.Update(ctx, *userID, entity.UserEntity{
			Password: []byte("a-brand-new-hash"),
		}); updateErr != nil {
			return updateErr
		}
		if revokeErr := tokens.DeleteByUserID(ctx, *userID); revokeErr != nil {
			return revokeErr
		}
		return tokens.Create(ctx, entity.RefreshTokenEntity{
			ID: uuid.New(), UserID: *userID,
			TokenHash: "collides", ExpiresAt: time.Now().Add(time.Hour),
		})
	})
	testutil.Error(t, txErr, "the reissue collides, so the unit of work fails")

	// The password must be the old one. If it were not, the user would be
	// holding a password that works nowhere.
	stored, err := users.FindByID(ctx, *userID)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, stored.Password, originalPassword, "the password change was rolled back")

	// And the session must be back: a revocation that survives a failed change
	// would sign the user out for nothing.
	total, err := tokens.CountActiveByUserID(ctx, *userID, time.Now())
	testutil.NoError(t, err)
	testutil.Equal(t, total, int64(1), "the original session was restored by the rollback")
}
