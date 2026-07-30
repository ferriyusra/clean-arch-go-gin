# Go Clean Architecture Boilerplate (Gin)

A production-oriented starting point for a Go HTTP API: **Gin** for routing, **GORM**
for persistence, JWT bearer authentication with rotating refresh tokens, and Clean
Architecture layering with unit tests at every layer.

Backend only — no frontend embedding or dependencies.

## Quick Start

```bash
make install-deps     # Install dependencies
cp env.example .env   # Configure (works as-is for local dev)
make dev              # Start with hot-reload (air) on :8080
```

`make dev` sets `DEV_MODE=true`, which falls back to built-in JWT secrets and a
local SQLite file (`dev.db`), so there is nothing to configure to get started. The
server refuses to boot in that state once `DEV_MODE=false` — see
[Production checklist](#production-checklist).

Verify it is up:

```bash
curl localhost:8080/api/health
curl localhost:8080/api/health/ready   # also checks the database
```

### Using this as a template

Rename the module to your own path before writing code:

```bash
NEW_MODULE=github.com/you/your-service
grep -rl 'github.com/ferriyusra/boilerplate-golang-gin' --include='*.go' --include='*.md' . \
  | xargs sed -i '' "s|github.com/ferriyusra/boilerplate-golang-gin|$NEW_MODULE|g"
sed -i '' "s|^module .*|module $NEW_MODULE|" go.mod
go mod tidy && make check
```

Then update `formatters.settings.goimports.local-prefixes` in [.golangci.yml](.golangci.yml)
and `JWT_ISSUER` in your `.env`. (Drop the `''` after `-i` on GNU sed.)

## Commands

```bash
make dev              # Hot-reload server (air), DEV_MODE=true
make server           # Run directly, DEV_MODE=true
make build            # Static production binary into ./bin/
make test             # All tests, with -race and coverage
make test-coverage    # Writes coverage.html
make fmt              # gofmt -w .
make lint             # golangci-lint
make check            # fmt-check + vet + lint + test — what CI runs
make repository-mocks # Regenerate mocks after changing a repository interface
make docker-up        # API + Postgres via docker compose
make help             # Full list
```

Run one test: `go test -run TestRefresh ./internal/service/user/...`

## API

Full specification: [docs/openapi.yaml](docs/openapi.yaml).

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/api/health` | — | Liveness; touches no dependencies |
| GET | `/api/health/ready` | — | Readiness; 503 if the database is down |
| POST | `/api/auth/register` | — | Create an account |
| POST | `/api/auth/login` | — | Get an access + refresh token pair |
| POST | `/api/auth/refresh` | — | Rotate the token pair |
| GET | `/api/auth/me` | Bearer | Current user |
| POST | `/api/auth/logout` | Bearer | Revoke all refresh tokens for the user |

The `/api/auth/*` endpoints are rate limited per client IP.

### Response envelope

Every response uses the same shape, built by the helpers in
[internal/model/response/wrapper.go](internal/model/response/wrapper.go):

```json
{ "success": true, "message": "Login successful", "data": {} }
```

```json
{ "success": false, "message": "Validation failed",
  "errors": { "email": "Must be a valid email address" } }
```

`data` and `errors` are omitted when empty; `meta` carries pagination when needed.

### Example flow

```bash
BASE=http://localhost:8080/api

curl -X POST $BASE/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"supersecret123","name":"Jane"}'

TOKENS=$(curl -s -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"supersecret123"}')

ACCESS=$(echo "$TOKENS" | jq -r .data.accessToken)
curl $BASE/auth/me -H "Authorization: Bearer $ACCESS"
```

## Authentication

- **Access token** — 15 min default, sent as `Authorization: Bearer <token>`.
- **Refresh token** — 7 days default, exchanged at `/api/auth/refresh`.
- Tokens are returned in the response body, not set as cookies, so there is no CSRF
  token to manage. Store them where your client can keep them out of reach of
  injected scripts.
- Access and refresh tokens are signed with **separate secrets**, so a refresh
  token cannot be presented as an access token.

Security properties worth knowing about, because they shape how the code is written:

- **Refresh tokens are stored hashed.** Only the SHA-256 digest is written to the
  database ([hash.go](internal/service/token/hash.go)), so a database leak yields
  nothing replayable. Tokens are looked up by digest via a unique index.
- **Rotation with reuse detection.** Each refresh consumes the old token. Replaying
  a consumed token revokes *every* refresh token for that user, on the assumption
  that a replayed token is a stolen one
  ([refresh.service.go](internal/service/user/refresh.service.go)).
- **Logout revokes everything**, not just the calling session, so a stolen refresh
  token dies at logout.
- **Login does not leak which emails exist.** An unknown email and a wrong password
  return an identical response, and the unknown-email path still runs a bcrypt
  comparison so the timing matches.
- **Internal errors never reach the client.** Handlers map known sentinel errors
  ([errors.go](internal/service/user/errors.go)) to statuses and answer everything
  else with a generic 500, logging the real cause against the request ID.
- **Passwords** are bcrypt-hashed and capped at 72 bytes, because bcrypt silently
  ignores anything beyond that.
- **Proxies are not trusted by default**, so `X-Forwarded-For` cannot be used to
  spoof a client IP past the rate limiter. Set `TRUSTED_PROXIES` when you actually
  run behind one.

Middleware context keys, and the helpers that read them
([auth.go](internal/api/middleware/auth.go)): `user_id`, `user_email`, `claims` via
`GetUserIDFromContext(c)`, `GetEmailFromContext(c)`, `GetClaimsFromContext(c)`.
`OptionalAuthMiddleware` populates the same keys but lets anonymous requests through.

## Architecture

Dependencies point inward only:

```
HTTP (api/) → Service (service/) → Repository (repository/) → Model (model/)
```

```
cmd/server/main.go            Entry point: config, container, serve, graceful shutdown
docs/openapi.yaml             API specification

internal/
├── api/
│   ├── handler/              Bind + validate request, call service, map errors to status
│   ├── middleware/           Auth, request ID, structured logging, recovery, rate limit
│   └── router.go             Route registration
├── service/                  Business logic; one package per domain
│   ├── user/                 register, login, refresh, logout, sentinel errors
│   ├── token/                JWT issue/validate, token hashing (no repository)
│   └── health/               Liveness and dependency checks
├── repository/
│   ├── interfaces/           Contracts (*.repository_interface.go)
│   ├── implementations/      GORM implementations, one package per domain
│   └── mock/                 Generated gomock mocks
├── model/
│   ├── entity/               GORM models
│   ├── request/              Input DTOs with `binding` validation tags
│   └── response/             Output DTOs + the response envelope
├── di/container.go           Wires repositories → services → handlers → router
└── platform/                 Config, database, migrations, logger
```

Layer guides: [internal/](internal/README.md) ·
[service/](internal/service/README.md) · [repository/](internal/repository/README.md) ·
[model/](internal/model/README.md)

### Conventions

- **TDD** — write the test first; see [internal/service/README.md](internal/service/README.md).
- **File naming** — `<action>.<layer>.go`, e.g. `login.service.go`, `find_by_id.gorm.go`.
- **Tests** — `<action>.<layer>_test.go`, table-driven, `gomock` for repositories.
- **Context** — every service and repository method takes a `context.Context`.
- **Interfaces** — services depend on repository interfaces, never concrete types.
- **DTOs** — requests in, responses out; entities stay behind the repository layer.
- **Errors** — services return sentinel errors for expected failures and wrap
  everything else with `fmt.Errorf("...: %w", err)`.
- **Imports** — three groups: stdlib, third-party, then this module.

## Configuration

Copy `env.example` to `.env`; it documents every variable with its default. The ones
that matter most:

| Variable | Default | Notes |
|----------|---------|-------|
| `DEV_MODE` | `false` | `true` enables fallback secrets, Gin debug, SQL logging |
| `DATABASE_TYPE` | `sqlite` | or `postgres` |
| `DATABASE_DSN` | `dev.db` | file path, or a Postgres connection string |
| `DATABASE_AUTO_MIGRATE` | `true` | set `false` in production |
| `JWT_ACCESS_SECRET` | — | **required** when `DEV_MODE=false`, ≥32 chars |
| `JWT_REFRESH_SECRET` | — | **required**, ≥32 chars, must differ from the above |
| `ALLOWED_ORIGINS` | `http://localhost:5173` | comma-separated CORS origins |
| `TRUSTED_PROXIES` | *(none)* | set only when behind a proxy you control |
| `RATE_LIMIT_LOGIN_ATTEMPTS` | `10` | per IP per window; `0` disables |
| `LOG_LEVEL` | *(auto)* | `debug` in dev mode, `info` otherwise |

## Database

SQLite (default, pure Go — no CGO) or PostgreSQL. Switch with:

```bash
DATABASE_TYPE=postgres
DATABASE_DSN="host=localhost user=postgres password=postgres dbname=app port=5432 sslmode=disable"
```

Schema is applied by [platform.Migrate](internal/platform/migrate.go) at startup when
`DATABASE_AUTO_MIGRATE=true`. Register new entities in `migrationModels` there.

**AutoMigrate is not a migration tool.** It adds columns and indexes but never
alters or drops them, and it has no version history or rollback. For anything
deployed, set `DATABASE_AUTO_MIGRATE=false` and manage the schema with
[golang-migrate](https://github.com/golang-migrate/migrate),
[atlas](https://atlasgo.io), or [goose](https://github.com/pressly/goose).

Expired refresh tokens are swept hourly by a janitor goroutine started in
[main.go](cmd/server/main.go); expired rows are never read, so without it the table
would only grow.

## Observability

- **Structured logs** via `log/slog` — text in dev mode, JSON otherwise. One line
  per request with method, path, status, latency, client IP, and request ID.
- **Request IDs** — every response carries `X-Request-ID`. Send your own header and
  it is reused, so a client-reported failure can be traced to its log lines.
- **Panics** are recovered, logged with a stack trace, and answered as a generic 500.
- **Probes** — `/api/health` for liveness, `/api/health/ready` for readiness. Point
  your orchestrator's liveness probe at the former and readiness at the latter, so a
  brief database outage removes the instance from rotation instead of restarting it.

## Testing

```bash
make test           # -race, coverage
make test-coverage  # coverage.html
```

Coverage spans every layer: service logic with mocked repositories, middleware and
handlers over `httptest`, and a full register → login → protected route → rotate →
logout flow against a real SQLite database in
[internal/di/container_test.go](internal/di/container_test.go).

## Docker

```bash
make docker-build   # Multi-stage build → distroless, non-root, static binary
make docker-up      # API + Postgres, API waits for the database to be healthy
make docker-down
```

The runtime image is `distroless/static` with no shell, so configure the readiness
probe in your orchestrator against `/api/health/ready` rather than a `HEALTHCHECK`.

## CI

[.github/workflows/ci.yml](.github/workflows/ci.yml) runs on push and pull request:
formatting, `go mod tidy` check, vet, `go test -race -cover`, build, golangci-lint,
and a Docker build. Reproduce it locally with `make check`.

## Production checklist

- [ ] `DEV_MODE=false`. Startup then **fails** unless both JWT secrets are set, are
      at least 32 characters, differ from each other, and are not the built-in dev
      values ([container.go](internal/di/container.go)).
- [ ] Generate secrets properly: `openssl rand -base64 48`.
- [ ] `DATABASE_TYPE=postgres` with a real DSN and TLS.
- [ ] `DATABASE_AUTO_MIGRATE=false`, schema managed by a migration tool.
- [ ] `ALLOWED_ORIGINS` set to your actual origins, not the default.
- [ ] `TRUSTED_PROXIES` set if and only if you run behind a proxy.
- [ ] Terminate TLS at the load balancer or a reverse proxy.
- [ ] Rate limiting is in-process, so each replica counts separately. Behind more
      than one instance, move it to Redis or your gateway before treating it as a
      hard guarantee.

## Credits

- Originally inspired by [clean-go-vite-react](https://github.com/kamil5b/clean-go-vite-react) by [@kamil5b](https://github.com/kamil5b)
- Extracted from [clean-arch-go-vite-react](https://github.com/ferriyusra/clean-arch-go-vite-react)
