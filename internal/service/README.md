# Service Layer - Test-Driven Development Guide

This guide explains how to create and test services here, test first.

[CLAUDE.md](../../CLAUDE.md) has the rules in checklist form and
[TESTING.md](../../TESTING.md) describes all four test harnesses; this file is
the worked example for one of them.

## What a service is

The service layer is where business logic lives. Each service:

- takes `*request.X` and returns `*response.X` — entities stay below this layer
  and never reach a handler;
- depends on repository **interfaces** only, so it can be tested without a
  database;
- returns `*apperr.Error` values, which is how a failure becomes a status code
  without anyone matching on a message string;
- wraps multi-write and check-then-act work in `TxManager.WithinTx`;
- reads its logger out of the `context.Context` rather than taking one in its
  constructor.

A package per domain: `user/`, `counter/`, `message/`, `token/`, `csrf/`,
`health/`. The interface, the struct and the constructor live in
`<pkg>.service.go`; every method gets its own `<action>.service.go`. That file
naming is not cosmetic — `make mocks` finds service interfaces by looking for
exactly `internal/service/<pkg>/<pkg>.service.go`.

## TDD Workflow

### Step 1: Define the request and response models

See [`../model/README.md`](../model/README.md). Validation lives in `binding:`
tags, so a service never re-checks what the binding already rejected.

```go
// internal/model/request/user.go
type RegisterUserRequest struct {
	Email    string `json:"email"    binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Name     string `json:"name"     binding:"required,min=1,max=100"`
}

// internal/model/response/user.go
type RegisterResponse struct {
	User         GetUser `json:"user"`
	AccessToken  string  `json:"-"`
	RefreshToken string  `json:"-"`
}
```

Do this before writing the test: it is what tells you what the test can assert.

### Step 2: Write the test first

`<action>.service_test.go`, table-driven, one `t.Run` per case. The shape used
throughout this repo puts the mock expectations in the table rather than the
inputs, because what distinguishes one case from another is usually *which
repository calls happen*:

```go
func TestRegister(t *testing.T) {
	validRequest := &request.RegisterUserRequest{
		Email:    "test@example.com",
		Password: "password123",
		Name:     "Test User",
	}

	tests := []struct {
		name string
		// expect declares the repository interactions this case should produce.
		expect func(deps *testDeps)
		// wantErr is the sentinel the caller must be able to match with
		// errors.Is. nil means the call is expected to succeed.
		wantErr error
	}{
		{
			name: "registers a new user and issues tokens",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).Return(nil, nil)
				deps.users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&uuid.UUID{}, nil)
				deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "rejects an email that is already registered",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).
					Return(&entity.UserEntity{ID: uuid.New(), Email: validRequest.Email}, nil)
			},
			wantErr: apperr.ErrUserAlreadyExists,
		},
		{
			name: "reports a lookup failure as internal, not as a duplicate",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).
					Return(nil, errors.New("database error"))
			},
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			tt.expect(deps)

			result, err := deps.service.Register(context.Background(), validRequest)

			if tt.wantErr != nil {
				testutil.ErrorIs(t, err, tt.wantErr)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, result.User.Email, validRequest.Email, "email")
		})
	}
}
```

Four details in there are house style, and each has a reason:

- **`testutil.ErrorIs`, never `err.Error() == "..."`.** Asserting on a message
  means the test breaks when the wording improves and passes when the *kind* of
  error changes, which is exactly backwards. `internal/testutil/assert.go` holds
  the whole assertion vocabulary: `Equal`, `DeepEqual`, `True`, `NoError`,
  `Error`, `ErrorIs`. There is no testify here.
- **No `defer ctrl.Finish()`.** `gomock.NewController(t)` registers its own
  cleanup, so the expectations are verified automatically.
- **A `newTestDeps(t)` helper per package**, holding the mocks and the service.
  It keeps the table readable and means adding a constructor argument is one
  edit rather than one per test.
- **Real collaborators when they are pure.** `newTestDeps` in `service/user`
  builds a genuine `token.TokenService` rather than a mock: it is deterministic
  and does no I/O, so mocking it would only assert that the mock was called.

That helper, in full:

```go
func newTestDeps(t *testing.T) *testDeps {
	t.Helper()

	ctrl := gomock.NewController(t)
	users := mock.NewMockUserRepository(ctrl)
	refreshTokens := mock.NewMockRefreshTokenRepository(ctrl)
	tokens := token.NewTokenService(token.TokenConfig{...})

	return &testDeps{
		users:         users,
		refreshTokens: refreshTokens,
		tokens:        tokens,
		service:       NewUserService(users, refreshTokens, tokens, testutil.PassthroughTx{}, testRefreshTTL),
	}
}
```

Note `testutil.PassthroughTx{}` — see [Transactions](#transactions) below.

Run it and watch it fail. Then:

### Step 3: Define the interface and constructor

`<pkg>.service.go` holds the interface, the unexported struct and the
constructor, and nothing else:

```go
package counter

type CounterService interface {
	GetCounter(ctx context.Context) (*response.GetCounter, error)
	IncrementCounter(ctx context.Context) (*response.GetCounter, error)
}

type counterService struct {
	repo interfaces.CounterRepository
}

func NewCounterService(repo interfaces.CounterRepository) CounterService {
	return &counterService{repo: repo}
}
```

The constructor returns the *interface* here, unlike a repository constructor —
the difference is that handlers are wired against the interface and the mock is
generated from it, while a repository's only consumer is the container.

New repository dependency? Add it as a constructor parameter, never as a
package-level variable, and never as a `*gorm.DB`.

### Step 4: Implement one method per file

```go
// internal/service/counter/get_counter.service.go
func (s *counterService) GetCounter(ctx context.Context) (*response.GetCounter, error) {
	value, err := s.repo.GetCounter(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("reading counter: %w", err))
	}
	return &response.GetCounter{Value: value}, nil
}
```

### Step 5: Run the tests

```bash
go test ./internal/service/counter/ -v
go test -run TestRegister ./internal/service/user/
go test -cover ./internal/service/...
```

## Errors

A service's return value is the only thing that decides the status code a client
sees, so getting the classification right *here* is the whole point of
`internal/apperr`.

```go
// A condition the client is allowed to know about: return the sentinel.
if existingUser != nil {
	return nil, apperr.ErrUserAlreadyExists          // -> 409
}

// Anything unexpected: wrap it, so the cause reaches the log and not the client.
if err != nil {
	return nil, apperr.Internal(fmt.Errorf("checking existing user: %w", err))
}
```

- `handler.Fail` is the *only* place an error becomes a status code. A service
  never touches `net/http`.
- Anything that is not an `*apperr.Error` is treated as internal: the full chain
  is logged, the client gets "Internal server error". A raw repository error that
  slips through is therefore safe, but also useless to the client — classify it.
- `WithCause`, `WithFields` and `WithMessage` return *copies*, so deriving from a
  package-level sentinel never mutates it. `errors.Is` still matches the copy
  against the sentinel it came from.
- The sentinel list is `internal/apperr/sentinels.go`. Add to it rather than
  inventing an ad-hoc `New(...)` at a call site, so two services cannot describe
  the same condition two ways.

The judgement call is which conditions deserve their own sentinel and which
deliberately share one. `ChangePassword` answers a wrong current password and a
user row that no longer exists with the *same* `ErrInvalidCredentials`: saying
"no such user" there would turn a protected endpoint into an account-existence
oracle for anyone holding a stale token.

## Context

Every service method takes `ctx` first and passes it down unchanged.

Methods that do work of their own before reaching a repository open with the
cancellation guard — everything in `user/` and `health/` does — so that an
abandoned request stops immediately rather than after a round trip:

```go
select {
case <-ctx.Done():
	return nil, ctx.Err()
default:
}
```

Thin pass-throughs such as `counter.GetCounter` leave it out and rely on the
repository's own guard, but their tests still cover cancellation, by having the
mock return `context.Canceled`.

The **logger travels in the context**, never in a constructor:

```go
logging.FromContext(ctx).Warn("refresh token reuse detected, revoking every session for the user",
	"user_id", userID.String())
```

`middleware.RequestID` puts a request-scoped `*slog.Logger` there, already tagged
with the request id (and, when tracing is on, the trace id), so a log line from
four layers down still correlates with the response the user received. Because
every method already takes a `ctx`, this needs no signature changes anywhere.

Log inside a service only where the information would otherwise be lost — a
best-effort cleanup that failed, a security event like the reuse detection above.
Do not log an error you are also returning; the handler logs it once.

## Transactions

Wrap work in `WithinTx` when two or more writes must land together, or when a
read decides whether a write happens:

```go
var accessToken, refreshToken string
err = s.txManager.WithinTx(ctx, func(ctx context.Context) error {
	if _, createErr := s.userRepository.Create(ctx, userEntity); createErr != nil {
		return apperr.Internal(fmt.Errorf("creating user: %w", createErr))
	}

	var issueErr error
	accessToken, refreshToken, issueErr = s.issueTokens(ctx, user)
	return issueErr
})
if err != nil {
	return nil, err
}
```

**Use the `ctx` the callback gives you**, not the outer one — that is the value
carrying the transaction. Returning a non-nil error is what rolls back; returning
nil commits.

The four places that use it, and why:

| Method | What must be all-or-nothing |
|---|---|
| `Register` | the account row and its first refresh token. Without it, a failed token write left an account that could not sign in and whose email was permanently claimed by the unique index |
| `Refresh` | deleting the presented token and issuing the new pair — half of that leaves either two live tokens for one session or none |
| `ChangePassword` | the new hash and the revocation of every session. A window where the password changed but old sessions survive is the exact thing the user changed their password to prevent |
| `DeleteAccount` | the sessions and the account. Either half alone is worse than neither |

And what stays **outside** the transaction, deliberately: `ChangePassword`
verifies the old password and hashes the new one before opening it. Those are two
bcrypt operations, a few hundred milliseconds of CPU each, and holding a database
connection open across them would pin a connection per in-flight password change
for nothing — the check is not one a concurrent writer can invalidate in a way a
transaction would repair.

`ListSessions` does not wrap its count and its listing either: a session revoked
between the two makes the total briefly off by one, which is what any paginated
view of changing data looks like, and a transaction would buy consistency nobody
can observe at the cost of a connection held across two round trips.

In tests, `testutil.PassthroughTx{}` runs the callback directly. Service tests
mock their repositories, so there is no database to roll back and a real
transaction would only add noise to the expectations; that a rollback truly
removes the row is verified against a real database in
`internal/repository/implementations/tx/tx.gorm_test.go`.

## Pagination

List methods take a `request.Pagination` and return the metadata alongside the
rows:

```go
func (s *userService) ListSessions(
	ctx context.Context,
	userID uuid.UUID,
	currentRefreshToken string,
	page request.Pagination,
) (*response.SessionList, error) {
	window := page.Normalized()
	now := s.now()

	total, err := s.refreshTokenRepository.CountActiveByUserID(ctx, userID, now)
	// ...
	rows, err := s.refreshTokenRepository.ListActiveByUserID(ctx, userID, now, window.Limit, window.Offset())
	// ...

	return &response.SessionList{
		Sessions: sessions,
		Meta:     response.NewMeta(window.Page, window.Limit, total),
	}, nil
}
```

`Normalized()` applies the defaults (page 1, limit 20) and the ceiling (100);
`Offset()` does the arithmetic. Call `Normalized()` once and use the result — the
`Meta` must report the window that was actually used, not the one the client
asked for, or a client that sent `limit=5000` is told it received 5000 rows.

The handler turns that into the envelope with `handler.OKWithMeta`, which puts
`meta` beside `data` rather than inside it.

## Time

Anything that reasons about expiry takes its clock from a swappable field:

```go
type userService struct {
	// ...
	// now is swappable so tests can reason about expiry without sleeping.
	now func() time.Time
}
```

`NewUserService` sets it to `time.Now`; a test assigns something fixed. This is
also why the repository's "active" queries take a `now` parameter instead of
calling `time.Now()` in SQL — one clock, one place to move it.

## Services without repositories

Not every service has a store behind it. `token` and `csrf` are pure functions
over a secret and a TTL; `health` takes its dependency probes as a call argument
(`CheckWithDependencies(ctx, checks)`), which the container assembles once —
`"database": platform.PingCheck(db)` — and hands to the handler. They follow the
same file layout and the same mock generation, which is what lets a handler test
replace any of them.

Because they are pure, their tests are the fastest in the suite and they carry
the extras:

```bash
make bench     # go test -run '^$' -bench . -benchmem ./...
make fuzz      # one target per invocation; PKG=... FUZZ=... FUZZTIME=...
```

`FuzzValidate` (CSRF) and `FuzzValidateAccessToken` (token) exist because both
parse attacker-supplied text, and a crash on a malformed token is a denial of
service on a public endpoint. `internal/apperr`, `token` and `csrf` also carry
benchmarks — the error path runs on every rejected request, so an allocation
regression there is worth noticing.

Tests that own no shared state call `t.Parallel()` (on the parent test *and* on
each subtest). The service tests that use gomock with a per-case controller are
safe to parallelise; repository tests, which hold a single database connection
each, are not.

## Example package layout

```
internal/service/user/
├── user.service.go                 # interface, struct, constructor, issueTokens
├── user.service_test.go            # newTestDeps + constructor test
├── register.service.go
├── register.service_test.go
├── login.service.go
├── refresh.service.go
├── list_sessions.service.go
├── change_password.service.go
├── delete_account.service.go
├── store_refresh_token.service.go
├── revoke_refresh_tokens.service.go
└── purge_expired_refresh_tokens.service.go
```

## Troubleshooting

### "missing call(s) to ..." at the end of a test

An `EXPECT()` was declared but the code path never reached it — usually the case
under test returns earlier than the table assumes. Fix the expectation, not the
assertion: it is telling you the flow differs from what you believed.

### "there are no expected calls of the method ..."

The reverse: the service called a repository the case did not declare. Most often
a guard clause moved, or a new repository call was added to a method and the
older table rows were not updated.

### The test passes but the handler returns 500

The service returned a plain error instead of an `*apperr.Error`. Anything
unclassified maps to 500 with a generic message by design. Wrap it:
`apperr.Internal(fmt.Errorf("...: %w", err))`, or return the right sentinel.

### The mock is missing a method you just added

`make mocks`, then commit the regenerated files. CI runs `make verify-mocks` and
fails on stale ones.
