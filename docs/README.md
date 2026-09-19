# API documentation

`openapi.yaml` is the OpenAPI 3.1.0 description of this service's HTTP API. It
is written by hand — there is no generator, no code annotations, and nothing in
the build regenerates it — so it is only correct as long as someone updates it
alongside the routes.

## Viewing it

Nothing here needs to be installed globally; each of these downloads on demand.

```bash
# Interactive reference docs in a browser (reloads as you edit the file)
npx --yes @redocly/cli preview-docs docs/openapi.yaml

# A one-file static HTML bundle you can commit or publish
npx --yes @redocly/cli build-docs docs/openapi.yaml -o docs/api.html

# Swagger UI instead, if you prefer the try-it-out console
docker run --rm -p 8081:8080 \
  -e SWAGGER_JSON=/spec/openapi.yaml \
  -v "$PWD/docs:/spec" swaggerapi/swagger-ui
```

Offline, most editors will render it: VS Code's "OpenAPI (Swagger) Editor"
extension and JetBrains' built-in OpenAPI support both read this file directly.

## Validating it

```bash
npx --yes @redocly/cli lint docs/openapi.yaml
```

Two warnings are expected and are not defects:

- `no-server-example.com` on the `http://localhost:8080` dev server — that
  server entry is the point.
- `no-unused-components` on `MethodNotAllowed`. A `405` is the answer to a
  method that has *no* operation, so no operation can reference it; it is
  defined anyway so clients know the shape. The status really is emitted:
  `newRouter` sets `r.HandleMethodNotAllowed = true`, which gin leaves off by
  default and without which the `NoMethod` handler is dead code and a wrong
  verb answers `404`.

Do **not** use `npx @apidevtools/swagger-cli validate` on this file. That
package is abandoned and ships a broken OpenAPI 3.1 meta-schema that rejects a
`description` on a server variable — which the 3.1 specification explicitly
allows. It fails on a three-line minimal document with the same error, so the
failure says nothing about this file.

## Keeping it true

The spec has no tests behind it. These are the changes that silently invalidate
it, and what each one requires here:

| You changed | Update in `openapi.yaml` |
| --- | --- |
| A route in `internal/api/router.go` | The matching entry under `paths`. Adding, removing or renaming an operation. Paths are written **in full** (`/api/v1/auth/login`), so a new route needs its whole prefix. |
| The `/api/v1` group, or a route's prefix | Every affected key under `paths`, and the prose that quotes it. There is no base-path variable to edit in one place — see "Why the paths are written in full" below. |
| `SetupHealthRoutes` in `internal/api/router.go` | The `/api/health*` entries. These are **unversioned on purpose**; if that ever changes, the versioning note in `info.description` and the `Health` tag both have to change with it. |
| `internal/model/request/pagination.go` (`MaxLimit`, `DefaultLimit`, `DefaultPage`) | `components/parameters/Page` and `Limit` — the `default`, `minimum` and `maximum` — plus `Meta.limit`'s `maximum` and the prose in `GET /api/v1/auth/sessions` that quotes the ceiling. |
| `response.Session` in `internal/model/response/session.go` | The `Session` schema. **A token or digest field must never appear there** — that absence is the documented security property of the sessions endpoint, not an omission. |
| Whether `ChangePassword` revokes all sessions, or re-issues a pair to the caller | The `PATCH /api/v1/auth/password` description and its `200` `Set-Cookie` header. The revocation *is* the contract clients design around. |
| `gorm.DeletedAt` on `UserEntity` or `RefreshTokenEntity` | The `DELETE /api/v1/auth/me` description — soft vs hard delete, and the permanently reserved email — and the register `409`, which documents the same fact from the other side. |
| `Register`'s duplicate check, or `TranslateError` in `internal/platform/database.go` | The register `409`. It covers a live duplicate *and* a soft-deleted one; the second case only reaches `409` because `TranslateError` turns the unique-index violation into `gorm.ErrDuplicatedKey`. Turning that flag off silently makes it a `500` again. Note `internal/testutil/db.go` sets it too, or no test could observe the mapping. |
| Whether `AuthMiddleware` validates access tokens against stored state | The "sign-out is not instant" section of `PATCH /api/v1/auth/password`. It is true only because verification is signature-only with no repository lookup. |
| `middleware.AuthMiddleware` on a route | That operation's `security` — add or drop `accessTokenCookie`. |
| `middleware.CSRFMiddleware` on a route | That operation's `security` — add or drop `csrfToken`. It is attached **per route**, never globally, so never assume "all POSTs". |
| A `binding:` tag in `internal/model/request/` | The request schema's `minLength` / `maxLength` / `format` / `required`. Those tags *are* the validation contract. |
| A struct in `internal/model/response/` | The matching schema. A `json:"-"` field must **not** appear — that tag is how the auth tokens are kept out of response bodies. |
| A sentinel in `internal/apperr/sentinels.go` | The example `message` values under `components/responses`, which quote the sentinel text verbatim. |
| The status mapping in `internal/apperr/apperr.go` | Which responses each operation lists. |
| A status code or `message` string in a handler | That operation's responses. |
| Cookie names or TTL defaults (`internal/api/middleware/auth.go`, `internal/platform/config.go`) | `components/securitySchemes` and the `Set-Cookie` header descriptions. |
| `authRateLimiter` in `internal/di/container.go`, or which routes take it | The `TooManyRequestsAuth` response and the per-operation "Rate limit" notes. It applies to register, login and refresh **only**; everything else gets the global `TooManyRequests`. |

## Why the paths are written in full

Each entry under `paths` carries its whole prefix, and the `servers` entries are
bare origins with no base path. That is not an oversight left over from an
earlier draft — it is forced by the routing:

- The API surface is under `/api/v1`.
- The health probes are **not**. They stayed at `/api/health`,
  `/api/health/live` and `/api/health/ready` when everything else moved, because
  they are a contract with the orchestrator rather than with an API client.

Those two prefixes share no root beyond `/api`, so no single base path describes
both. An earlier version of this file used a `basePath` server variable; it
stopped working the moment health and the API diverged. OpenAPI can express a
per-operation `servers` override, but generators handle it poorly and it hides
the one thing a reader wants — the real URL — so full paths it is.

There are no unversioned aliases for the `/api/v1` routes: the old paths return
`404`, which is documented in the `NotFound` response.

## Things about this API that surprise people

Each is documented in the spec itself; they are listed here so a reviewer knows
to check them.

- **There is no `Authorization: Bearer` path.** Tokens move only as `HttpOnly`
  cookies the server sets, which is why the security schemes are `apiKey` /
  `in: cookie` — a client cannot read or attach them by hand.
- **CSRF is required on exactly five operations** (`POST /api/v1/auth/refresh`,
  `POST /api/v1/auth/logout`, `PATCH /api/v1/auth/password`,
  `DELETE /api/v1/auth/me`, `POST /api/v1/counter`), not on every write.
  Register and login have no session for a forged request to ride on, and the
  reads — including `GET /api/v1/auth/sessions` — have no action to forge.
- **Refresh rotates both tokens**, and replaying a refresh token that has no
  database row revokes every session for that user.
- **The CSRF token expires** (default 12h) because a stateless token cannot be
  revoked — clients should re-fetch on `403`, not cache one forever.
- **Health is not versioned.** `/api/health*` did not move to `/api/v1`, and
  there is no versioned alias.
- **`Meta` is returned by `GET /api/v1/auth/sessions`**, in the envelope's own
  `meta` field rather than inside `data`. Its `limit` is the *effective* window
  after defaults and the ceiling of 100 were applied, not what the client asked
  for.
- **`limit` over 100 is rejected, not clamped.** A silent clamp would hand back
  a short page a client could mistake for the end of the collection.
- **A session never exposes its token or the stored digest**, not even
  truncated. `current` is computed by comparing digests server-side.
- **Changing a password signs out every other device — but not instantly.**
  Their refresh tokens die at once; their *access* tokens stay valid for up to
  `JWT_ACCESS_TTL` (15m), because `AuthMiddleware` verifies by signature alone
  with no lookup. The spec says so in plain terms, because a user changing a
  password after a compromise will assume otherwise.
- **Deleting an account permanently reserves the email address**, for its
  original owner too — the soft-deleted row keeps its slot in the unique index,
  so re-registering it answers `409`.
- **Rate limiting answers `429`, never `403`**, and sends no `Retry-After` or
  `X-RateLimit-*` headers. The strict credential budget covers register, login
  and refresh only.

## Deliberately absent

The spec describes only the routes that exist today, and the behaviour they
actually have rather than the behaviour they ought to have. A spec describing
endpoints that do not exist, or answers they do not give, is worse than no spec:
it cannot be distinguished from a spec describing endpoints that are broken.
