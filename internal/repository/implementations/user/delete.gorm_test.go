package user_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	userRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/user"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// TestDeletedUserCannotBeFoundByAnyLookup is the ground truth behind "a deleted
// account cannot sign in".
//
// UserEntity carries gorm.DeletedAt, so Delete is a soft delete and the row
// survives. What makes that safe is GORM's default scope: every query the
// application issues excludes soft-deleted rows, so FindByEmail — the first
// thing Login does — reports the account as missing and Login answers with the
// same invalid-credentials error it gives an unknown address. The same holds
// for FindByID, which is what GetUser and ChangePassword call.
//
// If the soft delete were ever swapped for a query that used Unscoped, this
// test is what would fail; the login path itself would still look correct.
func TestDeletedUserCannotBeFoundByAnyLookup(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	deleted := newUser("deleted@example.com")
	_, err := repo.Create(ctx, deleted)
	testutil.NoError(t, err)

	testutil.NoError(t, repo.Delete(ctx, deleted.ID))

	byEmail, err := repo.FindByEmail(ctx, "deleted@example.com")
	testutil.NoError(t, err)
	if byEmail != nil {
		t.Errorf("a deleted account must not be findable by email, got %+v", byEmail)
	}

	byID, err := repo.FindByID(ctx, deleted.ID)
	testutil.NoError(t, err)
	if byID != nil {
		t.Errorf("a deleted account must not be findable by id, got %+v", byID)
	}

	// The row really is still there, which is what "soft delete" means and why
	// the email stays reserved. Stating it here keeps the trade-off visible
	// rather than leaving it to be rediscovered by a user who cannot re-register.
	var rows int64
	testutil.NoError(t, db.Unscoped().Model(&entity.UserEntity{}).
		Where("id = ?", deleted.ID).Count(&rows).Error)
	testutil.Equal(t, rows, int64(1), "the soft-deleted row survives")
}

// TestDeletingOneAccountLeavesOthersAlone guards against a Delete that matches
// on the wrong column.
func TestDeletingOneAccountLeavesOthersAlone(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	target := newUser("target@example.com")
	bystander := newUser("bystander@example.com")
	_, err := repo.Create(ctx, target)
	testutil.NoError(t, err)
	_, err = repo.Create(ctx, bystander)
	testutil.NoError(t, err)

	testutil.NoError(t, repo.Delete(ctx, target.ID))

	survivor, err := repo.FindByID(ctx, bystander.ID)
	testutil.NoError(t, err)
	if survivor == nil {
		t.Fatalf("deleting one account must not delete another")
	}

	// Deleting an id that is not there is not an error: a retried request and a
	// double click both have to succeed.
	testutil.NoError(t, repo.Delete(ctx, uuid.New()))
}
