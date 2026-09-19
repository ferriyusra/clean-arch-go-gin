# Testing Guide

Every layer of this project has a test harness, and each one answers a different
question. Picking the wrong harness is the usual reason a test suite is slow,
flaky, or green while the application is broken.

| Layer | Harness | Answers |
|---|---|---|
| `internal/service/**` | gomock + repository mocks | Is the business rule right? |
| `internal/repository/**` | real sqlite, in memory | Does the query map onto the contract? |
| `internal/api/handler/**`, `internal/api/middleware/**` | `httptest` + a real `gin.Engine` + service mocks | Is the HTTP contract right? |
| `internal/di` | the real container, in-memory database, nothing mocked | Is it wired together correctly? |

Shared helpers live in [`internal/testutil`](internal/testutil).

## Running tests

```bash
make test            # everything; works without a C toolchain
make test-race       # adds -race; needs CGO_ENABLED=1 and gcc/clang
make test-coverage   # writes coverage.out and coverage.html
go test -run TestLogin ./internal/service/user/   # a single test
```

`make test` deliberately omits `-race`. The race detector needs cgo and a C
compiler, which many Windows installations do not have, and a default target
that fails on a fresh clone trains people to ignore it. CI runs `-race` on Linux,
where it is free.

## Assertions

There is no testify. `internal/testutil` provides a handful of generic helpers:

```go
testutil.Equal(t, rec.Code, http.StatusOK, "status")
testutil.NoError(t, err)
testutil.ErrorIs(t, err, apperr.ErrUserNotFound)
testutil.True(t, token != "", "a token is issued")
```

They are about thirty lines and keep the module's test dependencies at zero. If
you would rather use testify, add it; nothing here depends on its absence.

**Assert on sentinels, not on message strings.** `err.Error() == "user already
exists"` breaks the moment anyone rewords the message, and it passes when a
different code path happens to produce the same text. `errors.Is` compares
identity:

```go
testutil.ErrorIs(t, err, apperr.ErrUserAlreadyExists)
```

## 1. Service tests — gomock

The service layer is where business rules live, so its tests mock the
repositories and assert on behaviour.

```go
deps := newTestDeps(t)
deps.users.EXPECT().FindByEmail(gomock.Any(), email).Return(nil, nil)

_, err := deps.service.Login(context.Background(), req)

testutil.ErrorIs(t, err, apperr.ErrInvalidCredentials)
```

`newTestDeps` (in `internal/service/user/user.service_test.go`) wires the mocks
once so each test states only the expectations that matter to it.

Note what is *not* mocked: the token service. It is pure and deterministic, so
mocking it would assert that the mock was called rather than that the code
works. Mock things that do I/O; use the real thing otherwise.

Every service method starts with a `ctx.Done()` guard, and every service has a
test that cancels the context and asserts the guard fires.

## 2. Repository tests — real sqlite, in memory

Repository tests use a genuine database because what they verify is exactly what
a mock cannot express: how GORM's results map onto the repository contract.

```go
db := testutil.NewDB(t)          // migrated, isolated, closed automatically
repo := user.NewGORMUserRepository(db)
```

Two details inside `testutil.NewDB` are load-bearing:

- **A bare `:memory:` DSN gives every connection its own empty database.** The
  pool migrates on one connection and queries on another, and the table appears
  to vanish. The fix is a uniquely named database with `cache=shared` plus
  `SetMaxOpenConns(1)`.
- **The name embeds the test name and a UUID**, so tests never see each other's
  rows.

The pragma syntax is glebarez/modernc's `_pragma=foreign_keys(1)`, not mattn's
`_foreign_keys=1`. Copying the wrong one silently disables foreign keys.

These tests are where behaviour that only a real driver shows up gets pinned:

- a missing row is `(nil, nil)`, not an error — the service layer depends on it;
- the unique index on `email` really does reject a duplicate, which is the ground
  truth behind the 409 that `POST /api/v1/auth/register` returns;
- a soft-deleted user still occupies that unique index, so their address cannot
  be registered again. `gorm.Config.TranslateError` is what turns that
  constraint violation into `gorm.ErrDuplicatedKey`, which is the only reason
  the service can tell a conflict from a server fault and answer 409.

**sqlite is not postgres.** These tests cover query shape and error mapping.
Behaviour that differs between engines needs a test against a real postgres —
`docker compose up postgres` provides one.

## 3. Handler and middleware tests — httptest with a real engine

Handler tests mount the handler on a real `gin.Engine`, behind the same
middleware production uses, and drive it with real HTTP requests. The paths
below are the test's own — a handler test mounts a bare route rather than
calling `SetupRoutes`, so they need not match the live `/api/v1/...` surface.
That is also the gap these tests cannot close: only the container tests in
`internal/di` prove a handler is reachable at the path it is meant to be.

```go
r := testutil.NewEngine(t)
r.POST("/api/auth/register", h.Register)
protected := r.Group("", middleware.AuthMiddleware(tokens))
protected.GET("/api/auth/me", h.GetMe)

rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", body))
testutil.Equal(t, rec.Code, http.StatusCreated, "status")
```

Use a real engine rather than `gin.CreateTestContext`, because the interesting
behaviour is the chain: that `CSRFMiddleware` aborts before `Logout` runs, that
`AuthMiddleware` populates the key `GetMe` reads. A hand-built context tests none
of it.

`gin.CreateTestContext` has exactly one good use here: unit-testing the context
extractors in `middleware/auth.go`, where there is no request at all.

Assert on the decoded envelope, never on raw JSON text:

```go
envelope := testutil.Envelope(t, rec)          // success, message, errors
user := testutil.DataAs[response.GetUser](t, rec)  // typed payload
cookies := testutil.Cookies(rec)               // by name
```

Cookie *properties* are worth asserting explicitly. `HttpOnly`, `Secure` and
`MaxAge` are what make the auth design safe, they are invisible in the response
body, and they are easy to break by accident.

## 4. End-to-end tests — the real container

`internal/di/container_test.go` builds the whole application against an
in-memory database and mocks nothing. It is the only place that can catch
wiring mistakes: a middleware that was never attached, a route registered on the
wrong group, a handler wired to the wrong service.

```go
container, err := di.NewContainer(cfg, nil)
rec := testutil.Do(container.Router, req)
```

It walks the real client journey — register, call a protected route, login,
refresh, logout, then confirm the revoked refresh token is rejected even though
its JWT has not expired.

Keep these few. They are the slowest tests and the least precise about *where* a
failure is.

## Regenerating mocks

```bash
make mocks   # repository + service mocks
```

`mockgen` is declared in `go.mod` as a tool dependency, so a fresh clone needs
nothing installed. Mocks are generated with `-typed`, which makes `.Return(...)`
compile-time checked.

CI runs `make mocks` and fails if anything changed, because a stale mock is a
silent source of green-but-wrong tests.

## Adding a feature, test first

1. `model/entity/<x>.go` and the table's entry in `platform/migrate.go`
2. `repository/interfaces/<x>.repository_interface.go`
3. `make mocks`
4. `repository/implementations/<x>/` plus a test against `testutil.NewDB`
5. `model/request/` and `model/response/` with `binding:` tags
6. `service/<x>/<action>.service_test.go` — a failing test first
7. `service/<x>/<action>.service.go`
8. `api/handler/<x>.go` plus a handler test
9. `api/router.go` — register the route, and add `middleware.CSRFMiddleware` for
   state-changing verbs
10. `di/container.go` — wire it up

Write the handler test against the status codes you *want*, not the ones the
code currently returns, and then make it pass.

## Tracing tests

`internal/tracing` is tested with an in-memory span exporter
(`tracetest.NewInMemoryExporter`) rather than a live collector, so the
assertions are about spans as data:

```go
exporter := tracetest.NewInMemoryExporter()
provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
otel.SetTracerProvider(provider)
// ... drive a request ...
spans := exporter.GetSpans()
```

`WithSyncer` rather than `WithBatcher` matters: the batch processor exports on a
timer, so a test would have to sleep or flush. The syncer exports on `span.End()`
and the assertion can run immediately.

Two of these tests are worth copying when you add instrumentation of your own:

- one asserts the database span is a **child of** the request span and shares its
  trace id, which is what makes database time attributable to a request. It
  fails if someone drops the `WithContext(ctx)` that carries the span;
- one scans every attribute of every span for a known email address. Query
  parameters carry emails, refresh tokens and bcrypt hashes, and the moment
  instrumentation starts recording them they are in a third-party system. That
  test is a security regression test, not a coverage exercise.

`t.Cleanup` restores the previous global tracer provider, since
`otel.SetTracerProvider` is process-wide and would otherwise leak into the rest
of the suite.
