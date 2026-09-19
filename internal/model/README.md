# Model Package Developer Guide

## Overview

The `model` package holds every data structure that crosses a layer boundary,
split into three kinds that never mix:

- **Entity** — GORM models; the shape of a database row. They live *below* the
  service layer and must never reach a handler.
- **Request** — what a client sends, with its validation rules in `binding:`
  tags.
- **Response** — what a client receives, plus the `APIResponse` envelope every
  endpoint answers with.

```
model/
├── entity/         # database entities (GORM models)
├── request/        # API request DTOs
├── response/       # API response DTOs + wrapper.go (the envelope) + naming_test.go
└── README.md
```

The separation is what stops a password hash reaching a response body by
accident, and it is why `ChangePassword` can take `currentPassword` without that
field existing anywhere near the database schema.

## Entity Models

Create a file per table in `entity/`.

**Conventions**

- Suffix the type with `Entity`.
- `uuid.UUID` primary key, tagged `gorm:"primaryKey"`.
- `CreatedAt` / `UpdatedAt` on everything.
- An explicit `TableName()`, so renaming the Go type is not a schema change.
- `gorm.DeletedAt` **only where a soft delete is what you want**.

```go
package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserEntity represents a user in the system
type UserEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	Email     string    `gorm:"uniqueIndex"`
	Password  []byte
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UserEntity) TableName() string {
	return "user_entities"
}
```

### The soft-delete decision is not a default

`RefreshTokenEntity` deliberately has **no** `gorm.DeletedAt`:

```go
type RefreshTokenEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	UserID    uuid.UUID `gorm:"index"`
	TokenHash string    `gorm:"uniqueIndex;size:64"`
	ExpiresAt time.Time `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

A soft-deleted row still occupies the unique index on `TokenHash`, and a revoked
credential that lingers in the table is one that can be restored. Revocation has
to be a real delete. `UserEntity` goes the other way on purpose: deleting an
account hides the row from every query the application makes — the lookups
behind login and refresh included — while the row itself survives.

Note also what is *not* in that struct: the token. Only a SHA-256 digest is
stored, so a leaked database dump yields nothing replayable.

### Registering a new table

Two edits in [`internal/platform/migrate.go`](../platform/migrate.go):

1. add the type to `entities()`;
2. **append** a `Migration` with the next version number.

The schema is a versioned, forward-only ledger — a `schema_migrations` table and
an ordered list — not an `AutoMigrate` sweep at boot. Never edit or renumber an
existing migration: a version that has already run somewhere is a fact, and a
startup check rejects duplicates and out-of-order versions. See
[`../repository/README.md`](../repository/README.md#schema-changes) for the
detail.

## Request Models

One file per domain in `request/`. Validation is declarative: every rule lives in
a `binding:` tag, so it is visible in one place and handlers stay free of
hand-rolled checks.

```go
// RegisterUserRequest is the payload for POST /api/v1/auth/register.
type RegisterUserRequest struct {
	Email    string `json:"email"    binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Name     string `json:"name"     binding:"required,min=1,max=100"`
}
```

**Conventions**

- Suffix the type with `Request`.
- `json:` tags on every field. They are not only for decoding: `handler.Fail`
  registers a tag-name function with the validator so a rejected field is
  reported under its JSON name (`email`), not its Go name (`Email`). Without the
  tag a client gets back a key it never sent and cannot highlight.
- Include only what the client actually supplies. An ID that comes from the
  access token is a function parameter, not a body field.

Two rules in there are load-bearing rather than arbitrary:

- **`max=72` on every password field.** bcrypt refuses inputs longer than 72
  bytes, so without the cap a long password surfaces as a 500 from the hashing
  step instead of as a field-level validation error.
- **`LoginRequest` validates presence only** — no `min=8`. Rejecting a malformed
  password at login would tell an attacker which stored passwords cannot exist.
  `ChangePasswordRequest` splits the difference: the new password carries the
  full rules, the current one only presence and the bcrypt ceiling, so a password
  that predates the `min=8` rule can still be replaced.

### Pagination

`request.Pagination` is the shared query-string window for list endpoints:

```go
type Pagination struct {
	Page  int `form:"page"  json:"page"  binding:"omitempty,min=1"`
	Limit int `form:"limit" json:"limit" binding:"omitempty,min=1,max=100"`
}
```

- **`form:` tags, not just `json:`.** These are `?page=2&limit=50` parameters on
  a GET, bound with `c.ShouldBindQuery` via `handler.BindPagination`. A `json:`
  tag alone would bind nothing.
- **Every rule is `omitempty`,** because the zero value has to mean "unset" —
  otherwise `min=1` would reject a plain `GET /resource` that named no page.
- **`max=100` is the rule that matters.** Without a ceiling, a client asks for
  `limit=1000000` and the database does the work: a scan, a result set
  materialised in memory and serialised, a connection tied up for the duration.
  That is a denial of service costing the caller one query string.
  `request.MaxLimit` mirrors the number so callers can quote it; the tag is what
  rejects.
- `Normalized()` returns a **copy** with the defaults applied (page 1, limit 20)
  rather than mutating the receiver, so the bound request still shows what the
  client actually asked for — which is what an access log or an error message
  should report. `Offset()` normalises first, so calling it on a zero value
  yields `0` and not a negative offset.

## Response Models

One file per domain in `response/`, plus `wrapper.go` for the envelope.

```go
// GetUser represents a user in the system
type GetUser struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}
```

**Conventions**

- Name for the thing, not the verb where possible: `GetUser`, `GetCounter`,
  `Session`, `SessionList`.
- Every exported field gets a `json:` tag, **camelCase** (see below).
- Never include a password, a token digest, or an internal id you do not want
  public.

### The envelope

Every response in this API — success, error, 404, even a recovered panic — has
the same shape, built by the helpers in `wrapper.go`:

```go
response.OK("User retrieved", user)
response.OKWithMeta("Sessions retrieved", sessions, meta)
response.Err("Email is already registered")
response.ValidationErr("Validation failed", map[string]string{"email": "Must be a valid email address"})
```

```json
{ "success": true,  "message": "User retrieved", "data": { } }
{ "success": false, "message": "Validation failed", "errors": { "email": "Must be a valid email address" } }
```

`APIResponse` also carries `requestId` and `traceId`, attached by
`WithCorrelation` on **error** responses only, so a user can quote an identifier
that leads straight to the server-side log line and the span. Successful
responses stay lean; the `X-Request-ID` header already carries the same value.
Both fields are `omitempty`, so `traceId` simply disappears when the request is
not part of a trace.

### Pagination metadata

```go
type Meta struct {
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

func NewMeta(page, limit int, total int64) *Meta
```

`Total` is `int64` because that is what a SQL `COUNT` returns and what every
counting repository method hands back. Narrowing it at the boundary would be a
conversion with nothing to gain and an overflow to lose on a 32-bit build.

The metadata belongs in the envelope's own `meta` field, not inside `data`, so a
paginated list has the same `data: [...]` shape as any other collection and a
client does not unwrap a different container just because a response happens to
be paginated. `GET /api/v1/auth/sessions` is the live example.

A service that pages carries the `Meta` on its own response type and lets the
handler lift it out:

```go
// SessionList is what UserService.ListSessions returns.
type SessionList struct {
	Sessions []Session `json:"sessions"`
	Meta     *Meta     `json:"-"`
}
```

`Meta` is `json:"-"` here because it does not belong in `data` — the handler
passes it to `response.OKWithMeta`. Carrying it on the struct keeps the service
as the place that knows the total, rather than making the handler count.

### `json:"-"` is a security tool, not a formatting one

```go
type LoginResponse struct {
	User         GetUser `json:"user"`
	AccessToken  string  `json:"-"`
	RefreshToken string  `json:"-"`
}
```

The handler needs the tokens from the service in order to set the HttpOnly
cookies; the client must never see them in a body, where they would land in logs,
proxies and browser caches. `json:"-"` is what makes those two facts compatible.
`RegisterResponse` and `ChangePasswordResponse` do the same.

`response.Session` takes it further and has no token field **at all**, not even a
truncated one for display: the stored digest is the only thing between a leaked
database row and a replayable credential. A test in `internal/api/handler`
serialises the type and fails if a digest appears in the output, because a
comment does not survive a refactor and a test does.

### camelCase, enforced

Every JSON key this API emits is camelCase: `requestId`, never `request_id`.
`naming_test.go` walks every response type with reflection and fails on a key
that breaks the rule, including an exported field with no tag at all (which would
serialize under its PascalCase Go name).

Go cannot enumerate a package's types at run time, so the list is maintained by
hand:

```go
func responseTypes() []any {
	return []any{
		APIResponse{}, Meta{}, GetUser{}, LoginResponse{}, RegisterResponse{},
		RefreshResponse{}, CSRFTokenResponse{}, HealthStatus{}, GetCounter{},
		GetMessage{}, CommonIDResponse{}, Session{}, SessionList{},
		ChangePasswordResponse{},
	}
}
```

**A new response type is not covered until it is added there.** That is the one
piece of manual upkeep in this package, and it is the last step of adding a
response model.

Log fields are the exception, and are not affected: they keep the OpenTelemetry
spelling (`trace_id`, `span_id`). The camelCase rule is about the HTTP API.

## Mapping between layers

Request → Entity → Response, always, so the wire format and the schema can move
independently:

```go
// handler: bind the request
req := &request.RegisterUserRequest{}
if !BindJSON(c, req) { return }

// service: build the entity
userEntity := entity.UserEntity{
	ID:       uuid.New(),
	Email:    req.Email,
	Password: hashedPassword,
	Name:     req.Name,
}

// service: build the response
user := response.GetUser{
	ID:    userEntity.ID,
	Email: userEntity.Email,
	Name:  userEntity.Name,
}
```

The mapping is written out by hand, and that is the point: the moment
`UserEntity` gains a field, nothing is exposed until someone decides to expose
it.

## Existing models

| Domain | Entity | Request | Response |
|---|---|---|---|
| User | `UserEntity` | `RegisterUserRequest`, `LoginRequest`, `ChangePasswordRequest` | `GetUser`, `LoginResponse`, `RegisterResponse`, `RefreshResponse`, `ChangePasswordResponse` |
| Sessions | `RefreshTokenEntity` | `Pagination` | `Session`, `SessionList` |
| Counter | `CounterEntity` | — | `GetCounter` |
| Message | `MessageEntity` | — | `GetMessage` |
| CSRF | — | — | `CSRFTokenResponse` |
| Health | — | — | `HealthStatus` |
| Shared | — | `Pagination` | `APIResponse`, `Meta`, `CommonIDResponse` |

## Import paths

```go
import (
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)
```

A handler importing `entity` is a smell: entities stop at the service layer.

## Checklist for a new model

- [ ] Entity: UUID PK, timestamps, `TableName()`, a deliberate answer on
      `gorm.DeletedAt`.
- [ ] Entity registered in `entities()` **and** given an appended `Migration`.
- [ ] Request: `json:` tags, `binding:` rules, `form:` tags if it binds from a
      query string.
- [ ] Response: camelCase `json:` tags, `json:"-"` on anything that must not
      reach a body.
- [ ] Response type added to `responseTypes()` in `naming_test.go`.
- [ ] `go test ./internal/model/...` passes.
