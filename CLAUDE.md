# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make dev               # Start server with hot-reload (air via `go tool`, DEV_MODE=true)
make server            # Run server directly (DEV_MODE=true)
make build             # Build ./bin/server-$GOOS-$GOARCH (-trimpath, -ldflags version stamp)
make test              # go test -cover ./...            (no cgo needed)
make test-race         # CGO_ENABLED=1 go test -race -cover ./... (needs gcc)
make test-coverage     # coverage.out + coverage.html, with -coverpkg=./...
make lint              # golangci-lint run ./...
make mocks             # Regenerate BOTH repository and service mocks
make verify-mocks      # Fail if generated mocks are stale (CI runs this)
make vuln              # govulncheck over ./... (via go run, no global install)
make bench             # go test -bench . -benchmem ./...
make fuzz              # Short fuzz pass: make fuzz PKG=./internal/service/csrf FUZZ=FuzzValidate
make ci                # tidy-check + vet + verify-mocks + test-race
make clean             # Remove ./bin, coverage files, test cache
```

Run a single test / package:

```bash
go test -run TestRefresh ./internal/service/user/
go test -cover ./internal/service/token/
```

Environment gotchas on this machine:

- `make` is GNU Make 4.4.1, installed with `winget install ezwinports.make`.
  It installs under `$LOCALAPPDATA/Microsoft/WinGet/Packages/ezwinports.make_*/bin`,
  which winget added to the user PATH — a terminal opened before that install
  will not see it. Every target has been run from Git Bash and works, including
  the shell loops in `mocks` and `SHELL := /usr/bin/env bash`.
- There is no gcc, so `make test-race` fails here with `cgo: C compiler "gcc"
  not found`. `make test` deliberately omits `-race` for this reason;
  CI runs the race detector on Linux.
- Makefile recipes are POSIX shell — run them from Git Bash/WSL, not cmd/PowerShell.
- `mockgen` and `air` are declared in `go.mod` under the `tool` directive, so
  `go tool mockgen` and `go tool air` work with no global install.

## Architecture

Clean Architecture, dependencies point inward only:

**api/ (HTTP) → service/ (business logic) → repository/ (data access) → model/entity (GORM)**

- **api/handler/**: bind request DTO via `BindJSON`, call service, respond with
  `OK(...)` or `Fail(...)`. `respond.go` is the only place an error becomes a
  status code.
- **api/middleware/**: `RequestID`, `AccessLog`, `Recovery`, `SecurityHeaders`,
  `BodyLimit`, `RateLimit`, `Timeout`, plus `AuthMiddleware`,
  `OptionalAuthMiddleware`, `CSRFMiddleware` and the `Get*FromContext` extractors.
- **api/router.go**: `SetupRoutes`, `SetupHealthRoutes`, `SetupFallbacks`. CSRF
  middleware is attached **per route**, not globally. The API lives under
  `/api/v1`; health stays unversioned at `/api/health*` because it is an
  orchestrator contract, not public API, and versioning it would mean editing
  every probe on a version bump. There are no `/api/...` aliases — a test
  asserts the unversioned surface 404s.
- **service/**: one package per domain; depends on repository *interfaces* only.
- **repository/interfaces/**: contracts, named `<domain>.repository_interface.go`.
- **repository/implementations/<domain>/**: GORM impls, one method per file.
- **repository/mock/** and **service/mock/**: generated, never hand-edit.
- **model/**: `entity/` (GORM, UUID PKs, `gorm.DeletedAt`), `request/` (input DTOs
  with `binding:` tags), `response/` (output DTOs + `APIResponse` in `wrapper.go`).
- **apperr/**: the `*apperr.Error` type plus the sentinels every layer shares.
- **logging/**: `slog` setup and the context-scoped logger accessor.
- **tracing/**: OpenTelemetry setup, the in-house GORM span plugin, and the
  trace-id helpers. Off unless `OTEL_ENABLED=true`.
- **repository/dbtx/**: carries the ambient transaction in `context.Context`;
  `dbtx.Conn(ctx, r.db)` is how every repository gets its connection.
- **platform/**: `config.go` (env parsing), `config_validate.go` (startup
  validation), `database.go` (dialector, pool, ping/retry, close, health probe),
  `migrate.go` (the versioned migration ledger).
- **di/container.go**: the single wiring point — validates config, builds the gin
  engine and its middleware chain, opens and migrates the DB, constructs repos →
  services → handlers → routes.
- **observability/**: Prometheus metrics and `net/http/pprof`, served on their
  own admin listener (`ADMIN_HOST`/`ADMIN_PORT`, loopback by default) and
  deliberately never on the public router. Off unless `METRICS_ENABLED` or
  `PPROF_ENABLED`.
- **testutil/**: shared test harness (assertions, in-memory DB, HTTP helpers).

Entry point: `cmd/server/main.go` → godotenv → `platform.NewConfig()` →
`logging.New()` → `di.NewContainer()` → `http.Server`, shut down on
SIGINT/SIGTERM with `cfg.Server.ShutdownTimeout` and `container.Close()`.

Cross-cutting facts that are not visible from a single file:

- **Migrations are versioned and forward-only.** `platform.Migrate(db)` applies
  the ordered `migrations()` list in `internal/platform/migrate.go`, each in its
  own transaction with its `schema_migrations` ledger row. Append a new
  `Migration`; never edit or renumber an existing one. `AutoMigrate` survives
  only inside migration 1, because it cannot drop or rename anything. Startup
  validates the list and rejects duplicate or out-of-order versions.
  `DATABASE_AUTO_MIGRATE=false` skips the run so production can migrate as a
  separate step.
- **Multi-row writes go through `TxManager.WithinTx`.** The transaction rides in
  the `context.Context` and repositories pick it up through `dbtx.Conn(ctx,
  r.db)`, so no repository signature mentions it. Nested calls reuse the outer
  transaction rather than opening a second one, which sqlite would block on.
  Four call sites rely on it: `Register`, `Refresh`, `ChangePassword` and
  `DeleteAccount`. A single-statement write is not wrapped — it is already
  atomic and the transaction would only buy a round trip.
- **The container can reuse an injected DB**: it only calls
  `platform.InitializeDatabase` when `cfg.Database.Gorm` is nil, and
  `Container.Close()` then leaves that injected handle alone. Tests rely on this.
- **Service methods that do real work start with a `select { case <-ctx.Done(): ... }`
  guard.** Thin pass-throughs such as `counter.GetCounter` and `message.GetMessage`
  leave it out and rely on the repository's own guard. Either way every service
  has a test that cancels the context — that part has no exceptions.
- **The logger travels in `context.Context`**, never in constructor arguments:
  `middleware.RequestID` puts a request-scoped `*slog.Logger` there and any layer
  reads it with `logging.FromContext(ctx)`.
- **Errors**: services return `*apperr.Error` sentinels, or wrap an internal
  cause with `apperr.Internal`. Never compare error strings; use `errors.Is`.
  Anything that is not an `*apperr.Error` becomes a 500 with a generic message.

- **Tracing is opt-in and correlated with the logs.** `middleware.RequestID`
  adopts the W3C trace id as the request id when a span exists, so one
  identifier covers the log line, the error body and the span. `handler.Fail`
  marks the span failed on 5xx only. The GORM plugin records SQL text but never
  bound parameters, and a test enforces that.
- **Response JSON is camelCase.** `internal/model/response/naming_test.go` walks
  every response type and fails on a key that breaks the rule; a new response
  struct has to be added to its `responseTypes()` list to be covered. Log fields
  keep OpenTelemetry spelling (`trace_id`, `span_id`) and are not affected.
- **Pagination is a query-string DTO, not ad-hoc parameters.** `request.Pagination`
  binds with `ShouldBindQuery`, defaults to page 1 / limit 20, and caps limit at
  100 — without the ceiling a client asks for a million rows and the database
  does the work. Paginated endpoints answer with `response.OKWithMeta`.
- **Metrics and pprof are never on the public router.** They get their own
  listener, built in `di.NewContainer` and started by `Container.StartAdmin`.
  `Config.Validate` refuses to bind pprof to a non-loopback address outside
  DEV_MODE: it hands an unauthenticated caller a heap dump and a 30s CPU stall.
- **The metrics middleware sits OUTSIDE `Recovery`, and the order is load-bearing.**
  A panic unwinds through every `c.Next()` above it, and the middleware records
  after its own `c.Next()` with no `defer` — so from inside `Recovery` a
  panicked request is counted zero times. A `defer` would not fix it either: it
  runs during unwinding, before `Recovery` writes the 500, and would record 200.
  `TestPanickedRequestsAreCounted` pins this.
- **Both metric labels are bounded.** `route` uses `c.FullPath()` with
  `"unmatched"` as the fallback, and `method` is whitelisted to the standard
  verbs with `"other"` for the rest — `net/http` accepts any HTTP token as a
  method, so without that an anonymous caller mints unbounded time series.
- **`r.HandleMethodNotAllowed = true`** in `newRouter`. gin leaves it off, which
  makes `SetupFallbacks`' `NoMethod` handler dead code and answers a wrong verb
  with 404 — telling a client the path does not exist when only the verb is wrong.
- **`gorm.Config.TranslateError` is on**, in production and in `testutil.NewDB`
  alike. It is what turns a unique-index violation into `gorm.ErrDuplicatedKey`
  so a service can answer 409 instead of 500. Turning it off silently converts
  conflicts into server errors.

## Adding a Feature (TDD order)

1. `model/entity/<x>.go` — GORM entity, plus a new `Migration` appended to
   `migrations()` in `platform/migrate.go` (and the entity added to `entities()`)
2. `repository/interfaces/<x>.repository_interface.go` — contract
3. `make mocks` — regenerate repository **and** service mocks
4. `repository/implementations/<x>/` — `<x>.gorm.go` (struct + constructor) then
   one file per method, plus a test against `testutil.NewDB`
5. `model/request/` + `model/response/` — DTOs with `binding:` validation tags
6. `service/<x>/<action>.service_test.go` — failing table-driven test with gomock
7. `service/<x>/<x>.service.go` (interface + struct + constructor) and
   `<action>.service.go` per method
8. `api/handler/<x>.go` — handler, plus a handler test using the service mock
9. `api/router.go` — register route (public vs `protected`, add
   `middleware.CSRFMiddleware` for state-changing verbs)
10. `di/container.go` — add repo, service, handler, and pass to `SetupRoutes`

Longer walkthroughs: `TESTING.md` (all four test harnesses),
`internal/service/README.md`, `internal/repository/README.md`, `internal/model/README.md`.

## Conventions

- **File naming**: `<domain>.<layer>.go` holds the interface/struct/constructor;
  `<action>.<layer>.go` holds one method each (`get_counter.service.go`,
  `increment_counter.gorm.go`). Methods hang off the unexported struct in the
  same package.
- **Tests**: `<action>.<layer>_test.go` next to the implementation; table-driven
  with a `tests := []struct{...}` slice and `t.Run`. The gomock controller
  registers its own cleanup, so `defer ctrl.Finish()` is not needed.
- **Assertions**: `internal/testutil` helpers, not testify. Assert on sentinels
  with `testutil.ErrorIs`, not on `err.Error()` strings.
- **Signatures**: services take `*request.X` and return `*response.X`; entities
  stay below the service layer and never reach handlers.
- **Errors**: `apperr` sentinels for anything a client should see;
  `apperr.Internal(fmt.Errorf("doing thing: %w", err))` for internal failures.
- Mock file names are derived by the Makefile:
  `<name>.repository_interface.go` → `repository/mock/<name>_mock.go`, and
  `service/<pkg>/<pkg>.service.go` → `service/mock/<pkg>.service_mock.go`.

## Auth System

`AUTH.md` has the full reference. The load-bearing details:

- Access and refresh JWTs are delivered **only** as HttpOnly cookies
  `access_token` / `refresh_token`. There is no `Authorization: Bearer` path.
- TTLs come from `JWT_ACCESS_TTL` / `JWT_REFRESH_TTL` (defaults 15m / 168h) and
  are single-sourced: the same values sign the tokens and set the cookie
  max-age, so the two can no longer drift.
- `UserService.Register` and `Login` mint **and persist** the token pair; the
  handler only moves them into cookies.
- Refresh tokens are JWTs **and** rows in the DB, but only a SHA-256 digest is
  stored, never the token. `Refresh` validates the JWT, then requires a matching
  unexpired row, then **rotates**: the presented token is deleted and a new pair
  issued. A token that verifies but has no row is treated as a replay and
  revokes every session for that user. Each refresh token carries a random JWT
  ID, without which two tokens minted in the same second would be identical and
  rotation would be a no-op. Expired rows are swept by a janitor started from
  `Container.StartJanitor`. Logout deletes rows and always succeeds.
- CSRF is stateless HMAC-SHA256:
  `hex(nonce).hex(issuedAt).hex(hmac(nonce+issuedAt))`, validated by recomputing
  the MAC. The issue time is inside the signed material, so it cannot be edited
  to extend a captured token, and it is only read *after* the MAC verifies.
  Tokens expire after `CSRF_TOKEN_TTL` (default 12h) because a stateless token
  cannot be revoked. Clients fetch one from `GET /api/v1/csrf`.
- `DEV_MODE=true` substitutes throwaway secrets, drops the cookie `Secure` flag
  and keeps gin in debug mode. With it off, `Config.Validate` requires all three
  secrets, each at least 32 characters, with the two JWT secrets different.

## Environment

Copy `env.example` to `.env`; it documents every variable. `Config.Validate()`
runs inside `di.NewContainer` and reports every problem at once.

## Doc Accuracy

`README.md`, `TESTING.md`, `AUTH.md`, `env.example`, `internal/README.md`, the
per-layer READMEs (`internal/model`, `internal/repository`, `internal/service`)
and `docs/openapi.yaml` all match the code.

The per-layer READMEs are the worked-example companions to this file, not
duplicates of it: `internal/repository/README.md` for `dbtx.Conn`, the
`TxManager` and the migration ledger; `internal/service/README.md` for the TDD
loop, `apperr` classification and `WithinTx`; `internal/model/README.md` for
DTOs, the response envelope and pagination.

The code is still the authority when they disagree: `internal/api/router.go` for
routes, `internal/platform/migrate.go` for the schema, `internal/platform/config.go`
for environment variables. Changing any of those three means updating the docs
in the same commit — `docs/openapi.yaml` included, since nothing generates it.

## Claude Code Configuration in This Repo

`.claude/settings.json` carries a permission allowlist and nothing else. The two
hooks it used to register were leftovers from the fullstack repo this was
extracted from and have been deleted: `read_hook.js` blocked reading every
`.yaml` file except a `docs/openapi.yaml` that did not exist at the time, and
`update-openapi-hook.sh` exec-ed a script that was never in the repo. There is
no `.claude/hooks/` directory any more, and YAML files are read normally.

`.claude/settings.local.json` is per-developer and gitignored. It used to be
committed, carrying `yarn install` and `tsc` permissions from that same
fullstack repo; if you need local overrides, create it and it will stay out of
the history.
