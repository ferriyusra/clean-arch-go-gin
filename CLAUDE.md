# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make dev               # Start server with hot-reload (air via `go tool`, DEV_MODE=true)
make server            # Run server directly (DEV_MODE=true)
make build             # Build ./bin/server-$GOOS-$GOARCH (-trimpath, -ldflags version stamp)
make test              # go test -cover ./...            (no cgo needed)
make test-race         # CGO_ENABLED=1 go test -race ./...
make test-coverage     # coverage.out + coverage.html, with -coverpkg=./...
make lint              # golangci-lint run ./...
make mocks             # Regenerate BOTH repository and service mocks
make verify-mocks      # Fail if generated mocks are stale (CI runs this)
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
  middleware is attached **per route**, not globally.
- **service/**: one package per domain; depends on repository *interfaces* only.
- **repository/interfaces/**: contracts, named `<domain>.repository_interface.go`.
- **repository/implementations/<domain>/**: GORM impls, one method per file.
- **repository/mock/** and **service/mock/**: generated, never hand-edit.
- **model/**: `entity/` (GORM, UUID PKs, `gorm.DeletedAt`), `request/` (input DTOs
  with `binding:` tags), `response/` (output DTOs + `APIResponse` in `wrapper.go`).
- **apperr/**: the `*apperr.Error` type plus the sentinels every layer shares.
- **logging/**: `slog` setup and the context-scoped logger accessor.
- **platform/**: `config.go` (env parsing), `config_validate.go` (startup
  validation), `database.go` (dialector, pool, ping/retry, close, health probe),
  `migrate.go` (schema + seed).
- **di/container.go**: the single wiring point — validates config, builds the gin
  engine and its middleware chain, opens and migrates the DB, constructs repos →
  services → handlers → routes.
- **testutil/**: shared test harness (assertions, in-memory DB, HTTP helpers).

Entry point: `cmd/server/main.go` → godotenv → `platform.NewConfig()` →
`logging.New()` → `di.NewContainer()` → `http.Server`, shut down on
SIGINT/SIGTERM with `cfg.Server.ShutdownTimeout` and `container.Close()`.

Cross-cutting facts that are not visible from a single file:

- **No migration tool.** `platform.Migrate(db)` runs `AutoMigrate` for every
  entity and seeds the counter/message rows. Repository constructors no longer
  migrate and no longer return an error. Adding a table means adding it to
  `entities()` in `internal/platform/migrate.go`.
- **The container can reuse an injected DB**: it only calls
  `platform.InitializeDatabase` when `cfg.Database.Gorm` is nil, and
  `Container.Close()` then leaves that injected handle alone. Tests rely on this.
- **Every service method starts with a `select { case <-ctx.Done(): ... }` guard**,
  and every service has a test that cancels the context.
- **The logger travels in `context.Context`**, never in constructor arguments:
  `middleware.RequestID` puts a request-scoped `*slog.Logger` there and any layer
  reads it with `logging.FromContext(ctx)`.
- **Errors**: services return `*apperr.Error` sentinels, or wrap an internal
  cause with `apperr.Internal`. Never compare error strings; use `errors.Is`.
  Anything that is not an `*apperr.Error` becomes a 500 with a generic message.

## Adding a Feature (TDD order)

1. `model/entity/<x>.go` — GORM entity, plus its entry in `platform/migrate.go`
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
- Refresh tokens are JWTs **and** rows in the DB, so they can be revoked.
  `Refresh` validates the JWT, then requires a matching unexpired row. Logout
  deletes rows via `RevokeRefreshTokens` and always succeeds.
- CSRF is stateless HMAC-SHA256: `hex(nonce).hex(hmac(nonce))`, validated by
  recomputing the MAC. Clients fetch one from `GET /api/csrf`.
- `DEV_MODE=true` substitutes throwaway secrets, drops the cookie `Secure` flag
  and keeps gin in debug mode. With it off, `Config.Validate` requires all three
  secrets, each at least 32 characters, with the two JWT secrets different.

## Environment

Copy `env.example` to `.env`; it documents every variable. `Config.Validate()`
runs inside `di.NewContainer` and reports every problem at once.

## Doc Accuracy

`README.md`, `TESTING.md`, `AUTH.md` and `env.example` match the code.
`internal/README.md` and the per-layer READMEs predate the error, logging and
testing work and have drifted — treat `internal/api/router.go` and the code as
the source of truth; those READMEs are useful for their worked examples only.

## Claude Code Hooks in This Repo

`.claude/settings.json` registers two hooks, and **both are broken**:

- `read_hook.js` (PreToolUse on Read/Grep) blocks reading any `.yaml` file
  except `docs/openapi.yaml`, which does not exist. It now also blocks reading
  `.golangci.yml`, `docker-compose.yml` and `.github/workflows/ci.yml`. Read
  those with `cat` via Bash, or remove the hook.
- `update-openapi-hook.sh` (PostToolUse on edits) execs
  `.claude/scripts/update-openapi.sh`, which is not in the repo, and matches
  paths (`controllers/`, `routers/`, `models/`, `services/`) that do not exist in
  this layout. It fires on writes to `cmd/server/main.go` and fails.

Both are leftovers from the fullstack repo this was extracted from and are safe
to delete.
