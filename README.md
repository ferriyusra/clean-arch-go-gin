# Clean Architecture Go API (Gin)

A production-ready Go backend built with **Clean Architecture** principles using
the **Gin** framework and **GORM** ORM, with a test harness for every layer.

This is the backend-only version extracted from
[clean-arch-go-vite-react](https://github.com/ferriyusra/clean-arch-go-vite-react),
with no frontend embedding or dependencies.

## Quick Start

```bash
make install-deps     # download dependencies
cp env.example .env   # configure
make dev              # run with hot-reload
```

The API is available at `http://localhost:8080`.

With Docker, including a postgres to develop against:

```bash
make docker-up        # docker compose up --build
```

> **Windows:** the recipes are POSIX shell, so run them from **Git Bash** or WSL,
> not cmd or PowerShell. If you do not have make yet:
>
> ```powershell
> winget install ezwinports.make
> ```
>
> That is a single self-contained `make.exe` (GNU Make 4.4.1, no admin rights and
> no DLLs). Open a new terminal afterwards so the updated PATH takes effect.

## Commands

```bash
make dev              # start with hot-reload (air)
make server           # run directly
make build            # build a stripped, version-stamped binary
make test             # all tests; works without a C toolchain
make test-race        # all tests with -race (needs CGO_ENABLED=1 + gcc/clang)
make test-coverage    # writes coverage.out and coverage.html
make lint             # golangci-lint
make fmt vet          # format, vet
make mocks            # regenerate repository + service mocks
make verify-mocks     # fail if the generated mocks are stale
make docker-up        # app + postgres via docker compose
make ci               # everything CI runs, locally
make clean            # remove build artifacts
```

`mockgen` and `air` are declared in `go.mod` as tool dependencies, so a fresh
clone needs nothing installed beyond Go itself.

## Project Structure

```
cmd/server/main.go        -> entry point: config, logger, container, HTTP server
internal/di/container.go  -> the single wiring point: repos -> services -> handlers -> routes
```

Dependencies flow inward only:

```
internal/
├── api/                  # HTTP layer
│   ├── handler/          # bind -> call service -> respond; Fail() maps errors to status
│   ├── middleware/       # request id, access log, recovery, security headers,
│   │                     # rate limit, body limit, timeout, JWT auth, CSRF
│   └── router.go         # SetupRoutes, SetupHealthRoutes, SetupFallbacks
│
├── service/              # business logic; depends on repository interfaces only
│   ├── user/ counter/ csrf/ token/ health/ message/
│   └── mock/             # generated service mocks (used by handler tests)
│
├── repository/
│   ├── interfaces/       # contracts (*.repository_interface.go)
│   ├── implementations/  # GORM implementations, one method per file
│   └── mock/             # generated repository mocks (used by service tests)
│
├── model/
│   ├── entity/           # GORM models (UUID PKs, soft deletes)
│   ├── request/          # input DTOs with binding: validation tags
│   └── response/         # output DTOs + the APIResponse envelope
│
├── apperr/               # the application error type and its sentinels
├── logging/              # slog setup + context-scoped logger
├── platform/             # config + validation, database, migrations
├── di/                   # dependency injection container
└── testutil/             # shared test harness (assertions, in-memory DB, HTTP)
```

## API Endpoints

### Public

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Readiness: checks the database, 503 when it is down |
| GET | `/api/health/live` | Liveness: is the process up (touches no dependency) |
| GET | `/api/health/ready` | Same as `/api/health` |
| GET | `/api/message` | Get message |
| GET | `/api/csrf` | Get a CSRF token |
| POST | `/api/auth/register` | Register a new user |
| POST | `/api/auth/login` | Login |
| POST | `/api/auth/refresh` | Refresh the access token (requires CSRF) |

### Protected (requires the access-token cookie)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/auth/me` | Get the current user |
| POST | `/api/auth/logout` | Logout (requires CSRF) |
| GET | `/api/counter` | Get the counter value |
| POST | `/api/counter` | Increment the counter (requires CSRF) |

Every response — success, error, 404, even a recovered panic — uses the same
envelope:

```json
{ "success": true,  "message": "User retrieved", "data": {} }
{ "success": false, "message": "Email is already registered" }
{ "success": false, "message": "Validation failed", "errors": { "email": "Must be a valid email address" } }
```

## Errors

Services return `*apperr.Error` sentinels; the handler layer maps them to status
codes in one place (`handler.Fail`). Callers match with `errors.Is`, never on
message strings.

| Sentinel kind | Status |
|---|---|
| `CodeInvalidInput` | 400 |
| `CodeUnauthorized` | 401 |
| `CodeForbidden` | 403 |
| `CodeNotFound` | 404 |
| `CodeConflict` | 409 |
| `CodeUnavailable` | 503 |
| `CodeTimeout` | 504 |
| anything else | 500 |

Anything that is not an `*apperr.Error` is treated as internal: the full chain
goes to the log, and the client gets a generic message. A wrapped driver error
can never reach a response body.

## Auth System

JWTs are delivered only as HttpOnly cookies — there is no `Authorization: Bearer`
path. Access tokens last 15 minutes, refresh tokens 7 days; both TTLs are
configurable and single-sourced, so a cookie max-age cannot drift from the token
it carries. Refresh tokens are also rows in the database, which is what lets
logout revoke them. CSRF is stateless HMAC-SHA256, sent as `X-CSRF-Token`.

See [AUTH.md](AUTH.md) for the full reference.

## Observability

Structured logging with `log/slog` — JSON in production, text in development.
Every request gets an `X-Request-ID` (propagated when the client sends one) and a
logger pre-tagged with it, placed in the request `context.Context`. Since every
service and repository method already takes a `ctx`, any layer reaches the
correlated logger without a single signature change:

```go
logging.FromContext(ctx).Info("work happened")
```

Repositories and services return errors; handlers and middleware log them. One
log line per request, no duplicates.

## Environment

Copy `env.example` to `.env`; it documents every variable. The important ones:

| Variable | Notes |
|---|---|
| `DEV_MODE` | `true` substitutes throwaway secrets, drops the cookie `Secure` flag, enables gin debug mode |
| `DATABASE_TYPE` | `sqlite` (default) or `postgres`; anything else is rejected at startup |
| `JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET`, `CSRF_SECRET` | required outside dev mode, minimum 32 characters, and the two JWT secrets must differ |
| `LOG_LEVEL`, `LOG_FORMAT` | `debug`/`info`/`warn`/`error`, `json`/`text` |
| `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` | per-client-IP token bucket |
| `TRUSTED_PROXIES` | empty means trust none, so `ClientIP` is the direct peer |

Configuration is validated at startup and **every** problem is reported at once,
so a new environment is fixed in one pass rather than one restart per mistake.

## Testing

Every layer has a harness, and each answers a different question:

| Layer | Harness |
|---|---|
| services | gomock + repository mocks |
| repositories | a real sqlite database, in memory |
| handlers and middleware | `httptest` + a real `gin.Engine` + service mocks |
| the whole application | the real DI container, nothing mocked |

```bash
make test
```

**[TESTING.md](TESTING.md) is the full guide** — how each harness works, the
traps in the in-memory sqlite setup, and why the suite asserts on error sentinels
rather than on message strings.

## Database Schema

There is no migration tool. `platform.Migrate` runs `AutoMigrate` for every
entity once at startup and seeds the demo rows; adding a table means adding it to
the list in [`internal/platform/migrate.go`](internal/platform/migrate.go).

`AutoMigrate` is additive only — it creates tables, columns and indexes but never
drops or rewrites them. A destructive change (renaming a column, backfilling
data) needs a real migration tool such as golang-migrate or goose alongside it.

## Credits

- Originally inspired by [clean-go-vite-react](https://github.com/kamil5b/clean-go-vite-react) by [@kamil5b](https://github.com/kamil5b)
- Extracted from [clean-arch-go-vite-react](https://github.com/ferriyusra/clean-arch-go-vite-react)

## License

[MIT](LICENSE)
