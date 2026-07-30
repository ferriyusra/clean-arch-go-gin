# Backend — Go Clean Architecture Guide

Layer-by-layer developer guide. Setup, commands, API reference, configuration, and
deployment live in the [root README](../README.md) — this document covers only how the
layers fit together and how to add code to them.

## Quick Navigation

| Guide | Covers |
|-------|--------|
| [service/README.md](./service/README.md) | TDD workflow, table-driven tests, mocking repositories |
| [repository/README.md](./repository/README.md) | GORM implementations, interfaces, mock generation |
| [model/README.md](./model/README.md) | Entities, request/response DTOs, validation tags |
| [root README](../README.md) | Setup, commands, API, config, auth, Docker, CI |

## Architecture

Dependencies point **inward only**. An outer layer may import an inner one; never the
reverse.

```
┌─────────────────────────────────────────────┐
│  api/          handlers, middleware, routes │  HTTP concerns only
├─────────────────────────────────────────────┤
│  service/      business logic               │  depends on repository interfaces
├─────────────────────────────────────────────┤
│  repository/   data access (GORM)           │  depends on entities
├─────────────────────────────────────────────┤
│  model/        entities and DTOs            │  depends on nothing
└─────────────────────────────────────────────┘

di/         wires the layers together
platform/   config, database, migrations, logger
```

### Directory structure

```
internal/
├── api/
│   ├── handler/
│   │   ├── user.go             Auth endpoints
│   │   ├── health.go           Liveness + readiness
│   │   ├── errors.go           Sentinel error → HTTP status mapping
│   │   └── validation.go       Binding errors → field messages
│   ├── middleware/
│   │   ├── auth.go             JWT bearer auth + context helpers
│   │   ├── request_id.go       X-Request-ID correlation
│   │   ├── logger.go           slog request logging + panic recovery
│   │   └── rate_limit.go       Per-IP fixed-window limiter
│   └── router.go               SetupRoutes(r, RouterDeps{...})
│
├── service/
│   ├── user/                   register, login, refresh, logout, errors.go
│   ├── token/                  JWT issue/validate + HashToken (no repository)
│   └── health/                 Check, CheckWithDependencies
│
├── repository/
│   ├── interfaces/             *.repository_interface.go — the contracts
│   ├── implementations/        user/, refresh_token/ — GORM, one file per action
│   └── mock/                   Generated; do not edit
│
├── model/
│   ├── entity/                 GORM models
│   ├── request/                Input DTOs with `binding` tags
│   └── response/               Output DTOs + response envelope
│
├── di/container.go             Secret validation, logger, DB, wiring
└── platform/                   config.go, database.go, migrate.go, logger.go
```

### Layer responsibilities

| Layer | Does | Must not |
|-------|------|----------|
| **handler** | Bind and validate input, call one service, map errors to a status | Contain business logic or touch the database |
| **middleware** | Cross-cutting HTTP concerns | Contain domain logic |
| **service** | Business rules, orchestrate repositories, return DTOs | Know about `gin`, HTTP status codes, or SQL |
| **repository** | Persistence for one entity | Contain business rules |
| **model** | Describe data shapes | Contain behaviour or dependencies |
| **di** | Construct and wire everything | Contain logic worth testing on its own |
| **platform** | Infrastructure setup | Know about domains |

The `user` domain is the reference implementation — read it end to end before adding
a new one.

## Adding a Feature

Follow this order; it is Test-Driven Development.

```
1. Entity                     → model/README.md
   └─ internal/model/entity/product.go

2. Register the migration      ← easy to forget
   └─ Add &entity.ProductEntity{} to migrationModels in platform/migrate.go

3. Repository interface        → repository/README.md
   └─ internal/repository/interfaces/product.repository_interface.go

4. Generate mocks
   └─ make repository-mocks

5. Repository implementation   → repository/README.md
   └─ internal/repository/implementations/product/<action>.gorm.go

6. Request/response DTOs       → model/README.md
   └─ Define these before the tests — they are the inputs and outputs you assert on

7. Service tests FIRST         → service/README.md
   └─ Failing table-driven tests against the generated mocks

8. Service implementation      → service/README.md
   ├─ internal/service/product/product.service.go   (interface + constructor)
   ├─ internal/service/product/<action>.service.go  (one file per action)
   └─ internal/service/product/errors.go            (sentinel errors)

9. Handler + routes
   ├─ internal/api/handler/product.go
   ├─ Map new sentinels in handler/errors.go → serviceErrorStatus
   └─ Register routes in api/router.go (add the handler to RouterDeps)

10. Wire it up
    └─ internal/di/container.go: repository → service → handler → router

11. Document it
    └─ Add the endpoints to docs/openapi.yaml
```

Steps 2, 9 (the sentinel mapping), and 11 are the ones most often skipped and the
ones that cause the most confusing breakage later.

### Error handling contract

This is the rule most likely to be violated by copy-pasting older Go code.

Services return a **sentinel error** for every *expected* failure and wrap
*unexpected* ones:

```go
// internal/service/product/errors.go
var ErrProductNotFound = errors.New("product not found")

// in the service
if product == nil {
    return nil, ErrProductNotFound          // expected → sentinel
}
if err != nil {
    return nil, fmt.Errorf("finding product: %w", err)   // unexpected → wrapped
}
```

Handlers never answer with `err.Error()`:

```go
resp, err := h.productService.Get(c.Request.Context(), id)
if err != nil {
    respondServiceError(c, err)   // sentinel → mapped status; anything else → 500
    return
}
```

`respondServiceError` echoes only sentinel messages. Everything else is logged with
the request ID and answered as a generic `Internal server error`, because a wrapped
error carries database and driver detail that must not reach a client.

Register each new sentinel in `serviceErrorStatus` in
[api/handler/errors.go](./api/handler/errors.go), otherwise a perfectly expected
failure surfaces as a 500.

### Validation

Validation lives in `binding` tags on the request DTO, not in hand-written checks
inside handlers:

```go
type CreateProductRequest struct {
    Name  string  `json:"name" binding:"required,max=255"`
    Price float64 `json:"price" binding:"required,gt=0"`
}
```

`c.ShouldBindJSON` then produces field-level errors, which `respondBindError` turns
into a `Validation failed` response keyed by JSON field name.

## Testing

```bash
make test           # everything, with -race
make test-coverage  # writes coverage.html
go test -run TestRefresh ./internal/service/user/...
```

| Layer | Approach |
|-------|----------|
| **service** | Table-driven, repositories replaced with gomock |
| **middleware** | `httptest` against a minimal `gin.New()` router |
| **handler** | `httptest` with a hand-written service stub; asserts statuses, validation messages, and that internal errors are not leaked |
| **di** | Full HTTP flow through the real router against a temporary SQLite database |
| **platform** | Config defaults and env overrides |

Repository implementations are covered indirectly by the `di` integration tests
rather than by mocking GORM.

## Code Standards

- `gofmt` clean; `make check` before you call something done.
- Imports in three blank-line-separated groups: stdlib, third-party, this module.
- Every service and repository method takes `context.Context` as its first parameter
  and checks for cancellation before doing work.
- Services depend on repository *interfaces*, never concrete types.
- Entities never leave the repository layer; convert to a response DTO in the service.
- Use `errors.Is`/`errors.As`, never `==`, when inspecting errors.
- One action per file: `<action>.<layer>.go` with `<action>.<layer>_test.go` beside it.
- Exported types and functions get doc comments; comments explain *why*, not *what*.

## Troubleshooting

**Port already in use**
```bash
lsof -ti:8080 | xargs kill -9
```

**A new table or column is missing** — you added an entity but not its migration. Add
it to `migrationModels` in [platform/migrate.go](./platform/migrate.go). Note that
AutoMigrate never alters or drops existing columns, so changing a field's type needs a
real migration.

**`SQLite database is locked`** (development only)
```bash
rm dev.db && make dev
```

**Refuses to start: "JWT_ACCESS_SECRET and JWT_REFRESH_SECRET must be set"** — you are
running with `DEV_MODE=false`. Either set real secrets (≥32 chars, different from each
other) or use `make dev`.

**A known failure returns 500** — the sentinel is missing from `serviceErrorStatus` in
[api/handler/errors.go](./api/handler/errors.go).

**Mock generation fails** — install the tool and make sure it is on your `PATH`:
```bash
go install github.com/golang/mock/mockgen@latest
make repository-mocks
```

**Hot reload not working**
```bash
go install github.com/air-verse/air@latest   # then check .air.toml
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/gin-gonic/gin` | HTTP router and middleware |
| `github.com/gin-contrib/cors` | CORS |
| `gorm.io/gorm` | ORM |
| `github.com/glebarez/sqlite` | Pure-Go SQLite driver (no CGO) |
| `gorm.io/driver/postgres` | PostgreSQL driver |
| `github.com/golang-jwt/jwt/v5` | JWT signing and validation |
| `golang.org/x/crypto/bcrypt` | Password hashing |
| `github.com/go-playground/validator/v10` | Request validation behind `binding` tags |
| `github.com/google/uuid` | UUID primary keys and request IDs |
| `github.com/joho/godotenv` | `.env` loading |
| `github.com/golang/mock` | Test mocks |
