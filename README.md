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
make bench            # benchmarks only, with -benchmem
make fuzz             # one fuzz target: PKG=... FUZZ=... FUZZTIME=30s
make vuln             # govulncheck, pulled on demand
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
│   ├── dbtx/             # carries a transaction through context.Context
│   └── mock/             # generated repository mocks (used by service tests)
│
├── model/
│   ├── entity/           # GORM models (UUID PKs, soft deletes)
│   ├── request/          # input DTOs with binding: validation tags
│   └── response/         # output DTOs + the APIResponse envelope
│
├── apperr/               # the application error type and its sentinels
├── logging/              # slog setup + context-scoped logger
├── tracing/              # OpenTelemetry setup, GORM spans, trace-id helpers
├── observability/        # Prometheus metrics + pprof, on a separate admin listener
├── platform/             # config + validation, database, versioned migrations
├── di/                   # dependency injection container
└── testutil/             # shared test harness (assertions, in-memory DB, HTTP)
```

## API Endpoints

Everything except the health probes lives under `/api/v1`, with no unversioned
aliases beside it. The health endpoints stay unversioned deliberately: they are a
contract with the orchestrator — liveness probes, load balancers, uptime monitors
— all of which live outside this repository and must not have to be edited in
lockstep with an `/api/v2`.

### Health (unversioned)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Readiness: checks the database, 503 when it is down |
| GET | `/api/health/live` | Liveness: is the process up (touches no dependency) |
| GET | `/api/health/ready` | Same as `/api/health` |

### Public

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/message` | Get message |
| GET | `/api/v1/csrf` | Get a CSRF token |
| POST | `/api/v1/auth/register` | Register a new user |
| POST | `/api/v1/auth/login` | Login |
| POST | `/api/v1/auth/refresh` | Rotate the token pair (requires CSRF) |

### Protected (requires the access-token cookie)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/auth/me` | Get the current user |
| GET | `/api/v1/auth/sessions` | List active sessions, paginated (`?page=&limit=`) |
| PATCH | `/api/v1/auth/password` | Change the password (requires CSRF) |
| DELETE | `/api/v1/auth/me` | Delete the account (requires CSRF) |
| POST | `/api/v1/auth/logout` | Logout (requires CSRF) |
| GET | `/api/v1/counter` | Get the counter value |
| POST | `/api/v1/counter` | Increment the counter (requires CSRF) |

Every response — success, error, 404, even a recovered panic — uses the same
envelope:

```json
{ "success": true,  "message": "User retrieved", "data": {} }
{ "success": true,  "message": "Sessions retrieved", "data": [], "meta": { "page": 1, "limit": 20, "total": 3 } }
{ "success": false, "message": "Email is already registered" }
{ "success": false, "message": "Validation failed", "errors": { "email": "Must be a valid email address" } }
```

Paginated endpoints read `?page=` and `?limit=` from the query string, default to
page 1 and 20 rows, and refuse a limit above 100 — an unbounded `limit=1000000`
is a denial of service that costs the caller one query string. The window that
was actually used is reported back in `meta`.

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
| `CodeTooManyRequests` | 429 |
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
it carries.

Refresh tokens are stored as **SHA-256 digests**, never in the clear, so a
leaked database dump contains nothing replayable. Rows are hard-deleted, because
a revoked credential that lingers is one that can be restored.

Every refresh **rotates the pair**: the presented token is deleted and a new
access and refresh token issued, which bounds a stolen refresh token to a single
request instead of seven days. A token that verifies as a JWT but has no row was
either revoked or already rotated — indistinguishable, and the second case is a
replay — so it is treated as theft and every session for that user is revoked.
Expired rows are swept hourly by a janitor.

Login costs the same whether the email exists or not: an unknown address is
compared against a dummy bcrypt hash rather than returning early, because the
difference between an instant reply and a ~60 ms one is enough to map which
addresses are registered no matter how identical the message is.

CSRF is stateless HMAC-SHA256 sent as `X-CSRF-Token`. The issue time is part of
the signed material and tokens expire after `CSRF_TOKEN_TTL`, since a stateless
token cannot be revoked once issued.

See [AUTH.md](AUTH.md) for the full reference.

## Observability

**Structured logging** with `log/slog` — JSON in production, text in
development. Every request gets a correlation id and a logger pre-tagged with
it, placed in the request `context.Context`. Since every service and repository
method already takes a `ctx`, any layer reaches the correlated logger without a
single signature change:

```go
logging.FromContext(ctx).Info("work happened")
```

Repositories and services return errors; handlers and middleware log them. One
log line per request, no duplicates.

**Tracing** with OpenTelemetry, off by default (`OTEL_ENABLED=true`). When on:

- every request becomes a span, and every database query a child span, so time
  spent in the database is attributable to the request that caused it;
- W3C `traceparent` is honoured on the way in and propagated on the way out, so
  a trace spans services rather than stopping here;
- a 5xx marks its span failed and attaches the cause — a 4xx does not, because
  telling a client no is a normal outcome and should not make every trace look
  broken;
- the request id *becomes* the trace id, so the log line, the error response and
  the span in Jaeger or Tempo all share one identifier.

```bash
docker compose --profile tracing up   # Jaeger at http://localhost:16686
OTEL_ENABLED=true OTEL_TRACES_EXPORTER=console make server   # or just print spans
```

The GORM instrumentation is written in-house
([`internal/tracing/gorm.go`](internal/tracing/gorm.go)) rather than taken from
`gorm.io/plugin/opentelemetry`, which links a ClickHouse driver and a
compression library into the binary — 24 extra packages — to name the database
system. Writing it out also makes it visible that **query variables are never
recorded**: the SQL text goes into the span, the bound parameters (emails,
refresh tokens, password hashes) never do. There is a test that fails if they
ever start to.

**Correlation in responses.** Error responses carry the identifiers so a user
can quote something actionable; successful responses stay lean, since the
`X-Request-ID` header already carries the same value.

```json
{
  "success": false,
  "message": "Internal server error",
  "requestId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

`traceId` is omitted entirely when the request is not part of a trace.

An incoming `X-Request-ID` is validated before it is trusted: at most 128
characters of `[A-Za-z0-9._-]`. Anything else is replaced with a fresh UUID
rather than cleaned up, because the value is echoed back and written to every
log line for the request.

**JSON naming.** Every response key is camelCase — `requestId`, never
`request_id`. A test in `internal/model/response` walks every response type and
fails on a key that breaks the rule, so it cannot drift. Log fields keep their
OpenTelemetry spelling (`trace_id`, `span_id`); the camelCase rule is about the
HTTP API, not about log records.

**Metrics and profiling** live in `internal/observability` and are served on a
**separate admin listener** (`127.0.0.1:9090` by default), never on the public
router: Prometheus metrics at `/metrics`, the stdlib pprof handlers under
`/debug/pprof/`, and a `/healthz` for the listener itself. Both are off by
default.

```bash
METRICS_ENABLED=true make server
curl http://127.0.0.1:9090/metrics
```

The separation is the point. `/debug/pprof` lets an unauthenticated caller dump
the heap, stall the process for a thirty-second CPU profile or read a full
goroutine dump, and `/metrics` leaks operational detail — route names, traffic
shape, build info. Neither belongs on a port the internet can reach, so outside
`DEV_MODE` startup refuses to bind pprof to a non-loopback `ADMIN_HOST`, and the
pprof handlers are mounted on a mux we own rather than inherited from
`net/http/pprof`'s `init()`, which would attach them to `http.DefaultServeMux`
invisibly.

The request counter and duration histogram are labelled by the gin *route
template*, not the request path; a request that matched no route is labelled
`unmatched`, because otherwise a stranger sending random URLs chooses our label
values and mints unbounded time series.

## Environment

Copy `env.example` to `.env`; it documents every variable. The important ones:

| Variable | Notes |
|---|---|
| `DEV_MODE` | `true` substitutes throwaway secrets, drops the cookie `Secure` flag, enables gin debug mode |
| `DATABASE_TYPE` | `sqlite` (default) or `postgres`; anything else is rejected at startup |
| `JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET`, `CSRF_SECRET` | required outside dev mode, minimum 32 characters, and the two JWT secrets must differ |
| `LOG_LEVEL`, `LOG_FORMAT` | `debug`/`info`/`warn`/`error`, `json`/`text` |
| `CSRF_TOKEN_TTL` | how long a CSRF token stays valid (default 12h) |
| `REFRESH_TOKEN_PURGE_INTERVAL` | how often expired refresh rows are swept; `0` disables it |
| `METRICS_ENABLED` | off by default; `true` serves `/metrics` on the admin listener |
| `PPROF_ENABLED` | off by default; leave it off unless someone is actively profiling |
| `ADMIN_HOST`, `ADMIN_PORT` | where the admin listener binds, default `127.0.0.1:9090`; the port must differ from `SERVER_PORT`, and outside dev mode pprof refuses a non-loopback host |
| `OTEL_ENABLED` | off by default; `true` turns on request and database spans |
| `OTEL_TRACES_EXPORTER` | `otlp` (a collector) or `console` (stdout, no collector needed) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | defaults to `http://localhost:4318` |
| `OTEL_TRACES_SAMPLER_ARG` | fraction of new traces kept, 0 to 1 |
| `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` | per-client-IP token bucket for the API |
| `AUTH_RATE_LIMIT_RPS`, `AUTH_RATE_LIMIT_BURST` | a separate, much tighter budget for the credential endpoints |
| `DATABASE_AUTO_MIGRATE` | run pending migrations at startup; set `false` in production |
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

Migrations are versioned and forward-only, in
[`internal/platform/migrate.go`](internal/platform/migrate.go). Each one is a Go
function with a version number, applied in order inside its own transaction
together with the `schema_migrations` row that records it, so a failure leaves
the database on the last version that fully succeeded.

Adding a change means appending a `Migration` to the list — never editing or
renumbering an existing one, because a version that has already run somewhere is
a fact. A startup check rejects duplicate or out-of-order versions, which is the
mistake two branches make when both add "the next" migration and are merged.

`AutoMigrate` is still used, but only *inside* migration 1, where creating
tables is all it has to do. It is additive: it cannot drop or rename a column,
which is why dropping the old plaintext refresh-token column had to be written
out as migration 3.

There are no down migrations. Reversing a schema change in production is nearly
always a restore or a new forward migration, and a down step that is never
exercised is a false sense of safety.

Concurrency is not coordinated: two instances booting against an empty database
at the same moment will both try, and the loser fails on the ledger primary key
and exits. That is safe but noisy. In production set `DATABASE_AUTO_MIGRATE=false`
and run migrations as their own step.

## Transactions

A service that must write more than one row wraps the work in
`TxManager.WithinTx`:

```go
err := s.txManager.WithinTx(ctx, func(ctx context.Context) error {
    if _, err := s.userRepository.Create(ctx, user); err != nil {
        return err
    }
    return s.storeToken(ctx, ...)   // joins the same transaction
})
```

The transaction travels in the `context.Context`, so repositories join it
without knowing it exists: each one resolves its connection through
`dbtx.Conn(ctx, r.db)` and gets either the ambient transaction or the pool.
Nothing in a repository signature changes, and the boundary stays in the
service that knows which writes belong together.

Nested calls reuse the outer transaction rather than opening a second one —
without that, sqlite, which allows a single writer, would block until the
deadline instead of failing.

Four methods use it. `Register` writes the account and its first refresh token
together — before, a failure at the second step left an account that existed but
could not sign in, and whose email was permanently claimed by the unique index.
`Refresh` retires the presented token and issues the new pair as one change of
state. `ChangePassword` stores the new hash and revokes every session in the same
transaction, because a window where the password has changed but the old sessions
survive is exactly what the user changed it to prevent. `DeleteAccount` removes
the sessions and the account, since either half alone is worse than neither.

A genuinely single statement is already atomic and is not wrapped; the
transaction is for work that has to be all-or-nothing, or for a read that decides
whether a write happens.

## Credits

- Originally inspired by [clean-go-vite-react](https://github.com/kamil5b/clean-go-vite-react) by [@kamil5b](https://github.com/kamil5b)
- Extracted from [clean-arch-go-vite-react](https://github.com/ferriyusra/clean-arch-go-vite-react)

## License

[MIT](LICENSE)
