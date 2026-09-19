# Repository Layer - How-To Guide

This guide explains how to create and implement repositories in this project.
Repositories handle all data access and are the bridge between the service layer
and the database.

[CLAUDE.md](../../CLAUDE.md) has the checklist version of this; what follows is
the worked example and the reasoning. [TESTING.md](../../TESTING.md) covers the
test harnesses in full.

## The rule that costs the most to learn the hard way

**Every repository method reaches the database through `dbtx.Conn(ctx, r.db)`,
never through `r.db` directly.**

```go
// ✅ joins whatever transaction the caller opened, or uses the pool if there is none
dbtx.Conn(ctx, r.db).Create(&user)

// ❌ compiles, passes its own test, and silently escapes the caller's transaction
r.db.WithContext(ctx).Create(&user)
```

The second form is not a style problem. A service wraps two writes in
`TxManager.WithinTx` precisely so that neither can survive without the other; a
method that goes to `r.db` directly gets its own connection, commits on its own,
and is *not* rolled back when the unit of work fails. Registration then leaves an
account with no session, whose email is already claimed by the unique index, so
the owner can neither sign in nor register again — which is the bug the
transaction existed to prevent.

It survives review easily because it looks correct, and it passes its own test
because a repository test has no ambient transaction, so both forms behave
identically there. The place it shows up is production, once.

`dbtx.Conn` already applies `WithContext`, so there is no second call to make:

```go
// internal/repository/dbtx/dbtx.go
func Conn(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if tx, ok := From(ctx); ok {
		return tx.WithContext(ctx)
	}
	return fallback.WithContext(ctx)
}
```

The transaction travels in the `context.Context` under an unexported key, which
is what lets a repository stay unaware of transactions entirely: no signature
mentions one, no constructor takes one, and the boundary stays in the service
that knows which writes belong together.

## What a repository owes its caller

- **Entities in, entities out.** `request` / `response` DTOs never appear here;
  they belong to the service layer and above.
- **Plain errors, not `apperr`.** A repository wraps its cause with context
  (`fmt.Errorf("listing active refresh tokens for user %s: %w", ...)`) and
  returns it. Turning a failure into something a client should see is the
  service's job, through a sentinel or `apperr.Internal(...)`.
- **A missing row is `(nil, nil)`, not an error.** `FindByEmail` on an unknown
  address returns no user and no error; the service decides whether that means
  401, 404, or "fine, carry on". Returning `gorm.ErrRecordNotFound` instead turns
  every login attempt with an unknown email into a 500. There is a test named for
  this contract in `implementations/user/user.gorm_test.go`.
- **A context check first.** Every method opens with the `select` guard, so an
  already-cancelled request never reaches the database.
- **No migrations.** Constructors used to call `AutoMigrate`; they no longer do.
  See [Schema changes](#schema-changes).

## Repository Creation Workflow

### Step 1: Create the entity

Entity models live in `internal/model/entity/<name>.go`, use a UUID primary key,
and name their table explicitly.

```go
package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserEntity represents a user in the system
type UserEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	Email     string    `gorm:"uniqueIndex"`
	Password  []byte
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UserEntity) TableName() string {
	return "user_entities"
}
```

**Key points:**

- `uuid.UUID` for primary keys, never an auto-increment int.
- `CreatedAt` / `UpdatedAt` on everything.
- `gorm.DeletedAt` **only where a soft delete is what you want.**
  `RefreshTokenEntity` deliberately has none: a soft-deleted row still occupies
  its slot in the unique index on the digest, and a revoked credential that
  lingers in the table is one that can be restored. Revocation has to be a real
  delete.
- An explicit `TableName()`, so renaming the Go struct is not a schema change.

Then register the table — see [Schema changes](#schema-changes).

### Step 2: Create the repository interface

`internal/repository/interfaces/<name>.repository_interface.go` holds the
contract. This is what services depend on and what the mocks are generated from.

```go
package interfaces

import (
	"context"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/google/uuid"
)

// UserRepository defines the interface for user data access
type UserRepository interface {
	Create(ctx context.Context, user entity.UserEntity) (*uuid.UUID, error)
	FindByID(ctx context.Context, id uuid.UUID) (*entity.UserEntity, error)
	FindByEmail(ctx context.Context, email string) (*entity.UserEntity, error)
	Update(ctx context.Context, id uuid.UUID, user entity.UserEntity) error
	Delete(ctx context.Context, id uuid.UUID) error
}
```

**Design guidelines:**

- `context.Context` first, always.
- Name parameters for what they really are. Every `RefreshTokenRepository` method
  that identifies a token takes a `tokenHash`, not a `token` — naming it for the
  digest is what stops a raw token being passed in by accident.
- Only the methods that exist. A speculative `FindAll` is an untested query plus
  one more mock method for every test that touches the repository.
- Document what a signature cannot say: what "active" means, what a count counts,
  whether a missing row is an error.

### Step 3: Generate mocks

```bash
make mocks
```

The Makefile derives the destination from the file name: it strips the
`_interface` suffix, so `user.repository_interface.go` becomes
`internal/repository/mock/user.repository_mock.go`. Mocks are generated with
`-typed`, so `EXPECT()` gives typed arguments and `Return` values rather than
`interface{}`.

The directory is deleted and regenerated wholesale, and `make verify-mocks`
(which CI runs) fails if the checked-in mocks differ from freshly generated ones.
**Never hand-edit anything under `mock/`.**

### Step 4: Implement with GORM

**4a — the struct and constructor**, in
`internal/repository/implementations/user/user.gorm.go`:

```go
package user

import (
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// GORMUserRepository is a GORM implementation of UserRepository
type GORMUserRepository struct {
	db *gorm.DB
}

// UserModel represents the users table schema
type UserModel = entity.UserEntity

// NewGORMUserRepository creates a new GORM user repository.
//
// The schema is applied once at startup by platform.Migrate, not here: a
// constructor that migrates makes the schema a side effect of wiring and runs
// DDL four times on every boot.
func NewGORMUserRepository(db *gorm.DB) *GORMUserRepository {
	return &GORMUserRepository{
		db: db,
	}
}
```

Three things here differ from older code in this repo, deliberately:

- **The constructor returns the concrete type, not the interface.** Returning an
  interface hides the type from its own package's tests and buys nothing: the DI
  container is the only caller, and it passes the value straight into a service
  parameter typed as `interfaces.UserRepository`, which is where the compiler
  checks the implementation anyway.
- **It does not return an error,** because nothing in it can fail any more. Each
  call site in `di/container.go` is one line.
- **It does not migrate.**

**4b — one file per method.**
`internal/repository/implementations/user/create.gorm.go`:

```go
package user

import (
	"context"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// Create creates a new user in GORM
func (r *GORMUserRepository) Create(ctx context.Context, user entity.UserEntity) (*uuid.UUID, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if err := dbtx.Conn(ctx, r.db).Create(&user).Error; err != nil {
		return nil, err
	}

	return &user.ID, nil
}
```

A read, with the not-found contract:

```go
func (r *GORMUserRepository) FindByEmail(ctx context.Context, email string) (*entity.UserEntity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var user entity.UserEntity
	if err := dbtx.Conn(ctx, r.db).Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // "no such user" is not a failure
		}
		return nil, err
	}

	return &user, nil
}
```

A partial update — note `Model(...).Where(...).Updates(...)` rather than `Save`,
so the fields the caller left at their zero value are untouched instead of being
written back as empty:

```go
func (r *GORMUserRepository) Update(ctx context.Context, id uuid.UUID, user entity.UserEntity) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return dbtx.Conn(ctx, r.db).
		Model(&UserModel{}).Where("id = ?", id).
		Updates(&user).
		Error
}
```

That is what lets `ChangePassword` pass `entity.UserEntity{Password: hashed}` and
change exactly one column.

### Step 5: Test against a real database

Repository tests run against real sqlite, in memory, because what they verify is
exactly the part a mock cannot express: how GORM's results map onto the contract.

```go
package user_test

func TestUserRepositoryMissingRowsAreNotErrors(t *testing.T) {
	db := testutil.NewDB(t)
	repo := userRepo.NewGORMUserRepository(db)
	ctx := context.Background()

	found, err := repo.FindByEmail(ctx, "nobody@example.com")
	testutil.NoError(t, err)
	testutil.True(t, found == nil, "no user")
}
```

`testutil.NewDB(t)` hands back a private, named in-memory database, migrated with
the real `platform.Migrate` and capped at one open connection. Two details there
are load-bearing and easy to get wrong; they are documented in
`internal/testutil/db.go`, and worth reading before touching the DSN.

These tests do not call `t.Parallel()`: each holds a single connection and there
is nothing to win. The pure in-process tests (token, csrf, counter, apperr) do.

Caveat worth repeating: sqlite is not postgres. Query shape and error mapping are
covered in memory; behaviour that genuinely differs between engines needs a test
against the postgres in `docker-compose.yml`.

## Transactions

A service that writes more than one row, or does a check-then-act, wraps the
work:

```go
// internal/service/user/register.service.go
err = s.txManager.WithinTx(ctx, func(ctx context.Context) error {
	if _, createErr := s.userRepository.Create(ctx, userEntity); createErr != nil {
		return apperr.Internal(fmt.Errorf("creating user: %w", createErr))
	}

	var issueErr error
	accessToken, refreshToken, issueErr = s.issueTokens(ctx, user)
	return issueErr
})
```

Both repository calls receive the `ctx` handed to the callback, so both join the
same transaction — provided every method they run goes through `dbtx.Conn`.

The pieces:

| File | What it is |
|---|---|
| `interfaces/tx.repository_interface.go` | `TxManager.WithinTx(ctx, fn)`, the contract a service depends on |
| `implementations/tx/tx.gorm.go` | the GORM implementation |
| `dbtx/dbtx.go` | `Into` / `From` / `Conn`, the context plumbing |
| `mock/tx.repository_mock.go` | generated, like any other repository mock |
| `testutil.PassthroughTx` | runs the callback with no transaction, for service tests |

Facts worth knowing:

- **The callback's error is the signal.** Return non-nil and the transaction rolls
  back; return nil and it commits. Nothing else rolls one back.
- **Nested calls reuse the outer transaction** rather than opening a second one.
  Without that, a service composing two transactional operations would block
  until the deadline on sqlite, which allows a single writer.
- **A genuinely single statement is already atomic and is not wrapped.**
  `IncrementCounter` needs a transaction because it is a read-modify-write
  (`SELECT ... FOR UPDATE` then `UPDATE`); `DeleteExpired` is one `DELETE` and
  needs nothing.
- **Service tests use `testutil.PassthroughTx`,** which runs the unit of work
  directly. A mocked transaction can only show that a function was called;
  whether a rollback actually removes the row is a property of the database, so
  it is tested where it can be tested honestly — in
  `implementations/tx/tx.gorm_test.go`, against a real one.

## Schema changes

`AutoMigrate` on every boot is gone. The schema is now a versioned, forward-only
ledger in [`internal/platform/migrate.go`](../platform/migrate.go): a
`schema_migrations` table plus an ordered `[]Migration`, each applied inside its
own transaction together with the row that records it, so a failure leaves the
database on the last version that fully succeeded.

Adding a table means two edits in that file:

1. add the entity to `entities()`;
2. **append** a `Migration` with the next version — never edit or renumber an
   existing one. A version that has already run somewhere is a fact, and changing
   it means that database and a fresh one no longer agree. A startup check
   rejects duplicate or out-of-order versions, which is the mistake two branches
   make when both add "the next" migration and are merged.

`AutoMigrate` does still run, but only inside migration 1, where creating tables
is all it has to do. It is additive only — it cannot drop or rename a column,
which is why removing the old plaintext refresh-token column had to be written
out by hand as migration 3.

`DATABASE_AUTO_MIGRATE` controls whether `di.NewContainer` applies pending
migrations at startup. Set it to `false` in production and run them as their own
step: startup migration is not coordinated between instances, so two booting at
once both try and the loser fails on the ledger's primary key.

## File layout

```
internal/repository/
├── dbtx/                             # the transaction-in-context plumbing
│   └── dbtx.go
├── interfaces/
│   ├── counter.repository_interface.go
│   ├── message.repository_interface.go
│   ├── refresh_token.repository_interface.go
│   ├── tx.repository_interface.go
│   └── user.repository_interface.go
├── implementations/
│   ├── counter/
│   │   ├── counter.gorm.go           # struct + constructor
│   │   ├── get_counter.gorm.go
│   │   └── increment_counter.gorm.go
│   ├── message/
│   ├── refresh_token/
│   │   ├── refresh_token.gorm.go
│   │   ├── create.gorm.go
│   │   ├── find_by_token_hash.gorm.go
│   │   ├── list_active_by_user_id.gorm.go
│   │   ├── count_active_by_user_id.gorm.go
│   │   ├── delete_by_token_hash.gorm.go
│   │   ├── delete_by_user_id.gorm.go
│   │   └── delete_expired.gorm.go
│   ├── tx/
│   │   └── tx.gorm.go
│   └── user/
└── mock/                             # generated; never hand-edited
    ├── counter.repository_mock.go
    ├── message.repository_mock.go
    ├── refresh_token.repository_mock.go
    ├── tx.repository_mock.go
    └── user.repository_mock.go
```

One method per file, named after the method: `increment_counter.gorm.go`,
`find_by_token_hash.gorm.go`. The struct and its constructor live in
`<package>.gorm.go`. Tests sit next to what they test, as `*.gorm_test.go`.

## Common Patterns

### Query with a filter

```go
func (r *GORMMessageRepository) GetMessage(ctx context.Context, key string) (*string, error) {
	var message entity.MessageEntity
	if err := dbtx.Conn(ctx, r.db).Where("key = ?", key).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &message.Value, nil
}
```

### Pagination: a window and a count, with identical predicates

The real pair is `ListActiveByUserID` / `CountActiveByUserID`. The repository
takes `limit, offset` — plain integers, already normalised by
`request.Pagination` up in the service — rather than a page number, because
offset arithmetic belongs in one place and that place is the DTO.

```go
func (r *GORMRefreshTokenRepository) ListActiveByUserID(
	ctx context.Context, userID uuid.UUID, now time.Time, limit, offset int,
) ([]entity.RefreshTokenEntity, error) {
	var tokens []entity.RefreshTokenEntity
	if err := dbtx.Conn(ctx, r.db).
		Where("user_id = ? AND expires_at > ?", userID, now).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Offset(offset).
		Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("listing active refresh tokens for user %s: %w", userID, err)
	}
	return tokens, nil
}
```

Two things there are worth copying:

- **The count must use the same `WHERE` as the listing.** A total computed from a
  different predicate gives a page count that does not match the rows, which
  users see as a final page that is empty.
- **Order by something unique.** `created_at DESC, id DESC` — with `created_at`
  alone, two rows written in the same clock tick can swap places between
  requests, and a client paging through them sees one row twice and another not
  at all.

Note also that "now" is a parameter rather than `time.Now()` inside the query.
The service owns a single, swappable notion of the current time, which is what
lets its tests reason about expiry without sleeping.

### Atomic read-modify-write

```go
func (r *GORMCounterRepository) IncrementCounter(ctx context.Context) (int, error) {
	var counter CounterModel

	if err := dbtx.Conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&counter).Error; err != nil {
			return err
		}
		counter.Value++
		return tx.Save(&counter).Error
	}); err != nil {
		return 0, err
	}

	return counter.Value, nil
}
```

Starting from `dbtx.Conn` matters here too: inside a caller's transaction GORM
turns this inner one into a savepoint, so the method still behaves when a service
calls it from within `WithinTx`.

### Reporting how much work was done

```go
result := dbtx.Conn(ctx, r.db).
	Where("expires_at < ?", before).
	Delete(&entity.RefreshTokenEntity{})
if result.Error != nil {
	return 0, fmt.Errorf("deleting expired refresh tokens: %w", result.Error)
}
return result.RowsAffected, nil
```

`RowsAffected` is what lets the janitor log "swept 412 expired refresh tokens"
rather than "swept some".

## Troubleshooting

### Mock generation failed

```bash
go tool mockgen --version      # declared in go.mod; nothing to install
ls internal/repository/interfaces/
make mocks
```

If CI reports stale mocks, the fix is `make mocks` plus committing the result,
never editing the generated file.

### A write escaped (or survived) its caller's transaction

Look for `r.db` where `dbtx.Conn(ctx, r.db)` belongs:

```bash
grep -rn 'r\.db' internal/repository/implementations/
```

Every hit should be inside a `dbtx.Conn(ctx, r.db)` call or the constructor.

### A query returns nothing after a delete

`UserEntity` carries `gorm.DeletedAt`, so `Delete` is a soft delete and every
later query filters the row out by default. That is intentional: deleting an
account keeps the row but makes it invisible to the lookups behind login and
refresh. Reach for `Unscoped()` only when you genuinely mean to see, or really
remove, the row.

### Context not propagating

`dbtx.Conn` applies `WithContext` for you. If a query ignores cancellation, the
likely cause is a `*gorm.DB` captured outside the request — a package-level
handle, or a `tx` stored on a struct.
