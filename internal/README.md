> **Note:** this document predates the error-handling, logging and testing work.
> Parts of it have drifted from the code — it still describes `Authorization:
> Bearer` auth, a `GET /api/auth/csrf` route and repository constructors that
> return an error, none of which exist any more. Treat `internal/api/router.go`
> and the code as the source of truth, and see [TESTING.md](../TESTING.md)
> for the current test harnesses. The worked examples below are still useful.

# Backend - Go Clean Architecture Guide

A production-ready Go backend built with **Clean Architecture** principles, featuring comprehensive testing, dependency injection, and clear separation of concerns.

## 📚 Quick Navigation

- **Getting Started** → [Jump to Setup](#-getting-started)
- **Adding Features** → [Development Workflow](#-development-workflow)
- **Model Layer** → See [`internal/model/README.md`](./model/README.md)
- **Service Layer** → See [`internal/service/README.md`](./service/README.md)
- **Repository Layer** → See [`internal/repository/README.md`](./repository/README.md)

## 🏗️ Architecture Overview

This backend implements **Clean Architecture** with strict layer separation:

```
┌─────────────────────────────────────────────────────────────┐
│                        HTTP Layer (API)                      │
│  • Handles HTTP requests/responses                          │
│  • Middleware (auth, CORS, logging)                         │
│  • Route definitions                                        │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                      Service Layer (Business Logic)          │
│  • Orchestrates operations                                  │
│  • Business rules and validation                            │
│  • Uses Request/Response DTOs                               │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                   Repository Layer (Data Access)             │
│  • Interface-based contracts                                │
│  • GORM implementations                                     │
│  • Uses Entity models                                       │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                          Database                            │
│  • PostgreSQL (production)                                  │
│  • SQLite (development)                                     │
└─────────────────────────────────────────────────────────────┘
```

### Directory Structure

```
internal/
├── api/                    # HTTP layer
│   ├── handler/           # Request handlers
│   ├── middleware/        # Auth, CORS, etc.
│   └── router.go          # Route configuration
│
├── service/               # Business logic layer
│   ├── user/             # User domain services
│   ├── counter/          # Counter services
│   ├── csrf/             # CSRF token services
│   ├── token/            # JWT token services
│   └── README.md         # 📖 Service development guide (TDD)
│
├── repository/           # Data access layer
│   ├── interfaces/       # Repository contracts
│   ├── implementations/  # GORM implementations
│   ├── mock/            # Generated mocks
│   └── README.md        # 📖 Repository implementation guide
│
├── model/                # Data models
│   ├── entity/          # Database entities
│   ├── request/         # API request DTOs
│   ├── response/        # API response DTOs
│   └── README.md        # 📖 Model structure guide
│
├── di/                   # Dependency injection
│   └── container.go     # Wire all dependencies
│
└── platform/            # Infrastructure
    ├── config.go        # Configuration management
    └── database.go      # Database initialization
```

### Layer Responsibilities

| Layer | Purpose | What It Contains | What It Uses |
|-------|---------|------------------|--------------|
| **API** | HTTP concerns | Handlers, middleware, routing | Services |
| **Service** | Business logic | Domain operations, validation | Repositories, Request/Response models |
| **Repository** | Data access | CRUD operations, queries | Entities, GORM |
| **Model** | Data structures | Entities, Request/Response DTOs | Nothing (pure data) |
| **Platform** | Infrastructure | Config, DB connection, external services | GORM, third-party libs |
| **DI** | Dependency wiring | Container, initialization | All layers |

## 🚀 Getting Started

### Prerequisites

- **Go 1.25.6+** (check with `go version`)
- **Air** for hot-reload: `go install github.com/air-verse/air@latest`
- **Make** for build automation
- **PostgreSQL** (production) or **SQLite** (development, default)
- **Redis** (optional, for caching)

### Installation

1. **Clone and navigate to project:**
   ```bash
   cd clean-go-vite-react
   ```

2. **Install dependencies:**
   ```bash
   make install-deps
   ```

3. **Set up environment:**
   ```bash
   cp env.example .env
   ```

4. **Configure `.env` file:**
   ```env
   # Server
   SERVER_PORT=8080
   SERVER_HOST=                    # Empty = localhost

   # Database (SQLite for dev)
   DATABASE_TYPE=sqlite
   DATABASE_DSN=dev.db

   # JWT Secrets (CHANGE IN PRODUCTION!)
   JWT_ACCESS_SECRET=change-me-in-production
   JWT_REFRESH_SECRET=change-me-in-production

   # Development
   DEV_MODE=true
   ```

5. **Run the server:**
   ```bash
   make dev          # Frontend + backend with hot-reload
   # OR
   make server       # Backend only with hot-reload
   ```

6. **Verify it's running:**
   ```bash
   curl http://localhost:8080/api/health
   # Expected: {"status":"ok"}
   ```

## 🔧 Development Workflow

### Adding a New Feature (TDD Workflow)

**Follow this exact order** - it's Test-Driven Development (TDD):

```
1. Create Entity           → See model/README.md
   └─ Define database schema (ProductEntity)

2. Create Repository Interface → See repository/README.md
   └─ Define contract (ProductRepository)

3. Generate Mocks
   └─ Run: make mocks

4. Implement Repository    → See repository/README.md
   └─ GORM implementation with real database logic

5. Define Request/Response Models → See model/README.md
   ├─ Create Request DTOs (input structure)
   └─ Create Response DTOs (output structure)

6. Write Service Tests FIRST → See service/README.md
   └─ Write failing tests using mocks (TDD!)

7. Implement Service       → See service/README.md
   ├─ Define service interface
   └─ Implement business logic to pass tests

8. Create Handler
   ├─ Write handler function
   └─ Add routes to router.go

9. Wire Dependencies
   └─ Update di/container.go
```

**Key Point**: Define Request/Response models (step 5) BEFORE writing tests (step 6) - how else would you know what inputs/outputs to test?

### Example: Adding a "Product" Feature (Following TDD)

**Step 1: Create Entity** (See [`model/README.md`](./model/README.md))

```go
// model/entity/product.go
package entity

import (
    "time"
    "github.com/google/uuid"
    "gorm.io/gorm"
)

type ProductEntity struct {
    ID        uuid.UUID      `gorm:"primaryKey"`
    Name      string         `gorm:"not null"`
    Price     float64        `gorm:"not null"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`
}
```

**Step 2: Create Repository Interface** (See [`repository/README.md`](./repository/README.md))

```go
// repository/interfaces/product.repository_interface.go
package interfaces

import (
    "context"
    "github.com/google/uuid"
    "github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

type ProductRepository interface {
    Create(ctx context.Context, product entity.ProductEntity) (*uuid.UUID, error)
    FindByID(ctx context.Context, id uuid.UUID) (*entity.ProductEntity, error)
}
```

**Step 3: Generate Mocks**

```bash
make mocks
```

This generates `internal/repository/mock/product.repository_mock.go`

**Step 4: Implement Repository** (See [`repository/README.md`](./repository/README.md))

```go
// repository/implementations/product/product.gorm.go
package product

import (
    "github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
    "github.com/ferriyusra/clean-arch-go-gin/internal/repository/interfaces"
    "gorm.io/gorm"
)

type GORMProductRepository struct {
    db *gorm.DB
}

func NewGORMProductRepository(db *gorm.DB) (interfaces.ProductRepository, error) {
    if err := db.AutoMigrate(&entity.ProductEntity{}); err != nil {
        return nil, err
    }
    return &GORMProductRepository{db: db}, nil
}

// repository/implementations/product/create.gorm.go
func (r *GORMProductRepository) Create(ctx context.Context, product entity.ProductEntity) (*uuid.UUID, error) {
    if product.ID == uuid.Nil {
        product.ID = uuid.New()
    }
    if err := r.db.WithContext(ctx).Create(&product).Error; err != nil {
        return nil, err
    }
    return &product.ID, nil
}
```

**Step 5: Define Request/Response Models** (See [`model/README.md`](./model/README.md))

```go
// model/request/product.go
package request

type CreateProductRequest struct {
    Name  string  `json:"name"`
    Price float64 `json:"price"`
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

**Step 6: Write Service Tests FIRST** (See [`service/README.md`](./service/README.md) - TDD!)

```go
// service/product/create_product.service_test.go
package product

import (
    "context"
    "testing"
    "go.uber.org/mock/gomock"
    "github.com/google/uuid"
    "github.com/ferriyusra/clean-arch-go-gin/internal/repository/mock"
    "github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
)

func TestCreateProduct(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    mockRepo := mock.NewMockProductRepository(ctrl)
    svc := NewProductService(mockRepo)

    // Setup mock expectation
    productID := uuid.New()
    mockRepo.EXPECT().
        Create(gomock.Any(), gomock.Any()).
        Return(&productID, nil).
        Times(1)

    // Test
    req := &request.CreateProductRequest{
        Name:  "Test Product",
        Price: 99.99,
    }
    result, err := svc.CreateProduct(context.Background(), req)

    // Assert
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if result == nil {
        t.Error("expected result, got nil")
    }
}
```

**Step 7: Implement Service** (Make tests pass)

```go
// service/product/product.service.go
package product

import (
    "context"
    "github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
    "github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
    "github.com/ferriyusra/clean-arch-go-gin/internal/repository/interfaces"
)

type ProductService interface {
    CreateProduct(ctx context.Context, req *request.CreateProductRequest) (*response.GetProduct, error)
}

type productService struct {
    repo interfaces.ProductRepository
}

func NewProductService(repo interfaces.ProductRepository) ProductService {
    return &productService{repo: repo}
}

// service/product/create_product.service.go
func (s *productService) CreateProduct(ctx context.Context, req *request.CreateProductRequest) (*response.GetProduct, error) {
    product := entity.ProductEntity{
        Name:  req.Name,
        Price: req.Price,
    }
    
    id, err := s.repo.Create(ctx, product)
    if err != nil {
        return nil, err
    }
    
    return &response.GetProduct{
        ID:    *id,
        Name:  req.Name,
        Price: req.Price,
    }, nil
}
```

Run tests: `go test ./internal/service/product -v` ✅ Tests should now pass!

**Step 8: Create Handler**

```go
// api/handler/product.handler.go
package handler

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/ferriyusra/clean-arch-go-gin/internal/service/product"
    "github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
    "github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

type ProductHandler struct {
    service product.ProductService
}

func NewProductHandler(service product.ProductService) *ProductHandler {
    return &ProductHandler{service: service}
}

func (h *ProductHandler) CreateProduct(c *gin.Context) {
    var req request.CreateProductRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, response.Err("Invalid request"))
        return
    }

    result, err := h.service.CreateProduct(c.Request.Context(), &req)
    if err != nil {
        c.JSON(http.StatusInternalServerError, response.Err(err.Error()))
        return
    }

    c.JSON(http.StatusCreated, response.OK("Product created", result))
}
```

**Step 9: Wire Dependencies**

```go
// di/container.go - Add to NewContainer()
productRepo, _ := productRepoImpl.NewGORMProductRepository(db)
productSvc := productService.NewProductService(productRepo)
productHandler := handler.NewProductHandler(productSvc)

// api/router.go - Add routes
func SetupProductRoutes(r *gin.Engine, handler *handler.ProductHandler) {
    api := r.Group("/api/products")
    api.POST("", handler.CreateProduct)
    api.GET("/:id", handler.GetProduct)
}
```

**Verify**: Start server (`make dev`) and test: `curl -X POST http://localhost:8080/api/products -d '{"name":"Widget","price":29.99}'`

## 📍 API Endpoints

### Public Routes

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Health check |
| GET | `/api/message` | Get message |
| GET | `/api/counter` | Get counter value |
| POST | `/api/counter` | Increment counter |

### Authentication Routes (Public)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/auth/register` | Register new user |
| POST | `/api/auth/login` | Login (returns JWT tokens) |
| POST | `/api/auth/logout` | Logout user |
| POST | `/api/auth/refresh` | Refresh access token |
| GET | `/api/auth/csrf` | Get CSRF token |

### Protected Routes (Requires JWT)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/auth/me` | Get current user info |

## 🧪 Testing

### Running Tests

```bash
# All tests
make test

# Verbose output
make test-verbose

# With coverage report
make test-coverage
```

### Test Coverage by Layer

- **Repository**: Tested via service layer mocks
- **Service**: Unit tests with mocked repositories
- **Handler**: Integration tests (future)

For detailed testing guides, see:
- Service testing: [`service/README.md`](./service/README.md)
- Repository mocks: [`repository/README.md`](./repository/README.md)

## 🔐 Authentication & Security

### JWT Token Flow

1. **Register/Login**: Returns `access_token` (15 min) and `refresh_token` (7 days)
2. **API Requests**: Include `Authorization: Bearer <access_token>` header
3. **Token Refresh**: Use `/api/auth/refresh` with refresh token to get new access token
4. **Protected Routes**: Validated via `AuthMiddleware`

### CSRF Protection

For state-changing operations:
1. Get token: `GET /api/auth/csrf`
2. Include in request: `X-CSRF-Token: <token>`

### Security Best Practices

- ✅ Passwords hashed with bcrypt
- ✅ JWT secrets from environment variables
- ✅ CORS configured
- ✅ Context-aware request handling
- ✅ Soft deletes for audit trails

## 🏭 Production Deployment

### Build for Production

```bash
make build
```

Outputs:
- `bin/server` - Optimized binary with embedded frontend

### Environment Configuration

**For Production:**

```env
# Server
SERVER_PORT=8080
SERVER_HOST=0.0.0.0

# Database (PostgreSQL recommended)
DATABASE_TYPE=postgres
DATABASE_DSN=postgresql://user:password@localhost:5432/dbname?sslmode=require

# JWT Secrets (GENERATE NEW ONES!)
JWT_ACCESS_SECRET=<generate-strong-secret>
JWT_REFRESH_SECRET=<generate-strong-secret>

# Timeouts
SERVER_READ_TIMEOUT=15s
SERVER_WRITE_TIMEOUT=15s
SERVER_IDLE_TIMEOUT=60s

# Database Pooling
DATABASE_MAX_OPEN_CONNS=25
DATABASE_MAX_IDLE_CONNS=5
DATABASE_CONN_MAX_LIFETIME=5m

# Production Mode
DEV_MODE=false
```

### Running in Production

**Option 1: Direct Binary**
```bash
./bin/server
```

**Option 2: Docker Compose**
```bash
docker-compose -f docker-compose.prod.yml up -d
```

**Option 3: Systemd Service**
```ini
[Unit]
Description=Clean Go Vite React Backend
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/app
ExecStart=/opt/app/bin/server
Restart=always
Environment="DATABASE_TYPE=postgres"
Environment="DATABASE_DSN=postgresql://..."

[Install]
WantedBy=multi-user.target
```

## 📊 Database Management

### Supported Databases

- **SQLite**: Default for development (no setup required)
- **PostgreSQL**: Recommended for production

### Automatic Migrations

Migrations run automatically via GORM's `AutoMigrate()` when repositories are initialized. See each repository's constructor in `repository/implementations/`.

### Switching to PostgreSQL

1. Install PostgreSQL
2. Create database:
   ```bash
   createdb myapp
   ```
3. Update `.env`:
   ```env
   DATABASE_TYPE=postgres
   DATABASE_DSN=postgresql://user:pass@localhost:5432/myapp?sslmode=disable
   ```

## 🐛 Troubleshooting

### Port Already in Use
```bash
# Find and kill process on port 8080
lsof -ti:8080 | xargs kill -9
```

### Database Locked (SQLite)
```bash
# Development only - delete and restart
rm dev.db
make server
```

### Module Not Found
```bash
go mod tidy
go mod download
```

### Hot Reload Not Working
```bash
# Reinstall Air
go install github.com/air-verse/air@latest

# Check .air.toml configuration
cat .air.toml
```

## 📝 Code Standards

### General Guidelines

1. **Dependency Direction**: Always depend on interfaces, never concrete types
2. **Error Handling**: Handle all errors explicitly, never ignore
3. **Context Usage**: Pass `context.Context` as first parameter in all operations
4. **Testing**: Write tests before implementation (TDD)
5. **Naming**: Use descriptive names, avoid abbreviations

### Layer-Specific Standards

| Layer | Key Rules |
|-------|-----------|
| **Models** | No business logic, pure data structures |
| **Repositories** | Use UUID for IDs, always accept context |
| **Services** | Business logic only, use Request/Response DTOs |
| **Handlers** | Minimal logic, delegate to services |

For detailed standards, see the README in each layer's directory.

## 🔗 Key Dependencies

From `go.mod`:

- **[Gin](https://gin-gonic.com/)**: High-performance HTTP framework
- **[GORM](https://gorm.io/)**: ORM with PostgreSQL/SQLite support
- **[JWT-Go](https://github.com/golang-jwt/jwt)**: JWT token handling
- **[UUID](https://github.com/google/uuid)**: UUID generation
- **[Bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt)**: Password hashing
- **[GoDotEnv](https://github.com/joho/godotenv)**: Environment variables
- **[GoMock](https://github.com/uber-go/mock)**: Mock generation for testing

## 📚 Further Reading

- **Implementation Guides**:
  - [Model Layer Guide](./model/README.md) - Entities, Requests, Responses
  - [Service Layer Guide](./service/README.md) - TDD workflow
  - [Repository Layer Guide](./repository/README.md) - GORM implementation

- **External Resources**:
  - [Clean Architecture by Uncle Bob](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)
  - [Gin Framework Docs](https://gin-gonic.com/docs/)
  - [GORM Documentation](https://gorm.io/docs/)
  - [Go Testing Best Practices](https://go.dev/doc/tutorial/add-a-test)

## 🤝 Contributing

When contributing:

1. Follow existing architecture patterns
2. Read the relevant layer's README before making changes
3. Write tests for new functionality
4. Use dependency injection throughout
5. Keep business logic in services, not handlers
6. Update documentation for new features

---

**Quick Reference:**
- Entry Point: `cmd/server/main.go`
- DI Container: `internal/di/container.go`
- Route Config: `internal/api/router.go`
- Framework: Gin
- ORM: GORM
- Architecture: Clean Architecture with Dependency Injection