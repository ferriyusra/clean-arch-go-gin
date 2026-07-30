# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make install-deps      # go mod tidy + download
make dev               # Start server with hot-reload (Air), sets DEV_MODE=true
make server            # Run server directly with DEV_MODE=true
make build             # Build static production binary to ./bin/
make test              # Run all tests with -v -cover -race
make test-verbose      # Run tests with -failfast
make test-coverage     # Generate HTML coverage report (coverage.html)
make fmt               # gofmt -w .
make fmt-check         # Fail if any file is not gofmt-clean
make vet               # go vet ./...
make lint              # golangci-lint run
make tidy-check        # Fail if go.mod/go.sum are not tidy
make check             # fmt-check + vet + lint + test — mirrors CI
make repository-mocks  # Regenerate gomock mocks from repository interfaces
make docker-build      # Build the production container image
make docker-up         # Start API + Postgres via docker compose
make clean             # Clean build artifacts and test cache
```

Run a single test: `go test -run TestRefresh ./internal/service/user/...`

Go module: `github.com/ferriyusra/boilerplate-golang-gin` (differs from the directory name).

Before declaring work done, run `make check` — CI runs the same gates plus a Docker build.

## Architecture

Clean Architecture with strict inward dependency flow:

**HTTP (api/) → Service (service/) → Repository (repository/) → Model (model/)**

- **api/handler/**: HTTP handlers. Bind + validate the request, call the service, map
  errors to a status. `errors.go` holds the sentinel → status table and the generic
  500 fallback; `validation.go` turns binding failures into field-level messages.
- **api/middleware/**: `auth.go` (JWT bearer + context helpers), `request_id.go`,
  `logger.go` (slog request logger + panic recovery), `rate_limit.go` (per-IP fixed
  window).
- **api/router.go**: Routes via `SetupRoutes(r, RouterDeps{...})` — health probes,
  rate-limited public auth routes, and protected routes behind `AuthMiddleware`.
- **service/**: Business logic, one package per domain with interface + implementation.
  `token` is standalone (no repository, configured via `TokenConfig`) and also owns
  `HashToken`. `user/errors.go` defines the sentinel errors handlers match on.
- **repository/interfaces/**: Repository contracts (`*_interface.go` files).
- **repository/implementations/**: GORM implementations, one package per domain.
  Constructors take a `*gorm.DB` and return the repository with **no error** — they
  do not migrate.
- **repository/mock/**: Auto-generated gomock mocks (regenerate with `make repository-mocks`).
- **model/entity/**: GORM database models (UUID PKs).
- **model/request/**: API input DTOs carrying `binding` validation tags.
- **model/response/**: API output DTOs + the `response.OK()`/`Err()`/`ValidationErr()` envelope.
- **di/container.go**: Validates secrets, builds the logger, opens + migrates the
  database, then wires repositories → services → handlers → router.
- **platform/**: `config.go` (env vars via `godotenv`), `database.go` (SQLite or
  Postgres, plus `PingDatabase`/`CloseDatabase`), `migrate.go`, `logger.go`.

Entry point: `cmd/server/main.go` → loads `.env` → builds the DI container → serves,
runs the refresh-token janitor, and shuts down gracefully closing the database.

API specification: `docs/openapi.yaml` — update it when routes or payloads change.

## Key Conventions

- **TDD workflow**: Write tests first, then implementation (see `internal/service/README.md`)
- **File naming**: `<action>.<layer>.go` (e.g. `login.service.go`, `find_by_id.gorm.go`)
- **Test naming**: `<action>.<layer>_test.go` alongside implementation files
- **Table-driven tests** with `gomock` for repository mocking
- **Context propagation**: All service and repository methods accept `context.Context`
- **Interface-based design**: Services depend on repository interfaces, never concrete types
- **DTOs**: Request models go in, Response models come out — entities stay in the repository layer
- **Imports**: three groups separated by blank lines — stdlib, third-party, then this module
- **Errors**: use `errors.Is`/`errors.As`, never `==`, on wrapped errors

### Error handling contract

Services return a **sentinel error** from `internal/service/user/errors.go` for every
expected failure, and wrap unexpected ones with `fmt.Errorf("context: %w", err)`.

Handlers must call `respondServiceError(c, err)` rather than answering with
`err.Error()`. Only sentinel messages reach the client; anything else is logged with
the request ID and answered as a generic 500. When you add an expected failure mode,
add a sentinel and an entry in `serviceErrorStatus` — do not return raw errors to
the client, as wrapped errors carry database and driver detail.

## Auth System

- JWT access tokens (15 min default) + refresh tokens (7 days default), both returned
  in the **response body**. No cookies, therefore no CSRF token.
- Access and refresh tokens are signed with **separate secrets**, so a refresh token
  cannot be replayed as an access token.
- Protected routes read `Authorization: Bearer <token>`.
- Refresh tokens are stored **only as SHA-256 hashes** (`token.HashToken`). Repository
  methods are named `*TokenHash` and callers hash before calling in — never persist or
  query a raw token.
- Refresh **rotates** and detects reuse: replaying a consumed token revokes every
  refresh token for that user. Logout revokes all of them too.
- Bcrypt password hashing, capped at 72 bytes because bcrypt ignores the remainder.
- Middleware context keys: `user_id` (string UUID), `user_email`, `claims` (`*token.TokenClaims`)
- Helper functions in middleware: `GetUserIDFromContext(c)`, `GetEmailFromContext(c)`,
  `GetClaimsFromContext(c)`, `GetRequestIDFromContext(c)`

## Database & Migrations

Schema lives in `platform.Migrate` (`internal/platform/migrate.go`), which runs at
startup when `DATABASE_AUTO_MIGRATE=true`. **Add every new entity to `migrationModels`
there** — repository constructors intentionally no longer call `AutoMigrate`.

AutoMigrate only adds columns and indexes; it never alters or drops them and has no
versioning. Production is expected to run `DATABASE_AUTO_MIGRATE=false` with a real
migration tool.

## Environment

Copy `env.example` to `.env` — it documents every variable and its default. Key vars:
- `DEV_MODE` — when true, uses fallback dev JWT secrets, Gin debug mode, and SQL query
  logging (never use in production)
- `DATABASE_TYPE` — `sqlite` (default) or `postgres`
- `DATABASE_DSN` — defaults to `dev.db` for SQLite
- `DATABASE_AUTO_MIGRATE` — defaults to true
- `JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET` — required when `DEV_MODE=false`; must be
  ≥32 chars and differ from each other, or the container refuses to start
- `JWT_ISSUER`, `JWT_ACCESS_EXPIRY`, `JWT_REFRESH_EXPIRY` — token settings
- `ALLOWED_ORIGINS` — comma-separated, defaults to `http://localhost:5173`
- `TRUSTED_PROXIES` — empty by default (trust none), so client IPs cannot be spoofed
  past the rate limiter
- `RATE_LIMIT_LOGIN_ATTEMPTS`, `RATE_LIMIT_LOGIN_WINDOW` — per-IP auth throttling
- `LOG_LEVEL` — `debug`/`info`/`warn`/`error`; defaults by `DEV_MODE`
