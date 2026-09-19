# Authentication System

This document describes the authentication system for this Go API.

There is no frontend in this repository — it is the backend-only extraction of a
larger project. The browser-side snippets below are written for whatever client
you put in front of it, and are kept because the cookie and CSRF handling they
show is the part that is easy to get wrong.

## Overview

The authentication system is designed as a **pure API concern** with the frontend as a dumb HTTP client. It uses:

- **HTTP-only cookies** for token storage (no localStorage, no XSS vectors)
- **JWT tokens** for stateless authentication
- **CSRF protection** with `SameSite=Lax` and token-based validation
- **Bcrypt password hashing** with default cost factor

This approach works identically whether the client is served from the same
origin, from a dev server proxying to this API, or from a separate domain
altogether — the last of which needs the origin listed in `ALLOWED_ORIGINS`.

## Architecture Principles

### Design Goals

1. **Backend owns auth entirely** — All logic resides in the API
2. **Frontend is a dumb HTTP client** — Uses standard `fetch` with `credentials: 'include'`
3. **No special dev-only hacks** — Same auth flow everywhere
4. **Easy to detach later** — Zero coupling to frontend framework

### Why HTTP-Only Cookies?

| Concern | Cookies | Bearer Tokens |
|---------|---------|---------------|
| Same-origin or proxied client | ✅ Automatic | ⚠️ CORS headers needed |
| Single-binary deployment | ✅ Seamless | ✅ Works |
| XSS resistance | ✅ HttpOnly flag | ❌ Token readable |
| CSRF protection | ✅ Built-in | ⚠️ Manual handling |
| Detaching frontend | ✅ No changes | ⚠️ More config |
| Mobile/CLI clients | ⚠️ Less ideal | ✅ Better |

For this **web-first** repo, cookies are the pragmatic choice.

## API Response Format

All API responses use a standardized envelope:

```json
// Success
{ "success": true, "message": "...", "data": { ... } }

// Error
{ "success": false, "message": "Error description" }

// Validation error
{ "success": false, "message": "Validation failed", "errors": { "field": "message" } }

// Paginated
{ "success": true, "message": "...", "data": [...], "meta": { "page": 1, "limit": 20, "total": 42 } }
```

Error responses also carry `requestId`, and `traceId` when the request was part
of a trace, so a user can quote an identifier that leads straight to the
server-side log line and span.

## API Endpoints

Every endpoint below lives under `/api/v1`. Only the health probes
(`/api/health`, `/api/health/live`, `/api/health/ready`) are unversioned, because
they are a contract with the orchestrator rather than with an API client.

### Public Endpoints

#### `POST /api/v1/auth/register`

Register a new user account.

**Request:**
```json
{
  "email": "user@example.com",
  "password": "secure-password",
  "name": "John Doe"
}
```

**Response (201 Created):**
```json
{
  "success": true,
  "message": "Registration successful",
  "data": {
    "user": {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "email": "user@example.com",
      "name": "John Doe"
    }
  }
}
```

**Cookies Set:**
- `access_token` (JWT, 15 minutes)
- `refresh_token` (JWT, 7 days)

**Error Responses:**
- `400 Bad Request` — Missing or invalid fields (with field-level `errors` map)
- `409 Conflict` — Email already registered

**Validation Error Example:**
```json
{
  "success": false,
  "message": "Validation failed",
  "errors": {
    "email": "This field is required",
    "password": "This field is required"
  }
}
```

---

#### `POST /api/v1/auth/login`

Authenticate an existing user.

**Request:**
```json
{
  "email": "user@example.com",
  "password": "secure-password"
}
```

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Login successful",
  "data": {
    "user": {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "email": "user@example.com",
      "name": "John Doe"
    }
  }
}
```

**Cookies Set:**
- `access_token` (JWT, 15 minutes)
- `refresh_token` (JWT, 7 days)

**Error Responses:**
- `400 Bad Request` — Missing email or password (with field-level `errors` map)
- `401 Unauthorized` — Invalid credentials

---

#### `POST /api/v1/auth/refresh`

Generate a new access token using the refresh token.

**Request:**
No body required. Refresh token is sent via cookie.

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Token refreshed"
}
```

**Cookies Set:**
- `access_token` (JWT, 15 minutes, replaces old)
- `refresh_token` (JWT, 168 hours, replaces old)

Refresh rotates the *pair*, not just the access token: the presented refresh
token is deleted and both cookies are replaced. A refresh token that verifies
but has no matching row is treated as a replay and revokes every session for
that user.

**Error Response (401):**
```json
{ "success": false, "message": "Missing refresh token" }
```

---

#### `GET /api/v1/csrf`

Get a CSRF token for state-changing operations.

**Request:**
No parameters required.

**Response (200 OK):**
```json
{
  "success": true,
  "message": "CSRF token generated",
  "data": {
    "token": "3f1a...c7d9.00000000663f1a80.b81e...4af2"
  }
}
```

The real token is three hex segments separated by dots — a 32-byte nonce, the
8-byte issue time, and the 32-byte HMAC over both — so roughly 146 characters.
Treat it as opaque: send it back verbatim in `X-CSRF-Token`.

---

### Protected Endpoints

All protected endpoints require a valid `access_token` cookie.

#### `GET /api/v1/auth/me`

Get the current authenticated user's information.

**Request Headers:**
- Cookie: `access_token=...` (automatic via browser)

**Response (200 OK):**
```json
{
  "success": true,
  "message": "User retrieved",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "user@example.com",
    "name": "John Doe"
  }
}
```

**Error Response (401):**
```json
{ "success": false, "message": "Missing authentication token" }
```

---

#### `POST /api/v1/auth/logout`

Clear authentication cookies and logout the user.

**Request Headers:**
- Cookie: `access_token=...` (automatic via browser)

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Logged out successfully"
}
```

**Cookies Cleared:**
- `access_token` (MaxAge: -1)
- `refresh_token` (MaxAge: -1)

---

#### `GET /api/v1/auth/sessions`

List the caller's active sessions, newest first. A "session" here is a live
refresh token: that row is the only server-side record of a signed-in device.
Expired rows are excluded, so what comes back is what can still authenticate.

No CSRF token is required — this is a read, and there is no action for a
third-party page to perform as the user.

**Query Parameters:**

| Name | Default | Rules |
|---|---|---|
| `page` | `1` | whole number, 1 or greater |
| `limit` | `20` | whole number, 1 to 100 |

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Sessions retrieved",
  "data": [
    {
      "id": "8f14e45f-ceea-467a-9f2a-1d3f4a5b6c7d",
      "current": true,
      "createdAt": "2024-05-01T09:12:44Z",
      "expiresAt": "2024-05-08T09:12:44Z"
    }
  ],
  "meta": { "page": 1, "limit": 20, "total": 3 }
}
```

`current` marks the session the calling request is authenticated by, which is
what lets a "sign out my other devices" screen avoid signing the user out of the
device they are looking at. It is decided by hashing the refresh cookie and
comparing digests; a request that carries no refresh cookie is still valid and
simply marks nothing as current.

Nothing that identifies a token crosses this boundary — there is no token field
and no truncated digest, because the stored digest is the only thing between a
leaked database row and a replayable credential. A handler test fails if a digest
ever appears in this response.

**Error Responses:**
- `400 Bad Request` — `?limit=500` or `?page=abc`, with a field-level `errors` map
- `401 Unauthorized` — missing or expired access token

---

#### `PATCH /api/v1/auth/password`

Change the password. Requires the `access_token` cookie **and** the
`X-CSRF-Token` header.

**Request:**
```json
{
  "currentPassword": "secure-password",
  "newPassword": "an-even-better-password"
}
```

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Password changed"
}
```

**Cookies Set:**
- `access_token` (JWT, 15 minutes, replaces old)
- `refresh_token` (JWT, 168 hours, replaces old)

Changing a password revokes **every** session for the account, including the one
that made the request — the new hash and the revocation are a single transaction,
so a state where the password has changed but the old sessions still work cannot
exist, not even for the moment between two statements. That is the point of the
change: a user does it because they believe someone else has their credentials.
A fresh pair is then minted for the acting client, which is why this endpoint
sets cookies: the device that asked stays signed in, every other device is
signed out.

**Error Responses:**
- `400 Bad Request` — missing field, or a new password outside 8–72 characters
- `401 Unauthorized` — missing access token, or the wrong current password
- `403 Forbidden` — missing or invalid `X-CSRF-Token`

A wrong current password and an account that no longer exists give the *same*
401 (`Invalid email or password`). Distinguishing them would turn a protected
endpoint into an account-existence oracle for anyone holding a stale token, and
the client's next move is the same either way.

---

#### `DELETE /api/v1/auth/me`

Delete the account and every session that belongs to it. Requires the
`access_token` cookie **and** the `X-CSRF-Token` header.

**Request:**
No body.

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Account deleted"
}
```

**Cookies Cleared:**
- `access_token` (MaxAge: -1)
- `refresh_token` (MaxAge: -1)

The cookies are cleared only *after* the delete succeeds; clearing them first
would sign the user out of an account that still exists if the delete failed.

The two deletions are one transaction, and they are not symmetrical. The user row
is soft-deleted — it stops being visible to every query the application makes,
the lookups behind login and refresh included, but the row survives. The refresh
token rows are really gone, because a revoked credential that lingers is one that
can be restored.

**Error Responses:**
- `401 Unauthorized` — missing or expired access token
- `403 Forbidden` — missing or invalid `X-CSRF-Token`
- `404 Not Found` — the account is already gone

---

## Token Details

### Access Token (JWT)

**Type:** JWT (HMAC-SHA256)  
**Expiry:** 15 minutes  
**Storage:** HTTP-only cookie  
**Purpose:** Authenticate API requests

**Claims:**
```json
{
  "sub": "550e8400-e29b-41d4-a716-446655440000",
  "email": "user@example.com",
  "name": "John Doe",
  "iat": 1699500000,
  "exp": 1699500900,
  "iss": "go-vite-react"
}
```

### Refresh Token (JWT)

**Type:** JWT (HMAC-SHA256)  
**Expiry:** 7 days  
**Storage:** HTTP-only cookie  
**Purpose:** Obtain new access tokens

**Claims:**
```json
{
  "sub": "550e8400-e29b-41d4-a716-446655440000",
  "jti": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6",
  "iat": 1699500000,
  "exp": 1699608000,
  "iss": "go-vite-react"
}
```

`jti` is load-bearing rather than decorative. Without a random token id, two
refresh tokens minted for the same user in the same second would be byte for
byte identical — so rotation would hand back the token it was supposed to
replace, and the digest of the old one would still match the new one.

---

## Cookies

### Access Token Cookie

```http
Set-Cookie: access_token=eyJ...; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=900
```

- **HttpOnly:** Prevents JavaScript access (XSS protection)
- **Secure:** Only sent over HTTPS (set in production)
- **SameSite=Lax:** CSRF protection for same-site requests
- **Max-Age=900:** 15 minutes

### Refresh Token Cookie

```http
Set-Cookie: refresh_token=eyJ...; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=604800
```

- **HttpOnly:** Prevents JavaScript access
- **Secure:** Only sent over HTTPS (set in production)
- **SameSite=Lax:** CSRF protection
- **Max-Age=604800:** 7 days

---

## CSRF Protection

### Token Format

The token is stateless and self-verifying:

```
hex(nonce) "." hex(issuedAt) "." hex(hmac-sha256(nonce + issuedAt))
```

Validation recomputes the MAC over the first two segments and compares. Because
the issue time is inside the signed material, it cannot be edited to extend a
captured token, and it is only read *after* the MAC verifies. Nothing is stored
server-side, which is why the token has to expire on its own: `CSRF_TOKEN_TTL`
(default 12h) is the only thing that limits a leaked one, since a stateless
token cannot be revoked.

### Strategy

**SameSite=Lax** covers most cases. For additional protection on sensitive operations:

1. **Frontend requests CSRF token:**
   ```typescript
   const body = await fetch('/api/v1/csrf', { credentials: 'include' }).then(r => r.json());
   const csrfToken = body.data.token;
   ```

2. **Frontend sends token in header:**
   ```typescript
   fetch('/api/v1/auth/password', {
     method: 'PATCH',
     credentials: 'include',
     headers: {
       'X-CSRF-Token': csrfToken
     },
     body: JSON.stringify({ ... })
   })
   ```

3. **Backend validates token:**
   - Middleware checks `X-CSRF-Token` header
   - Validates the token by recomputing the HMAC (see the token format above)

### When CSRF Headers Are Required

CSRF is attached **per route** in `internal/api/router.go`, not globally by
method, so the router is the only authority on which endpoints require it. As a
rule it guards state-changing requests made with an existing session, which is
why `POST /auth/register` and `POST /auth/login` do not carry it: neither has a
session to ride on yet. Check the route table in `router.go` before assuming a
new endpoint is covered — adding a POST does not add CSRF protection to it.

Safe operations do NOT require CSRF tokens:
- `GET` requests
- `HEAD` requests
- `OPTIONS` requests

---

## Environment Configuration

### Required Environment Variables

Set these in `.env` or your deployment platform:

```bash
# Secrets (MUST change in production!)
JWT_ACCESS_SECRET="your-access-token-secret-key-change-in-prod"
JWT_REFRESH_SECRET="your-refresh-token-secret-key-change-in-prod"
CSRF_SECRET="your-csrf-signing-key-change-in-prod"
```

Outside `DEV_MODE` all three are required, each at least 32 characters, and the
two JWT secrets must differ — `Config.Validate()` refuses to start otherwise and
reports every problem at once.

### Recommended Environment Variables

```bash
# Database
DATABASE_DSN="dev.db"
DATABASE_TYPE="sqlite"

# Server
SERVER_PORT=8080
SERVER_HOST=
```

### Development vs Production

**Development (.env):**
```bash
JWT_ACCESS_SECRET="dev-access-secret"
JWT_REFRESH_SECRET="dev-refresh-secret"
```

**Production:**
```bash
# Use strong, randomly generated secrets
# Example: openssl rand -base64 32

JWT_ACCESS_SECRET="$(openssl rand -base64 32)"
JWT_REFRESH_SECRET="$(openssl rand -base64 32)"
CSRF_SECRET="$(openssl rand -base64 32)"
```

For production with HTTPS, set `DEV_MODE=false` (or omit it). The `Secure` cookie flag is automatically derived from `DEV_MODE`:
- `DEV_MODE=true` → `Secure: false` (allows HTTP in development)
- `DEV_MODE=false` (default) → `Secure: true` (HTTPS only in production)

---

## Frontend Integration

### Basic Setup

Frontend uses standard `fetch` API with `credentials: 'include'` to send cookies automatically.

### Registration

```typescript
async function register(email: string, password: string, name: string) {
  const response = await fetch('/api/v1/auth/register', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, name })
  });

  const body = await response.json();

  if (!response.ok || !body.success) {
    throw new Error(body.message);
  }

  return body.data;  // { user: { id, email, name } }
}
```

### Login

```typescript
async function login(email: string, password: string) {
  const response = await fetch('/api/v1/auth/login', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password })
  });

  const body = await response.json();

  if (!response.ok || !body.success) {
    throw new Error(body.message);
  }

  return body.data;  // { user: { id, email, name } }
}
```

### Get Current User

```typescript
async function getCurrentUser() {
  const response = await fetch('/api/v1/auth/me', {
    credentials: 'include'
  });

  if (!response.ok) {
    if (response.status === 401) {
      return null;  // Not authenticated
    }
    throw new Error('Failed to fetch user');
  }

  const body = await response.json();
  return body.data;  // { id, email, name }
}
```

### Refresh Token

```typescript
async function refreshToken() {
  const response = await fetch('/api/v1/auth/refresh', {
    method: 'POST',
    credentials: 'include'
  });

  if (!response.ok) {
    // Refresh failed, user needs to login again
    return null;
  }

  // New access_token is set via cookie automatically
}
```

### Logout

```typescript
async function logout() {
  await fetch('/api/v1/auth/logout', {
    method: 'POST',
    credentials: 'include'
  });

  // Redirect to login page
  window.location.href = '/login';
}
```

### CSRF Protection for State-Changing Requests

```typescript
async function makeStateChangingRequest(method: 'POST' | 'PUT' | 'DELETE', url: string, data?: any) {
  // Get CSRF token
  const csrfResponse = await fetch('/api/v1/csrf', { credentials: 'include' });
  const csrfBody = await csrfResponse.json();
  const csrfToken = csrfBody.data.token;

  const response = await fetch(url, {
    method,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrfToken
    },
    body: data ? JSON.stringify(data) : undefined
  });

  const body = await response.json();

  if (!response.ok || !body.success) {
    throw new Error(body.message);
  }

  return body.data;
}
```

---

## Development Workflow

### Starting the App

```bash
make dev      # hot reload via air, DEV_MODE=true
# or
make server   # plain go run
```

`DEV_MODE=true` substitutes throwaway secrets and drops the cookie `Secure`
flag, which is what lets the flow work over plain HTTP on localhost.

If you run a browser client on another port, add its origin to
`ALLOWED_ORIGINS` and send credentialed requests (`credentials: "include"` in
`fetch`); cookies are handled by the browser from there.

### Testing with cURL

```bash
# Register (the password must be at least 8 characters)
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"test-password","name":"Test User"}' \
  -c cookies.txt

# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"test-password"}' \
  -c cookies.txt

# Get current user (using saved cookies)
curl -X GET http://localhost:8080/api/v1/auth/me \
  -b cookies.txt

# List active sessions (a read: no CSRF token needed)
curl -X GET 'http://localhost:8080/api/v1/auth/sessions?page=1&limit=20' \
  -b cookies.txt

# A CSRF token is needed for anything that changes state
CSRF=$(curl -s http://localhost:8080/api/v1/csrf | jq -r .data.token)

# Change the password (signs every other device out, keeps this one)
curl -X PATCH http://localhost:8080/api/v1/auth/password \
  -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" \
  -d '{"currentPassword":"test-password","newPassword":"test-password-2"}' \
  -b cookies.txt -c cookies.txt

# Logout
curl -X POST http://localhost:8080/api/v1/auth/logout \
  -H "X-CSRF-Token: $CSRF" \
  -b cookies.txt

# Delete the account
curl -X DELETE http://localhost:8080/api/v1/auth/me \
  -H "X-CSRF-Token: $CSRF" \
  -b cookies.txt
```

---

## Security Considerations

### Password Security

- Passwords are hashed with **bcrypt** (default cost: 10)
- Never stored in plaintext
- Validated on every login attempt

### Token Security

- Tokens are **signed with HMAC-SHA256**
- Cannot be modified without the secret
- **Access tokens expire after 15 minutes**
- **Refresh tokens expire after 7 days**

### Cookie Security

- **HttpOnly flag prevents XSS attacks** — JavaScript cannot access cookies
- **SameSite=Lax prevents CSRF attacks** — Cookies not sent cross-site
- **Secure flag (production only)** — Cookies only sent over HTTPS

### Additional Protections

- CSRF token validation for state-changing requests
- Context cancellation awareness (respects request timeouts)
- Clear error messages without leaking sensitive info

### Production Checklist

- [ ] **Change JWT secrets** — Generate new values with `openssl rand -base64 32`
- [ ] **Enable HTTPS** — Set `Secure: true` on cookies
- [ ] **Set strong secrets** — at least 32 characters; `Config.Validate` counts
      characters, not bytes of entropy, so use `openssl rand -base64 32`
- [ ] **Check the rate limits** — on by default, with a tighter budget on the
      credential endpoints; tune `AUTH_RATE_LIMIT_RPS` / `AUTH_RATE_LIMIT_BURST`
      rather than turning them off
- [ ] **Monitor failed logins** — Detect brute force attempts
- [ ] **Serve the client over HTTPS too** — cookies are `Secure` outside
      DEV_MODE, so a plain-HTTP client will not receive them
- [ ] **Configure CORS** — set `ALLOWED_ORIGINS` if the client is on another origin

---

## Detaching Frontend and Backend Later

The auth system is already designed for easy detachment:

### Step 1: Deploy Frontend Separately

No changes to auth code needed. The frontend can be deployed to any host.

### Step 2: Configure CORS

CORS is configured in `internal/di/container.go` (in `newRouter`) via `gin-contrib/cors`. Update the allowed origins:

```go
import "github.com/gin-contrib/cors"

r.Use(cors.New(cors.Config{
    AllowOrigins:     []string{"https://yourdomain.com"},
    AllowCredentials: true,
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
    AllowHeaders:     []string{"Content-Type", "Authorization", "X-CSRF-Token"},
}))
```

### Step 3: Update Frontend API Base URL

```typescript
const API_BASE_URL = process.env.REACT_APP_API_URL || 'http://localhost:8080';

fetch(`${API_BASE_URL}/api/v1/auth/login`, { ... })
```

**That's it.** No auth logic changes needed. The API works the same way.

---

## Extending the System

### Refresh Token Revocation

Refresh tokens are persisted via `RefreshTokenRepository`, but only as a
SHA-256 digest: the token itself is never written, so a leaked database dump
yields nothing that can be replayed. Rows are hard-deleted rather than soft
deleted, because a revoked credential that lingers in the table is one that can
be restored, and a soft-deleted row would also keep its slot in the unique
index on the digest.

On login and register a digest is stored with an expiry. On logout every digest
for the user is deleted.

**Refresh rotates the pair.** Each refresh deletes the presented token and
issues a new access *and* refresh token, so a stolen refresh token is usable
for one request rather than for its full seven days. The old row is removed
before the new one is written: a crash in between costs the user a re-login
instead of leaving two live tokens for one session.

**Reuse is treated as theft.** A token that verifies as a JWT but has no row
was either revoked by a logout or already rotated away. Those are
indistinguishable from the server side, and the second is a replay, so every
session for that user is revoked. The false positive is a client that fires two
refreshes with the same token and has to sign in again; the false negative
would be an attacker holding a stolen session indefinitely.

Expired rows are swept on `REFRESH_TOKEN_PURGE_INTERVAL` (default hourly, `0`
disables it). Nothing ever reads them again, so without the sweep the table
only grows.

### Rate Limiting

Already implemented, in two layers. `middleware.RateLimit` with a per-IP token
bucket runs on the whole router (`RATE_LIMIT_RPS` / `RATE_LIMIT_BURST`), and the
credential endpoints — register, login, refresh — carry a **second, much
tighter** limiter of their own (`AUTH_RATE_LIMIT_RPS` / `AUTH_RATE_LIMIT_BURST`,
default 0.2 rps with a burst of 5), built in `di.authRateLimiter` and attached
per route in `router.go`.

The split is the point: a global limit generous enough for normal browsing is
generous enough to guess passwords. `RATE_LIMIT_ENABLED=false` turns both off,
and the auth limiter then becomes a pass-through rather than a second policy
that quietly stays on.

A refused request is a `429` with the standard envelope — not a `403`, which
would tell a well-behaved client to stop retrying something that is only
temporarily refused.

### Multi-Device Sessions

Track active sessions per user:

Already implemented, without a separate entity: a `RefreshTokenEntity` row *is* a
session, so `GET /api/v1/auth/sessions` lists them and the refresh-token digest
identifies the caller's own.

```go
type RefreshTokenEntity struct {
  ID        uuid.UUID
  UserID    uuid.UUID
  TokenHash string     // SHA-256 digest; the token itself is never stored
  ExpiresAt time.Time
  CreatedAt time.Time
  UpdatedAt time.Time
}
```

What is still missing is per-session revocation (`DELETE .../sessions/:id`) and a
device label. A label would mean storing a parsed `User-Agent` against each row,
which is useful for the "is this you?" screen and worth doing deliberately rather
than by accident.

### Two-Factor Authentication

After login succeeds, challenge the user:
- `POST /api/v1/auth/challenge/2fa` — Send 2FA code
- `POST /api/v1/auth/verify/2fa` — Verify and issue tokens

### OAuth/Social Login

Add social auth providers:
- `POST /api/v1/auth/github` — Redirect to GitHub OAuth
- `POST /api/v1/auth/callback` — Handle OAuth callback

All while keeping the same HTTP-only cookie response format.

---

## Troubleshooting

### "missing access token" on protected endpoints

**Problem:** Frontend not sending cookies with requests.

**Solution:** Ensure `credentials: 'include'` in fetch calls:
```typescript
fetch('/api/v1/auth/me', {
  credentials: 'include'  // This is required
})
```

### "invalid email or password" even with correct credentials

**Problem:** Wrong hashing or comparison.

**Solutions:**
- Check bcrypt cost factor matches (default: 10)
- Verify password field is not trimmed unexpectedly
- Ensure database stores bcrypt hash correctly

### Cookies not being set in production

**Problem:** `Secure` flag set but not using HTTPS.

**Solutions:**
- Enable HTTPS in production
- During testing, set `Secure: false` (dev only)
- Use proper SSL certificates

### CSRF token errors on state-changing requests

**Problem:** Missing or invalid `X-CSRF-Token` header.

**Solutions:**
- Fetch CSRF token first: `GET /api/v1/csrf`
- Send token in header: `'X-CSRF-Token': token`
- Ensure header name matches exactly (case-sensitive)

### Token expired but app still shows logged in

**Problem:** Frontend not calling refresh endpoint.

**Solution:** Implement token refresh logic:
```typescript
if (response.status === 401) {
  // Token expired, try refresh
  await refreshToken();
  // Retry original request
}
```

---

## References

- **OWASP Authentication Cheat Sheet:** https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html
- **JWT Best Practices:** https://tools.ietf.org/html/rfc8949
- **HTTP Cookie Security:** https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies

---

## Support

For questions or issues with authentication:

1. Check the troubleshooting section above
2. Review example frontend integration code
3. Enable debug logging in token validation
4. Check JWT claims with `jwt.io` (for development only)