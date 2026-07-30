# Model Package Developer Guide

## Overview

The `model` package is the central location for all data structures used in the backend application. It organizes types into three main categories:

- **Entity**: Database models (GORM) representing actual database tables
- **Request**: Incoming API request payload structures
- **Response**: Outgoing API response payload structures

## Directory Structure

```
model/
├── entity/         # Database entities (GORM models)
├── request/        # API request DTOs
├── response/       # API response DTOs
└── README.md
```

## Adding New Models

### 1. Entity Models

Create a new file in `entity/` directory for your database model.

**Conventions:**
- Suffix the struct name with `Entity` (e.g., `UserEntity`)
- Use `uuid.UUID` for primary keys
- Add timestamp fields: `CreatedAt`, `UpdatedAt`
- Add `DeletedAt gorm.DeletedAt` **only when you want soft deletes.** Including it
  turns every `Delete` into an `UPDATE`, and rows stay in the table where a unique
  index still sees them. Security-sensitive records — revoked tokens, for example —
  should be hard deleted, so `RefreshTokenEntity` deliberately has no `DeletedAt`.
- Index the columns you filter on (`gorm:"index"`), and add `not null`/`size` where
  the constraint is real — AutoMigrate will create them
- **Register the entity in `migrationModels`** in `platform/migrate.go`, or its table
  is never created

**Example:**
```go
package entity

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ProductEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	Name      string
	Price     float64
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}
```

### 2. Request Models

Create a new file in `request/` directory for your API request payloads.

**Conventions:**
- Suffix the struct name with `Request` (e.g., `CreateProductRequest`)
- Include JSON tags for request body binding
- Only include fields that are expected from the client
- **Declare validation with `binding` tags.** This is the only place input rules
  belong — handlers must not hand-roll `if field == ""` checks. `c.ShouldBindJSON`
  enforces the tags and `respondBindError` reports failures per field.

**Example:**
```go
package request

type CreateProductRequest struct {
	Name  string  `json:"name" binding:"required,max=255"`
	Price float64 `json:"price" binding:"required,gt=0"`
}
```

See [Validation](#validation) for the tags in use and how errors are rendered.

### 3. Response Models

Create a new file in `response/` directory for your API response payloads.

**Conventions:**
- Name the struct based on the action (e.g., `GetProduct`, `ListProducts`)
- Include JSON tags for response serialization
- Only include fields that should be exposed to the client
- Exclude sensitive data (passwords, secrets, etc.)

**Example:**
```go
package response

import "github.com/google/uuid"

type GetProduct struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Price float64   `json:"price"`
}
```

## Common Patterns

### Response Wrappers

Use `CommonIDResponse` for simple ID-only responses:

```go
package response

type CommonIDResponse struct {
	ID uuid.UUID `json:"value"`
}
```

### Data Flow

Typical request → response flow:

1. **Client sends request** → `request.CreateProductRequest`
2. **Handler receives request** → Maps to `entity.ProductEntity` or business logic
3. **Handler returns response** → `response.GetProduct` with relevant fields

### Mapping Between Layers

Always map between Request → Entity → Response to:
- Prevent exposing unnecessary fields
- Maintain separation of concerns
- Control what clients can see/modify

**Example:**
```go
// request.CreateProductRequest → entity.ProductEntity → response.GetProduct
product := &entity.ProductEntity{
	ID:    uuid.New(),
	Name:  req.Name,
	Price: req.Price,
}

// Later, when responding:
return &response.GetProduct{
	ID:    product.ID,
	Name:  product.Name,
	Price: product.Price,
}
```

## Existing Models

### User
- **Entity**: `UserEntity` — account record; `Password` holds a bcrypt digest, never
  plaintext, and is `[]byte` so it is awkward to accidentally log as a string
- **Request**: `RegisterUserRequest`, `LoginRequest`, `RefreshTokenRequest`
- **Response**: `GetUser`, `RegisterResponse`, `LoginResponse`, `RefreshResponse`

Note that no response type exposes `Password` — `GetUser` carries only `id`, `email`,
and `name`. Keep it that way when you extend it.

### Refresh token
- **Entity**: `RefreshTokenEntity` — stores `TokenHash`, the SHA-256 digest of a
  refresh token, under a unique index. The raw token is never persisted, so a database
  leak yields nothing replayable. No `DeletedAt`: revocation must be permanent.
- No request or response types — refresh tokens are handled inside the auth flow.

### Health
- **Response**: `HealthStatus` — `status`, `message`, and a free-form `details` map
  used by the liveness and readiness probes

## Best Practices

1. **Naming**: Use consistent suffixes (`Entity`, `Request`, `Response`)
2. **Separation**: Never mix layers (request ≠ response ≠ entity)
3. **Security**: Exclude sensitive fields from responses
4. **Timestamps**: Always include `CreatedAt`, `UpdatedAt` for entities
5. **Soft Deletes**: Include `DeletedAt` field for auditing capability
6. **Type Safety**: Use `uuid.UUID` for IDs instead of strings
7. **Tags**: Always include JSON tags for serialization
8. **Documentation**: Add brief comments for public types

## Using Models in Handlers

A handler binds a request DTO and returns a response DTO. It never touches an entity —
that is the repository layer's business, and importing `entity` from a handler is a
layering violation.

```go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

func (h *ProductHandler) Create(c *gin.Context) {
	req := &request.CreateProductRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		respondBindError(c, err) // renders per-field validation messages
		return
	}

	// The service owns the entity and returns a response DTO.
	resp, err := h.productService.Create(c.Request.Context(), req)
	if err != nil {
		respondServiceError(c, err) // sentinel → status; anything else → generic 500
		return
	}

	c.JSON(http.StatusCreated, response.OK("Product created", resp))
}
```

Always wrap the payload in `response.OK`/`Err`/`ValidationErr` so every endpoint
returns the same envelope.

## Import Paths

```go
import (
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)
```

## Migration & Database Considerations

When adding a new `Entity` type:

1. Create the struct in `entity/` with proper GORM tags
2. **Add it to `migrationModels` in [`platform/migrate.go`](../platform/migrate.go).**
   Repository constructors do not migrate — this list is the single place the schema is
   declared, and forgetting it means the table is silently never created.
3. Decide deliberately whether the entity needs `DeletedAt` (soft delete) — see the
   entity conventions above
4. Verify against both drivers if you support Postgres as well as SQLite; column types
   and index behaviour differ

`AutoMigrate` only adds columns and indexes. It never alters or drops them, so changing
a column's type or removing a field needs a real migration tool. Production runs with
`DATABASE_AUTO_MIGRATE=false` for exactly this reason.

## Validation

Validation is declared with **`binding`** tags, which is the tag Gin's validator reads.
`validate` tags are silently ignored by `ShouldBindJSON` — using them means no
validation runs at all.

```go
type CreateProductRequest struct {
	Name  string  `json:"name" binding:"required,min=1,max=255"`
	Price float64 `json:"price" binding:"required,gt=0"`
}
```

Tags in use in this project:

| Tag | Meaning |
|-----|---------|
| `required` | Must be present and non-zero |
| `email` | Must parse as an email address |
| `min` / `max` | Length bounds for strings |
| `gt` / `gte` / `lt` / `lte` | Numeric bounds |

The full set is documented by
[validator](https://pkg.go.dev/github.com/go-playground/validator/v10).

### How failures are reported

`RegisterValidationTagNames` (called once at startup) makes the validator report the
**JSON** field name, so clients see `email` rather than `Email`. `respondBindError`
then produces:

```json
{
  "success": false,
  "message": "Validation failed",
  "errors": { "name": "name is required", "price": "Failed the \"gt\" rule" }
}
```

A body that cannot be parsed at all is not a validation failure — it returns the
generic `Invalid request body` with no `errors` object. To improve the wording for a
tag, extend `validationMessage` in
[`api/handler/validation.go`](../api/handler/validation.go).

### Constraints worth copying

- Cap password fields at **72 bytes** (`max=72`). bcrypt ignores everything past 72, so
  without the cap part of a longer password is never actually verified.
- Bound every free-text field with `max` so a client cannot post unbounded input.
