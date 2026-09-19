package di_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/di"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/platform"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// newTestContainer wires the whole application against an in-memory database.
//
// Nothing is mocked here: these tests drive real HTTP requests through the real
// middleware chain, router, handlers, services and repositories, which is the
// only way to catch wiring mistakes that unit tests cannot see.
func newTestContainer(t *testing.T) *di.Container {
	t.Helper()

	gin.SetMode(gin.TestMode)

	t.Setenv("DEV_MODE", "true")
	cfg := platform.NewConfig()
	cfg.Database.Gorm = testutil.NewDB(t)
	// Rate limiting would otherwise make the outcome depend on how many
	// requests a test happens to make.
	cfg.Security.RateLimitEnabled = false

	container, err := di.NewContainer(cfg, nil)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = container.Close() })

	return container
}

// csrfToken fetches a token the way a browser client does.
func csrfToken(t *testing.T, engine *gin.Engine) string {
	t.Helper()

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/api/csrf", nil))
	testutil.Equal(t, rec.Code, http.StatusOK, "csrf status")

	return testutil.DataAs[response.CSRFTokenResponse](t, rec).Token
}

// TestFullAuthenticationFlow walks the journey a real client makes.
func TestFullAuthenticationFlow(t *testing.T) {
	container := newTestContainer(t)
	engine := container.Router

	csrf := csrfToken(t, engine)

	// 1. Register.
	registerRec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email":    "e2e@example.com",
		"password": "password123",
		"name":     "End To End",
	}))
	testutil.Equal(t, registerRec.Code, http.StatusCreated, "register status")

	cookies := testutil.Cookies(registerRec)
	accessToken := cookies[middleware.AccessTokenCookie].Value
	testutil.True(t, accessToken != "", "register issues an access token")
	testutil.True(t, cookies[middleware.RefreshTokenCookie].Value != "", "register issues a refresh token")

	// 2. The access cookie authenticates a protected route.
	meRec := testutil.Do(engine, testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodGet, "/api/auth/me", nil),
		middleware.AccessTokenCookie, accessToken,
	))
	testutil.Equal(t, meRec.Code, http.StatusOK, "me status")
	testutil.Equal(t, testutil.DataAs[response.GetUser](t, meRec).Email, "e2e@example.com", "email")

	// 3. Registering the same address again is a conflict, not a bad request.
	duplicateRec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email":    "e2e@example.com",
		"password": "password123",
		"name":     "Impostor",
	}))
	testutil.Equal(t, duplicateRec.Code, http.StatusConflict, "duplicate register status")

	// 4. Login issues a fresh pair.
	loginRec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "e2e@example.com",
		"password": "password123",
	}))
	testutil.Equal(t, loginRec.Code, http.StatusOK, "login status")
	loginRefresh := testutil.Cookies(loginRec)[middleware.RefreshTokenCookie].Value

	// 5. Refresh exchanges the refresh cookie for a new access token.
	refreshReq := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/auth/refresh", nil),
		middleware.RefreshTokenCookie, loginRefresh,
	)
	refreshReq.Header.Set("X-CSRF-Token", csrf)
	testutil.Equal(t, testutil.Do(engine, refreshReq).Code, http.StatusOK, "refresh status")

	// 6. Logout revokes the stored refresh tokens.
	logoutReq := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/auth/logout", nil),
		middleware.AccessTokenCookie, accessToken,
	)
	logoutReq.Header.Set("X-CSRF-Token", csrf)
	testutil.Equal(t, testutil.Do(engine, logoutReq).Code, http.StatusOK, "logout status")

	// 7. The revoked refresh token is no longer accepted, even though the JWT
	// itself is still within its validity window.
	afterLogout := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/auth/refresh", nil),
		middleware.RefreshTokenCookie, loginRefresh,
	)
	afterLogout.Header.Set("X-CSRF-Token", csrf)
	testutil.Equal(t, testutil.Do(engine, afterLogout).Code, http.StatusUnauthorized, "refresh after logout")
}

func TestProtectedRoutesRejectAnonymousRequests(t *testing.T) {
	engine := newTestContainer(t).Router

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/auth/me"},
		{http.MethodGet, "/api/counter"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			rec := testutil.Do(engine, testutil.JSONRequest(t, route.method, route.path, nil))
			testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
		})
	}
}

// TestStateChangingRoutesRequireCSRF checks that the per-route CSRF middleware
// is actually attached, which the handler tests cannot see.
func TestStateChangingRoutesRequireCSRF(t *testing.T) {
	engine := newTestContainer(t).Router

	registerRec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "csrf@example.com", "password": "password123", "name": "CSRF User",
	}))
	testutil.Equal(t, registerRec.Code, http.StatusCreated, "register status")
	accessToken := testutil.Cookies(registerRec)[middleware.AccessTokenCookie].Value

	// Authenticated, but with no CSRF header.
	rec := testutil.Do(engine, testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/counter", nil),
		middleware.AccessTokenCookie, accessToken,
	))
	testutil.Equal(t, rec.Code, http.StatusForbidden, "status without a CSRF token")

	// The same request, with the header.
	withToken := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/counter", nil),
		middleware.AccessTokenCookie, accessToken,
	)
	withToken.Header.Set("X-CSRF-Token", csrfToken(t, engine))
	okRec := testutil.Do(engine, withToken)

	testutil.Equal(t, okRec.Code, http.StatusOK, "status with a CSRF token")
	testutil.Equal(t, testutil.DataAs[response.GetCounter](t, okRec).Value, 1, "counter value")
}

func TestHealthEndpoints(t *testing.T) {
	engine := newTestContainer(t).Router

	for _, path := range []string{"/api/health", "/api/health/live", "/api/health/ready"} {
		t.Run(path, func(t *testing.T) {
			rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, path, nil))
			testutil.Equal(t, rec.Code, http.StatusOK, "status")
			testutil.Equal(t, testutil.Envelope(t, rec).Success, true, "success flag")
		})
	}
}

// TestUnknownRoutesReturnTheEnvelope: clients parse the envelope on every
// response, so a bare 404 with an empty body breaks them.
func TestUnknownRoutesReturnTheEnvelope(t *testing.T) {
	engine := newTestContainer(t).Router

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/api/does-not-exist", nil))

	testutil.Equal(t, rec.Code, http.StatusNotFound, "status")
	testutil.Equal(t, testutil.Envelope(t, rec).Message, "Route not found", "message")
}

func TestEveryResponseCarriesSecurityHeadersAndARequestID(t *testing.T) {
	engine := newTestContainer(t).Router

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/api/message", nil))

	testutil.Equal(t, rec.Header().Get("X-Content-Type-Options"), "nosniff", "nosniff header")
	testutil.True(t, rec.Header().Get(middleware.RequestIDHeader) != "", "request id header")
}

func TestContainerRejectsAnInvalidConfiguration(t *testing.T) {
	// Production mode with no secrets must fail at startup rather than quietly
	// serving traffic signed with a development key.
	t.Setenv("DEV_MODE", "false")
	t.Setenv("JWT_ACCESS_SECRET", "")
	t.Setenv("JWT_REFRESH_SECRET", "")
	t.Setenv("CSRF_SECRET", "")

	cfg := platform.NewConfig()
	cfg.Database.Gorm = testutil.NewDB(t)

	_, err := di.NewContainer(cfg, nil)
	testutil.Error(t, err, "container with no secrets in production mode")
}

func TestContainerCloseLeavesAnInjectedDatabaseAlone(t *testing.T) {
	// The harness owns the injected database and closes it in t.Cleanup, so the
	// container must not close it out from under the test.
	container := newTestContainer(t)

	testutil.NoError(t, container.Close())

	rec := testutil.Do(container.Router, testutil.JSONRequest(t, http.MethodGet, "/api/message", nil))
	testutil.Equal(t, rec.Code, http.StatusOK, "the database still works after Close")
}

// TestErrorResponsesAreTraceableEndToEnd checks the wiring rather than the
// handler: the RequestID middleware, the router fallbacks and handler.Fail all
// have to agree on the same identifier for a report to be actionable.
func TestErrorResponsesAreTraceableEndToEnd(t *testing.T) {
	engine := newTestContainer(t).Router

	cases := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "unknown route", method: http.MethodGet, path: "/api/nope", status: http.StatusNotFound},
		{name: "unauthenticated", method: http.MethodGet, path: "/api/auth/me", status: http.StatusUnauthorized},
		{name: "missing csrf token", method: http.MethodPost, path: "/api/auth/refresh", status: http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := testutil.Do(engine, testutil.JSONRequest(t, tc.method, tc.path, nil))

			testutil.Equal(t, rec.Code, tc.status, "status")

			envelope := testutil.Envelope(t, rec)
			header := rec.Header().Get(middleware.RequestIDHeader)

			testutil.True(t, envelope.RequestID != "", "the body carries a requestId")
			testutil.Equal(t, envelope.RequestID, header, "body and header agree")
		})
	}
}

// TestValidationErrorsAreCamelCaseAndCorrelated pins the response contract the
// front end codes against.
func TestValidationErrorsAreCamelCaseAndCorrelated(t *testing.T) {
	engine := newTestContainer(t).Router

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "not-an-email",
	}))

	testutil.Equal(t, rec.Code, http.StatusBadRequest, "status")

	body := rec.Body.String()
	testutil.True(t, strings.Contains(body, `"requestId"`), "requestId is camelCase")
	testutil.True(t, !strings.Contains(body, `"request_id"`), "no snake_case key")

	envelope := testutil.Envelope(t, rec)
	for _, field := range []string{"email", "password", "name"} {
		if _, ok := envelope.Errors[field]; !ok {
			t.Errorf("expected a validation error for %q, got %v", field, envelope.Errors)
		}
	}
}

// TestRefreshRotatesAndDetectsReuse is the end-to-end proof of the refresh
// token design. Nothing is mocked: a replayed token has to take the whole
// session family down, through the real router, service and database.
func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	engine := newTestContainer(t).Router
	csrf := csrfToken(t, engine)

	registerRec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "rotation@example.com", "password": "password123", "name": "Rotation",
	}))
	testutil.Equal(t, registerRec.Code, http.StatusCreated, "register status")
	original := testutil.Cookies(registerRec)[middleware.RefreshTokenCookie].Value

	// 1. The first refresh succeeds and hands back a different token.
	firstRec := testutil.Do(engine, refreshRequest(t, original, csrf))
	testutil.Equal(t, firstRec.Code, http.StatusOK, "first refresh status")

	rotated := testutil.Cookies(firstRec)[middleware.RefreshTokenCookie].Value
	testutil.True(t, rotated != "", "a new refresh token is issued")
	testutil.True(t, rotated != original, "the refresh token actually rotated")

	// 2. Replaying the original token is refused: it was already rotated away.
	replayRec := testutil.Do(engine, refreshRequest(t, original, csrf))
	testutil.Equal(t, replayRec.Code, http.StatusUnauthorized, "replay status")

	// 3. And the replay revoked the whole family, so the token that was
	// legitimately issued in step 1 is dead too. That is the point: once a
	// token is known to have been copied, the session cannot be trusted.
	afterReuseRec := testutil.Do(engine, refreshRequest(t, rotated, csrf))
	testutil.Equal(t, afterReuseRec.Code, http.StatusUnauthorized,
		"the rotated token is revoked once reuse is detected")
}

// TestLoginDoesNotRevealWhichEmailsExist checks the message, which is the part
// a test can assert reliably; the timing equalisation it pairs with is covered
// by the dummy hash comparison in the service.
func TestLoginDoesNotRevealWhichEmailsExist(t *testing.T) {
	engine := newTestContainer(t).Router

	testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "known@example.com", "password": "password123", "name": "Known",
	}))

	unknown := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email": "nobody@example.com", "password": "password123",
	}))
	wrongPassword := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email": "known@example.com", "password": "the-wrong-password",
	}))

	testutil.Equal(t, unknown.Code, wrongPassword.Code, "status for unknown email vs wrong password")
	testutil.Equal(t, testutil.Envelope(t, unknown).Message,
		testutil.Envelope(t, wrongPassword).Message, "message for unknown email vs wrong password")
}

func refreshRequest(t *testing.T, refreshToken, csrf string) *http.Request {
	t.Helper()

	req := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/auth/refresh", nil),
		middleware.RefreshTokenCookie, refreshToken,
	)
	req.Header.Set("X-CSRF-Token", csrf)
	return req
}

// TestAuthEndpointsHaveTheirOwnRateLimit checks the route wiring, which the
// middleware tests cannot see: that the tighter limiter is actually attached to
// the credential endpoints and not to everything else.
func TestAuthEndpointsHaveTheirOwnRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("DEV_MODE", "true")
	cfg := platform.NewConfig()
	cfg.Database.Gorm = testutil.NewDB(t)
	cfg.Security.RateLimitEnabled = true
	// Generous globally, almost nothing for credentials.
	cfg.Security.RateLimitRPS = 1000
	cfg.Security.RateLimitBurst = 1000
	cfg.Security.AuthRateLimitRPS = 0.0001
	cfg.Security.AuthRateLimitBurst = 2

	container, err := di.NewContainer(cfg, nil)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = container.Close() })

	credentials := map[string]string{"email": "throttled@example.com", "password": "password123"}

	// The burst is spent on the first two attempts, whatever they answer.
	for i := 0; i < 2; i++ {
		rec := testutil.Do(container.Router,
			testutil.JSONRequest(t, http.MethodPost, "/api/auth/login", credentials))
		testutil.True(t, rec.Code != http.StatusTooManyRequests, "an attempt within the burst")
	}

	throttled := testutil.Do(container.Router,
		testutil.JSONRequest(t, http.MethodPost, "/api/auth/login", credentials))
	testutil.Equal(t, throttled.Code, http.StatusTooManyRequests, "the attempt past the burst")

	// Ordinary traffic is untouched: the two limiters keep separate budgets.
	public := testutil.Do(container.Router, testutil.JSONRequest(t, http.MethodGet, "/api/message", nil))
	testutil.Equal(t, public.Code, http.StatusOK, "a public endpoint after the auth limit is hit")
}
