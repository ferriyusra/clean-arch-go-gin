# Backend - Go Clean Architecture Guide

A production-ready Go backend built with **Clean Architecture** principles,
featuring a test harness for every layer, dependency injection, and clear
separation of concerns.

This is the tour of the `internal/` tree with a full worked example. The root
[README.md](../README.md) is the short version, [CLAUDE.md](../CLAUDE.md) the
checklist, [TESTING.md](../TESTING.md) the test harnesses and
[AUTH.md](../AUTH.md) the auth reference.

## 📚 Quick Navigation

- **Getting Started** → [Jump to Setup](#-getting-started)
- **Adding Features** → [Development Workflow](#-development-workflow)
- **Model Layer** → See [`internal/model/README.md`](./model/README.md)
- **Service Layer** → See [`internal/service/README.md`](./service/README.md)
- **Repository Layer** → See [`internal/repository/README.md`](./repository/README.md)

## 🏗️ Architecture Overview

Strict layer separation, dependencies pointing inward only:

```
┌─────────────────────────────────────────────────────────────┐
│                        HTTP Layer (API)                      │
│  • Handlers: bind → call service → respond                  │
│  • Middleware (request id, auth, CSRF, CORS, limits)        │
│  • Route definitions, all under /api/v1                     │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                      Service Layer (Business Logic)          │
│  • Orchestrates operations, owns transaction boundaries     │
│  • Business rules, apperr classification                    │
│  • Uses Request/Response DTOs                               │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                   Repository Layer (Data Access)             │
│  • Interface-based contracts                                │
│  • GORM implementations, one method per file                │
│  • Uses Entity models                                       │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                          Database                            │
│  • PostgreSQL (production)                                  │
│  • SQLite (development, and in memory for tests)            │
└─────────────────────────────────────────────────────────────┘
```

### Directory Structure

```
internal/
├── api/                    # HTTP layer
│   ├── handler/            # bind → call service → respond; respond.go maps errors
│   ├── middleware/         # request id, access log, recovery, security headers,
│   │                       # body limit, rate limit, timeout, auth, CSRF
│   └── router.go           # SetupRoutes, SetupHealthRoutes, SetupFallbacks
│
├── service/                # business logic; depends on repository interfaces only
│   ├── user/ counter/ message/ token/ csrf/ health/
│   ├── mock/               # generated service mocks (used by handler tests)
│   └── README.md           # 📖 Service development guide (TDD)
│
├── repository/             # data access layer
│   ├── interfaces/         # contracts (*.repository_interface.go)
│   ├── implementations/    # GORM implementations, one method per file
│   ├── dbtx/               # the transaction-in-context plumbing
│   ├── mock/               # generated repository mocks (used by service tests)
│   └── README.md           # 📖 Repository implementation guide
│
├── model/
│   ├── entity/             # GORM models (UUID PKs, explicit TableName)
│   ├── request/            # input DTOs with binding: validation tags
│   ├── response/           # output DTOs + the APIResponse envelope
│   └── README.md           # 📖 Model structure guide
│
├── apperr/                 # the *apperr.Error type and the shared sentinels
├── logging/                # slog setup + the context-scoped logger accessor
├── tracing/                # OpenTelemetry setup, the GORM span plugin, trace ids
├── observability/          # Prometheus metrics + pprof on a separate listener
├── platform/               # config.go, config_validate.go, database.go, migrate.go
├── di/                     # container.go — the single wiring point
└── testutil/               # shared test harness (assertions, in-memory DB, HTTP)
```

### Layer Responsibilities

| Layer | Purpose | What It Contains | What It Uses |
|-------|---------|------------------|--------------|
| **API** | HTTP concerns | Handlers, middleware, routing | Services |
| **Service** | Business logic | Domain operations, transaction boundaries, error classification | Repository interfaces, Request/Response models |
| **Repository** | Data access | Queries and writes, one method per file | Entities, GORM, `dbtx` |
| **Model** | Data structures | Entities, Request/Response DTOs | Nothing (pure data) |
| **Platform** | Infrastructure | Config + validation, DB connection, migrations | GORM, third-party libs |
| **DI** | Dependency wiring | Container, middleware chain, startup | All layers |

Three cross-cutting concerns deliberately do **not** appear as a layer:

- **Errors.** Services return `*apperr.Error`; `handler.Fail` is the single place
  an error becomes a status code. Nothing compares error strings.
- **Logging.** The logger travels in the `context.Context`, put there by
  `middleware.RequestID` and read anywhere with `logging.FromContext(ctx)`. No
  constructor takes a logger.
- **Tracing.** Off unless `OTEL_ENABLED=true`. When on, the W3C trace id is
  adopted as the request id, so one identifier covers the log line, the error
  body and the span.

## 🚀 Getting Started

### Prerequisites

- **Go 1.26+** (check with `go version`; `go.mod` declares `go 1.26.0`)
- **Make** for build automation — on Windows run the recipes from Git Bash or
  WSL, since they are POSIX shell
- **PostgreSQL** for production, or **SQLite** for development (the default, no
  setup required)

`air` (hot reload) and `mockgen` are declared in `go.mod` under the `tool`
directive, so `go tool air` and `go tool mockgen` work with nothing installed
globally.

### Installation

1. **Clone and enter the project:**
   ```bash
   cd clean-arch-go-gin
   ```

2. **Download dependencies:**
   ```bash
   make install-deps
   ```

3. **Set up environment:**
   ```bash
   cp env.example .env
   ```

4. **Configure `.env`** — `env.example` documents every variable; the minimum is:
   ```env
   # Server
   SERVER_PORT=8080
   SERVER_HOST=                    # empty = all interfaces

   # Database (SQLite for dev)
   DATABASE_TYPE=sqlite
   DATABASE_DSN=dev.db

   # Development: substitutes throwaway secrets, drops the cookie Secure flag
   DEV_MODE=true
   ```

   With `DEV_MODE=false` (the production default), `JWT_ACCESS_SECRET`,
   `JWT_REFRESH_SECRET` and `CSRF_SECRET` are all required, each at least 32
   characters, and the two JWT secrets must differ. `Config.Validate()` runs
   inside `di.NewContainer` and reports **every** problem at once.

5. **Run the server:**
   ```bash
   make dev          # hot-reload via air
   # OR
   make server       # go run, no reload
   ```

6. **Verify:**
   ```bash
   curl http://localhost:8080/api/health
   # {"success":true,"message":"Service is ready","data":{"status":"ok","message":"All dependencies healthy",...}}
   ```

## 🔧 Development Workflow

### Adding a New Feature (TDD)

```
1. Create the entity              → model/README.md
   └─ plus its entry in platform/migrate.go: entities() and a new Migration

2. Create the repository interface → repository/README.md
   └─ interfaces/<x>.repository_interface.go

3. Generate mocks
   └─ make mocks   (regenerates repository AND service mocks)

4. Implement the repository        → repository/README.md
   └─ <x>.gorm.go, then one file per method, plus a test against testutil.NewDB

5. Define Request/Response DTOs    → model/README.md
   └─ binding: tags; register the response type in naming_test.go

6. Write the service test FIRST    → service/README.md
   └─ table-driven, gomock, assert on apperr sentinels

7. Implement the service           → service/README.md
   └─ <x>.service.go (interface + struct + constructor), then one file per method

8. Create the handler
   └─ plus a handler test using the generated service mock

9. Register the route in api/router.go
   └─ public or protected; add middleware.CSRFMiddleware for state-changing verbs

10. Wire it in di/container.go
```

Step 5 comes before step 6 on purpose: the DTOs are what tell you what the test
can assert.

### Example: Adding a "Product" Feature

**Step 1: Create the entity** (see [`model/README.md`](./model/README.md))

```go
// model/entity/product.go
package entity

import (
    "time"

    "github.com/google/uuid"
    "gorm.io/gorm"
)

type ProductEntity struct {
    ID        uuid.UUID `gorm:"primaryKey"`
    Name      string    `gorm:"not null"`
    Price     float64   `gorm:"not null"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (ProductEntity) TableName() string { return "product_entities" }
```

Register it in `platform/migrate.go` — **both** halves:

```go
func entities() []any {
    return []any{
        &entity.UserEntity{},
        &entity.RefreshTokenEntity{},
        &entity.CounterEntity{},
        &entity.MessageEntity{},
        &entity.ProductEntity{},   // new
    }
}

func migrations() []Migration {
    return []Migration{
        // ... 1, 2, 3 unchanged — never edit or renumber an applied migration
        {
            Version: 4,
            Name:    "create products",
            Up:      func(tx *gorm.DB) error { return tx.AutoMigrate(&entity.ProductEntity{}) },
        },
    }
}
```

Adding to `entities()` alone changes nothing on an existing database: migration 1
has already run there, so only the new version creates the table.

**Step 2: Create the repository interface** (see [`repository/README.md`](./repository/README.md))

```go
// repository/interfaces/product.repository_interface.go
package interfaces

import (
    "context"

    "github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
    "github.com/google/uuid"
)

type ProductRepository interface {
    Create(ctx context.Context, product entity.ProductEntity) (*uuid.UUID, error)
    FindByID(ctx context.Context, id uuid.UUID) (*entity.ProductEntity, error)
}
```

**Step 3: Generate mocks**

```bash
make mocks
```

This writes `internal/repository/mock/product.repository_mock.go`. Never edit it;
CI runs `make verify-mocks` and fails if the committed mocks are stale.

**Step 4: Implement the repository**

```go
// repository/implementations/product/product.gorm.go
package product

import (
    "gorm.io/gorm"

    "github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

type GORMProductRepository struct {
    db *gorm.DB
}

type ProductModel = entity.ProductEntity

// The schema is applied at startup by platform.Migrate, not here: the
// constructor neither migrates nor returns an error.
func NewGORMProductRepository(db *gorm.DB) *GORMProductRepository {
    return &GORMProductRepository{db: db}
}
```

```go
// repository/implementations/product/create.gorm.go
package product

import (
    "context"

    "github.com/google/uuid"

    "github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
    "github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

func (r *GORMProductRepository) Create(ctx context.Context, product entity.ProductEntity) (*uuid.UUID, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    if product.ID == uuid.Nil {
        product.ID = uuid.New()
    }

    if err := dbtx.Conn(ctx, r.db).Create(&product).Error; err != nil {
        return nil, err
    }

    return &product.ID, nil
}
```

**`dbtx.Conn(ctx, r.db)`, never `r.db`.** A method that uses `r.db` directly
compiles, passes its own test, and silently escapes any transaction its caller
opened — so the write commits even when the unit of work around it rolls back.
That is the single most expensive mistake available in this layer; see
[`repository/README.md`](./repository/README.md) for the whole story.

**Step 5: Define the DTOs** (see [`model/README.md`](./model/README.md))

```go
// model/request/product.go
package request

type CreateProductRequest struct {
    Name  string  `json:"name"  binding:"required,min=1,max=255"`
    Price float64 `json:"price" binding:"required,gt=0"`
}

// model/response/product.go
package response

import "github.com/google/uuid"

type GetProduct struct {
    ID    uuid.UUID `json:"id"`
    Name  string    `json:"name"`
    Price float64   `json:"price"`
}
```

Then add `GetProduct{}` to `responseTypes()` in
`internal/model/response/naming_test.go`, or the camelCase check does not cover
it.

**Step 6: Write the service test first** (see [`service/README.md`](./service/README.md))

```go
// service/product/create_product.service_test.go
package product

func TestCreateProduct(t *testing.T) {
    validRequest := &request.CreateProductRequest{Name: "Widget", Price: 29.99}

    tests := []struct {
        name    string
        expect  func(repo *mock.MockProductRepository)
        wantErr error
    }{
        {
            name: "creates a product",
            expect: func(repo *mock.MockProductRepository) {
                id := uuid.New()
                repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&id, nil)
            },
        },
        {
            name: "reports a write failure as internal",
            expect: func(repo *mock.MockProductRepository) {
                repo.EXPECT().Create(gomock.Any(), gomock.Any()).
                    Return(nil, errors.New("database error"))
            },
            wantErr: apperr.ErrInternal,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // gomock.NewController registers its own cleanup — no ctrl.Finish().
            repo := mock.NewMockProductRepository(gomock.NewController(t))
            tt.expect(repo)

            result, err := NewProductService(repo).
                CreateProduct(context.Background(), validRequest)

            if tt.wantErr != nil {
                testutil.ErrorIs(t, err, tt.wantErr)
                return
            }
            testutil.NoError(t, err)
            testutil.Equal(t, result.Name, validRequest.Name, "name")
        })
    }
}
```

Assertions come from `internal/testutil`, not testify, and match **sentinels**
with `testutil.ErrorIs` rather than message strings.

**Step 7: Implement the service**

```go
// service/product/product.service.go
package product

type ProductService interface {
    CreateProduct(ctx context.Context, req *request.CreateProductRequest) (*response.GetProduct, error)
}

type productService struct {
    repo interfaces.ProductRepository
}

func NewProductService(repo interfaces.ProductRepository) ProductService {
    return &productService{repo: repo}
}
```

```go
// service/product/create_product.service.go
func (s *productService) CreateProduct(ctx context.Context, req *request.CreateProductRequest) (*response.GetProduct, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    id, err := s.repo.Create(ctx, entity.ProductEntity{Name: req.Name, Price: req.Price})
    if err != nil {
        return nil, apperr.Internal(fmt.Errorf("creating product: %w", err))
    }

    return &response.GetProduct{ID: *id, Name: req.Name, Price: req.Price}, nil
}
```

`apperr.Internal` keeps the driver error for the log and gives the client a
generic message. A condition the client *should* know about gets a sentinel
instead (`apperr.ErrUserAlreadyExists` and friends, in `apperr/sentinels.go`).

If the operation ever writes more than one row, wrap it:
`s.txManager.WithinTx(ctx, func(ctx context.Context) error { ... })`, using the
callback's `ctx`.

Run it: `go test ./internal/service/product/ -v` ✅

**Step 8: Create the handler**

```go
// api/handler/product.go
package handler

type ProductHandler struct {
    service product.ProductService
}

func NewProductHandler(service product.ProductService) *ProductHandler {
    return &ProductHandler{service: service}
}

func (h *ProductHandler) CreateProduct(c *gin.Context) {
    req := &request.CreateProductRequest{}
    if !BindJSON(c, req) {
        return // BindJSON already wrote the field-level 400
    }

    result, err := h.service.CreateProduct(c.Request.Context(), req)
    if err != nil {
        Fail(c, err)
        return
    }

    OK(c, http.StatusCreated, "Product created", result)
}
```

A handler never touches `c.JSON` directly and never decides a status code from an
error: `BindJSON`, `OK`, `OKWithMeta` and `Fail` in `handler/respond.go` are the
whole vocabulary. `Fail` logs the full chain, marks the span failed on 5xx only,
and attaches `requestId` / `traceId` to the body.

**Step 9: Register the route**

```go
// api/router.go, inside SetupRoutes
protected.GET("/products/:id", productHandler.GetProduct)
protected.POST("/products", middleware.CSRFMiddleware(csrfService), productHandler.CreateProduct)
```

CSRF is attached **per route**, not globally by method — adding a POST does not
add CSRF protection to it.

**Step 10: Wire it up**

```go
// di/container.go, inside NewContainer
productRepository := productRepo.NewGORMProductRepository(db)   // no error to handle
// ... services.Product = productSvc.NewProductService(productRepository)
// ... handlers.Product = handler.NewProductHandler(services.Product)
// ... pass handlers.Product to api.SetupRoutes
```

**Verify:**

```bash
CSRF=$(curl -s http://localhost:8080/api/v1/csrf | jq -r .data.token)
curl -X POST http://localhost:8080/api/v1/products \
  -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" \
  -b cookies.txt -d '{"name":"Widget","price":29.99}'
```

## 📍 API Endpoints

Everything except the health probes lives under **`/api/v1`**. There are
deliberately no unversioned aliases: two live surfaces is a tax paid on every
route forever, and the cost of the move is one base URL in the client.

### Health (unversioned on purpose)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Readiness: checks the database, 503 when it is down |
| GET | `/api/health/live` | Liveness: is the process up (touches no dependency) |
| GET | `/api/health/ready` | Same as `/api/health` |

These stay unversioned because they are a contract with the orchestrator, not
with an API client: liveness probes, load balancers and uptime monitors all live
outside this repository, and releasing `/api/v2` must not require editing them in
lockstep.

### Public

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/message` | Get the message |
| GET | `/api/v1/csrf` | Get a CSRF token |
| POST | `/api/v1/auth/register` | Register a new user (tighter rate limit) |
| POST | `/api/v1/auth/login` | Login (tighter rate limit) |
| POST | `/api/v1/auth/refresh` | Rotate the token pair (requires CSRF) |

### Protected (requires the `access_token` cookie)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/auth/me` | Get the current user |
| GET | `/api/v1/auth/sessions` | List active sessions, paginated (`?page=&limit=`) |
| PATCH | `/api/v1/auth/password` | Change the password (requires CSRF) |
| DELETE | `/api/v1/auth/me` | Delete the account (requires CSRF) |
| POST | `/api/v1/auth/logout` | Logout (requires CSRF) |
| GET | `/api/v1/counter` | Get the counter value |
| POST | `/api/v1/counter` | Increment the counter (requires CSRF) |

Unmatched routes and methods return the same envelope as everything else, not
gin's bare 404 with an empty body (`SetupFallbacks`).

## 🧪 Testing

Four harnesses, each answering a different question:

| Layer | Harness |
|---|---|
| services | gomock + generated repository mocks |
| repositories | a real sqlite database, in memory (`testutil.NewDB`) |
| handlers and middleware | `httptest` + a real `gin.Engine` + service mocks |
| the whole application | the real DI container, nothing mocked |

```bash
make test           # go test -cover ./...      (no cgo needed)
make test-verbose   # -v -failfast
make test-race      # needs CGO_ENABLED=1 and a C compiler
make test-coverage  # coverage.out + coverage.html
make bench          # benchmarks only, with -benchmem
make fuzz           # one fuzz target: PKG=... FUZZ=... FUZZTIME=30s
make vuln           # govulncheck, pulled on demand
```

Run one package or one test:

```bash
go test -cover ./internal/service/token/
go test -run TestRefresh ./internal/service/user/
```

Tests that own no shared state call `t.Parallel()`, on the parent test and on
each subtest. There are benchmarks in `apperr`, `token` and `csrf`, and two fuzz
targets — `FuzzValidate` (CSRF) and `FuzzValidateAccessToken` — because both
parse attacker-supplied text on a public endpoint.

**[TESTING.md](../TESTING.md) is the full guide**, including the traps in the
in-memory sqlite setup.

## 🔐 Authentication & Security

### Token flow

1. **Register / Login** mint *and persist* an access + refresh pair; the handler
   only moves them into HttpOnly cookies. There is no `Authorization: Bearer`
   path anywhere.
2. **API requests** carry the cookies automatically; `AuthMiddleware` validates
   the access token and puts the claims in the gin context
   (`middleware.GetUserIDFromContext`).
3. **Refresh** rotates the *pair*: the presented refresh token is deleted and a
   new pair issued, so a stolen refresh token is usable for one request rather
   than a week. A token that verifies but has no database row is treated as a
   replay and revokes every session for that user.
4. **Logout** deletes the rows and always succeeds.

Refresh tokens are JWTs *and* rows, but only a SHA-256 digest is stored — a
leaked dump yields nothing replayable. Expired rows are swept by a janitor
started from `Container.StartJanitor`.

### CSRF

Stateless HMAC-SHA256, fetched from `GET /api/v1/csrf` and sent as
`X-CSRF-Token`. The issue time is inside the signed material, so it cannot be
edited to extend a captured token; tokens expire after `CSRF_TOKEN_TTL` because a
stateless token cannot be revoked. The middleware is attached **per route** in
`router.go` — the router is the only authority on which endpoints require it.

### The middleware chain

Order is load-bearing, and it is built in one place, `di.newRouter`:

```
otelgin (if tracing) → RequestID → Recovery → metrics → AccessLog
→ SecurityHeaders → CORS → BodyLimit → RateLimit → Timeout
```

`RequestID` runs first so everything downstream — the recovery handler included —
has a correlated logger; `Recovery` wraps the rest so a panic still produces the
standard envelope; the metrics middleware sits after `Recovery` so a panicked
request is counted with the 500 that was actually written.

- ✅ Passwords hashed with bcrypt; login costs the same for an unknown email as a
  known one, so response time cannot be used to enumerate accounts
- ✅ Secrets from the environment, validated at startup (length, and the two JWT
  secrets must differ)
- ✅ Per-IP rate limiting, with a separate and much tighter budget for the
  credential endpoints
- ✅ Request body size limit (`MAX_REQUEST_BODY_BYTES`, default 1 MiB)
- ✅ `TRUSTED_PROXIES` empty by default, so `ClientIP` is the direct peer rather
  than a spoofable header
- ✅ Soft deletes for user rows; hard deletes for credentials

[AUTH.md](../AUTH.md) is the full reference.

## 📈 Observability

**Logging.** `log/slog`, JSON in production and text in development. Every
request gets a correlation id and a pre-tagged logger placed in the request
context; any layer reads it with `logging.FromContext(ctx)` without a single
signature change. Repositories and services return errors; handlers and
middleware log them — one line per request.

**Tracing.** OpenTelemetry, off unless `OTEL_ENABLED=true`. Each request becomes
a span and each query a child span; the request id *becomes* the trace id, so one
identifier ties the log line, the error body and the span together. The GORM
plugin is written in-house (`internal/tracing/gorm.go`) and records SQL text but
never bound parameters — there is a test that fails if that changes.

**Metrics and pprof.** `internal/observability` serves Prometheus metrics at
`/metrics` and the pprof handlers under `/debug/pprof/` on a **separate admin
listener**, default `127.0.0.1:9090`, never on the public router:

| Variable | Default | Notes |
|---|---|---|
| `METRICS_ENABLED` | `false` | serves `/metrics`; the registry is only built when it is on |
| `PPROF_ENABLED` | `false` | leave off unless actively profiling |
| `ADMIN_HOST` | `127.0.0.1` | loopback on purpose; reach it with a port forward |
| `ADMIN_PORT` | `9090` | must differ from `SERVER_PORT` |

`/debug/pprof` lets an unauthenticated caller dump the heap, stall the process
for a thirty-second CPU profile, or read a full goroutine dump, and `/metrics`
leaks operational detail. Neither belongs on a port the internet can reach, so
outside `DEV_MODE` startup refuses to bind pprof to a non-loopback address.

The listener is built by `di.NewContainer`, started by `Container.StartAdmin()`
from `main`, and drained first by `Container.Close()` — it is the least important
thing running and the most likely to be holding an open scrape or a long profile.
With both signals off it is never created at all, so no socket is opened.

**Correlation in responses.** Error responses carry `requestId` and `traceId`;
successful ones stay lean, since `X-Request-ID` already carries the same value.
Every response key is camelCase, enforced by a test in
`internal/model/response`.

## 🏭 Production Deployment

### Build

```bash
make build
```

Produces `./bin/server-$GOOS-$GOARCH` (`.exe` on Windows), built with
`CGO_ENABLED=0 -trimpath` and a version stamp in `-ldflags`.

### Environment

```env
SERVER_PORT=8080
SERVER_HOST=0.0.0.0

DATABASE_TYPE=postgres
DATABASE_DSN=postgresql://user:password@localhost:5432/dbname?sslmode=require

# Generate new ones: openssl rand -base64 32
JWT_ACCESS_SECRET=<32+ characters>
JWT_REFRESH_SECRET=<32+ characters, different from the access secret>
CSRF_SECRET=<32+ characters>

SERVER_READ_TIMEOUT=15s
SERVER_WRITE_TIMEOUT=15s
SERVER_IDLE_TIMEOUT=60s
SERVER_SHUTDOWN_TIMEOUT=10s
SERVER_REQUEST_TIMEOUT=10s

DATABASE_MAX_OPEN_CONNS=25
DATABASE_MAX_IDLE_CONNS=5
DATABASE_CONN_MAX_LIFETIME=5m

# Run migrations as their own step instead
DATABASE_AUTO_MIGRATE=false

DEV_MODE=false
```

### Running

**Binary**

```bash
./bin/server-linux-amd64
```

`cmd/server/main.go` shuts down on SIGINT/SIGTERM with
`cfg.Server.ShutdownTimeout`, then calls `container.Close()`, which stops the
admin listener, flushes the tracer and closes the database.

**Docker Compose**

```bash
make docker-up                          # app + postgres
docker compose --profile tracing up     # also Jaeger, at http://localhost:16686
```

**Systemd**

```ini
[Unit]
Description=clean-arch-go-gin
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/app
ExecStart=/opt/app/bin/server-linux-amd64
Restart=always
Environment="DATABASE_TYPE=postgres"
Environment="DATABASE_DSN=postgresql://..."
Environment="DATABASE_AUTO_MIGRATE=false"

[Install]
WantedBy=multi-user.target
```

## 📊 Database Management

### Supported databases

- **SQLite** — default for development, and what the tests run against in memory
- **PostgreSQL** — production. Anything else is rejected at startup by
  `Config.Validate`.

### Versioned migrations

There is no migration tool and no `AutoMigrate` sweep at boot.
`platform.Migrate(db)` walks an ordered `[]Migration` and applies whatever the
`schema_migrations` ledger says has not run, each inside its own transaction
together with the row that records it — so a failure leaves the database on the
last version that fully succeeded.

- **Append, never edit or renumber.** A version that has run somewhere is a fact.
  A startup check rejects duplicate or out-of-order versions, which is the
  mistake two branches make when both add "the next" migration.
- **No down migrations.** Reversing a schema change in production is nearly
  always a restore or a new forward migration; a down step that is never
  exercised is a false sense of safety.
- **`AutoMigrate` survives inside migration 1**, where creating tables is all it
  has to do. It is additive only, which is why dropping the old plaintext
  refresh-token column had to be written out by hand as migration 3.
- **Startup migration is not coordinated** between instances: two booting at once
  both try and the loser fails on the ledger primary key. Safe, but noisy — hence
  `DATABASE_AUTO_MIGRATE=false` in production.

### Switching to PostgreSQL

```bash
createdb myapp
```

```env
DATABASE_TYPE=postgres
DATABASE_DSN=postgresql://user:pass@localhost:5432/myapp?sslmode=disable
```

`InitializeDatabase` pings with retry and backoff before returning, so a
container that starts before its database does not die instantly.

## 🐛 Troubleshooting

### Port already in use
```bash
lsof -ti:8080 | xargs kill -9
```

### Database locked (SQLite)
```bash
rm dev.db && make server     # development only
```

### `make test-race` fails with `cgo: C compiler "gcc" not found`
Expected on a machine without a C toolchain. `make test` deliberately omits
`-race`; CI runs the race detector on Linux.

### Module not found
```bash
go mod tidy && go mod download
```

### Hot reload not working
`air` comes from the `tool` directive in `go.mod` — `make dev` runs
`go tool air`, so there is nothing to install. Check `.air.toml` if it starts but
does not rebuild.

### Generated mocks are out of date
```bash
make mocks     # then commit the result; CI runs make verify-mocks
```

## 📝 Code Standards

1. **Dependency direction** — depend on interfaces, never concrete types across a
   layer boundary.
2. **Errors** — `apperr` sentinels for anything a client should see;
   `apperr.Internal(fmt.Errorf("doing thing: %w", err))` for internal failures;
   `errors.Is`, never string comparison.
3. **Context first** — every operation takes `ctx` as its first parameter and
   passes it down unchanged. The logger rides along in it.
4. **Test first** — and assert on sentinels with `testutil.ErrorIs`.
5. **File naming** — `<domain>.<layer>.go` for the interface/struct/constructor,
   `<action>.<layer>.go` for one method each.

| Layer | Key rules |
|-------|-----------|
| **Models** | No business logic; camelCase JSON; register new response types in `naming_test.go` |
| **Repositories** | UUID keys, always `dbtx.Conn(ctx, r.db)`, missing row is `(nil, nil)` |
| **Services** | `*request.X` in, `*response.X` out; own the transaction boundary; classify errors |
| **Handlers** | Bind, call, respond; `Fail` is the only error path |

## 🔗 Key Dependencies

- **[Gin](https://gin-gonic.com/)** — HTTP framework
- **[GORM](https://gorm.io/)** — ORM, with the `glebarez/sqlite` pure-Go driver
  (which is why the test suite needs no cgo)
- **[golang-jwt](https://github.com/golang-jwt/jwt)** — JWT handling
- **[google/uuid](https://github.com/google/uuid)** — UUID generation
- **[bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt)** — password hashing
- **[godotenv](https://github.com/joho/godotenv)** — `.env` loading
- **[uber-go/mock](https://github.com/uber-go/mock)** — mock generation
- **[OpenTelemetry](https://opentelemetry.io/)** — tracing (opt-in)
- **[Prometheus client](https://github.com/prometheus/client_golang)** — metrics
  (opt-in)

## 📚 Further Reading

- [Model Layer Guide](./model/README.md) — entities, DTOs, the envelope
- [Service Layer Guide](./service/README.md) — TDD, errors, transactions
- [Repository Layer Guide](./repository/README.md) — GORM, `dbtx`, migrations
- [TESTING.md](../TESTING.md) — all four harnesses
- [AUTH.md](../AUTH.md) — the auth reference
- [Clean Architecture by Uncle Bob](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)

---

**Quick Reference:**
- Entry point: `cmd/server/main.go`
- DI container: `internal/di/container.go`
- Routes: `internal/api/router.go`
- Error → status: `internal/api/handler/respond.go`
- Schema: `internal/platform/migrate.go`
