package user_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	userRepo "github.com/ferriyusra/clean-arch-go-gin/internal/repository/implementations/user"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// These tests run against a real sqlite driver rather than a mock, because what
// they verify is precisely the part a mock cannot express: how GORM's results
// map onto the repository contract.
//
// Caveat: sqlite is not postgres. Query shape and error mapping are covered
// here; behaviour that differs between engines belongs in a test against a real
// postgres (see docker-compose.yml).

func newUser(email string) entity.UserEntity {
	return entity.UserEntity{
		ID:       uuid.New(),
		Email:    email,
		Password: []byte("hashed-password"),
		Name:     "Test User",
	}
}

func TestUserRepositoryCreateAndFind(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	created := newUser("test@example.com")
	id, err := repo.Create(ctx, created)
	testutil.NoError(t, err)
	testutil.Equal(t, *id, created.ID, "returned id")

	t.Run("finds by email", func(t *testing.T) {
		found, err := repo.FindByEmail(ctx, "test@example.com")
		testutil.NoError(t, err)
		if found == nil {
			t.Fatalf("expected to find the user")
		}
		testutil.Equal(t, found.ID, created.ID, "id")
		testutil.Equal(t, found.Name, "Test User", "name")
	})

	t.Run("finds by id", func(t *testing.T) {
		found, err := repo.FindByID(ctx, created.ID)
		testutil.NoError(t, err)
		if found == nil {
			t.Fatalf("expected to find the user")
		}
		testutil.Equal(t, found.Email, "test@example.com", "email")
	})
}

// TestUserRepositoryMissingRowsAreNotErrors documents the contract the service
// layer depends on: "no such user" is (nil, nil), not an error. Getting this
// wrong turns every anonymous login attempt into a 500.
func TestUserRepositoryMissingRowsAreNotErrors(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	t.Run("by email", func(t *testing.T) {
		found, err := repo.FindByEmail(ctx, "nobody@example.com")
		testutil.NoError(t, err)
		if found != nil {
			t.Errorf("expected nil user, got %+v", found)
		}
	})

	t.Run("by id", func(t *testing.T) {
		found, err := repo.FindByID(ctx, uuid.New())
		testutil.NoError(t, err)
		if found != nil {
			t.Errorf("expected nil user, got %+v", found)
		}
	})
}

// TestUserRepositoryRejectsDuplicateEmail exercises the uniqueIndex on Email,
// which is the ground truth behind the 409 the register endpoint returns.
func TestUserRepositoryRejectsDuplicateEmail(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	_, err := repo.Create(ctx, newUser("taken@example.com"))
	testutil.NoError(t, err)

	_, err = repo.Create(ctx, newUser("taken@example.com"))
	testutil.Error(t, err, "second insert with the same email")
}

func TestUserRepositoryUpdate(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	created := newUser("test@example.com")
	_, err := repo.Create(ctx, created)
	testutil.NoError(t, err)

	created.Name = "Renamed User"
	testutil.NoError(t, repo.Update(ctx, created.ID, created))

	found, err := repo.FindByID(ctx, created.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, found.Name, "Renamed User", "name after update")
}

// TestUserRepositorySoftDeleteKeepsTheEmailReserved records a real trap: the
// entity uses gorm.DeletedAt, so a deleted row still occupies the unique index
// and its address cannot be registered again.
func TestUserRepositorySoftDeleteKeepsTheEmailReserved(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	created := newUser("test@example.com")
	_, err := repo.Create(ctx, created)
	testutil.NoError(t, err)

	testutil.NoError(t, repo.Delete(ctx, created.ID))

	found, err := repo.FindByID(ctx, created.ID)
	testutil.NoError(t, err)
	if found != nil {
		t.Errorf("a soft-deleted user should not be found, got %+v", found)
	}

	_, err = repo.Create(ctx, newUser("test@example.com"))
	testutil.Error(t, err, "re-registering a soft-deleted email")
}

func TestUserRepositoryHonoursContextCancellation(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.FindByEmail(ctx, "test@example.com")
	testutil.ErrorIs(t, err, context.Canceled)
}
