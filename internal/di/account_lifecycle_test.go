package di_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// registered creates an account and returns the cookies it was issued.
func registered(t *testing.T, engine *gin.Engine, email, password string) (accessToken, refreshToken string) {
	t.Helper()

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": password, "name": "Lifecycle User",
	}))
	testutil.Equal(t, rec.Code, http.StatusCreated, "register status")

	cookies := testutil.Cookies(rec)
	return cookies[middleware.AccessTokenCookie].Value, cookies[middleware.RefreshTokenCookie].Value
}

// signedIn logs the same account in again, which mints a second live session.
func signedIn(t *testing.T, engine *gin.Engine, email, password string) (accessToken, refreshToken string) {
	t.Helper()

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}))
	testutil.Equal(t, rec.Code, http.StatusOK, "login status")

	cookies := testutil.Cookies(rec)
	return cookies[middleware.AccessTokenCookie].Value, cookies[middleware.RefreshTokenCookie].Value
}

// TestListSessionsEndToEnd drives the real router, service and database. It is
// the only place the session listing meets genuine rows rather than mock
// returns, so it is where "the digest never leaves the database" is worth
// asserting on an actual response body.
func TestListSessionsEndToEnd(t *testing.T) {
	container := newTestContainer(t)
	engine := container.Router
	db := container.Config.Database.Gorm

	const email, password = "sessions@example.com", "password123"
	firstAccess, firstRefresh := registered(t, engine, email, password)
	_, _ = signedIn(t, engine, email, password)

	req := testutil.WithCookie(
		testutil.WithCookie(
			testutil.JSONRequest(t, http.MethodGet, "/api/v1/auth/sessions", nil),
			middleware.AccessTokenCookie, firstAccess,
		),
		middleware.RefreshTokenCookie, firstRefresh,
	)
	rec := testutil.Do(engine, req)

	testutil.Equal(t, rec.Code, http.StatusOK, "status")

	sessions := testutil.DataAs[[]response.Session](t, rec)
	testutil.Equal(t, len(sessions), 2, "both devices are listed")

	// Exactly one row is the caller's own, and it is the one whose refresh
	// token was presented. That is what a "sign out my other devices" screen
	// needs in order to leave this device alone.
	current := 0
	for _, session := range sessions {
		if session.Current {
			current++
		}
	}
	testutil.Equal(t, current, 1, "exactly one session is marked current")

	envelope := testutil.Envelope(t, rec)
	if envelope.Meta == nil {
		t.Fatalf("expected pagination metadata, got none: %s", rec.Body.String())
	}
	testutil.Equal(t, envelope.Meta.Total, int64(2), "meta total")

	// The security assertion, made against the REAL digests rather than against
	// field names. Searching the body for "hash" and "token" only catches a
	// badly named field; it would sail past someone adding
	// `Fingerprint string` carrying row.TokenHash. Reading the actual stored
	// values and asserting none of them appears is the assertion that holds
	// whatever the field ends up being called.
	assertNoStoredDigestInBody(t, db, rec.Body.String())

	// Field names are still worth checking, as a second, weaker net.
	body := strings.ToLower(rec.Body.String())
	testutil.True(t, !strings.Contains(body, "hash"), "no digest field in the body")
	testutil.True(t, !strings.Contains(body, "token"), "no token field in the body")
}

// TestChangePasswordEndToEnd is the proof that the revocation is real: the
// other device's refresh token has to stop working, and the caller's must not.
func TestChangePasswordEndToEnd(t *testing.T) {
	engine := newTestContainer(t).Router
	csrf := csrfToken(t, engine)

	const email, oldPassword, newPassword = "rotate@example.com", "password123", "a-much-better-password"
	_, otherRefresh := registered(t, engine, email, oldPassword)
	callerAccess, _ := signedIn(t, engine, email, oldPassword)

	change := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPatch, "/api/v1/auth/password", map[string]string{
			"currentPassword": oldPassword,
			"newPassword":     newPassword,
		}),
		middleware.AccessTokenCookie, callerAccess,
	)
	change.Header.Set("X-CSRF-Token", csrf)
	rec := testutil.Do(engine, change)

	testutil.Equal(t, rec.Code, http.StatusOK, "change password status")

	// The caller is handed a replacement pair rather than being signed out.
	cookies := testutil.Cookies(rec)
	newAccess := cookies[middleware.AccessTokenCookie].Value
	newRefresh := cookies[middleware.RefreshTokenCookie].Value
	testutil.True(t, newAccess != "", "a replacement access token is issued")
	testutil.True(t, newRefresh != "", "a replacement refresh token is issued")

	meRec := testutil.Do(engine, testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodGet, "/api/v1/auth/me", nil),
		middleware.AccessTokenCookie, newAccess,
	))
	testutil.Equal(t, meRec.Code, http.StatusOK, "the caller is still signed in")

	// The other device is gone. This is the entire reason a user changes a
	// password: a session minted before the change must not outlive it.
	stale := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/refresh", nil),
		middleware.RefreshTokenCookie, otherRefresh,
	)
	stale.Header.Set("X-CSRF-Token", csrf)
	testutil.Equal(t, testutil.Do(engine, stale).Code, http.StatusUnauthorized,
		"a session from before the password change is revoked")

	// The old password no longer works, the new one does.
	oldLogin := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": oldPassword,
	}))
	testutil.Equal(t, oldLogin.Code, http.StatusUnauthorized, "the old password is refused")

	newLogin := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": newPassword,
	}))
	testutil.Equal(t, newLogin.Code, http.StatusOK, "the new password works")
}

func TestChangePasswordRejectsAWrongCurrentPasswordEndToEnd(t *testing.T) {
	engine := newTestContainer(t).Router
	csrf := csrfToken(t, engine)

	const email, password = "wrongpw@example.com", "password123"
	access, refresh := registered(t, engine, email, password)

	req := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPatch, "/api/v1/auth/password", map[string]string{
			"currentPassword": "not-the-current-password",
			"newPassword":     "a-much-better-password",
		}),
		middleware.AccessTokenCookie, access,
	)
	req.Header.Set("X-CSRF-Token", csrf)

	testutil.Equal(t, testutil.Do(engine, req).Code, http.StatusUnauthorized, "status")

	// Nothing was revoked: a failed attempt must not sign anyone out, or a
	// wrong guess becomes a way to log another user's devices off.
	survivor := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/refresh", nil),
		middleware.RefreshTokenCookie, refresh,
	)
	survivor.Header.Set("X-CSRF-Token", csrf)
	testutil.Equal(t, testutil.Do(engine, survivor).Code, http.StatusOK,
		"the existing session survives a failed password change")
}

// TestDeleteAccountEndToEnd covers the asymmetry between the two deletes: the
// user row is soft-deleted and the refresh tokens are really gone, and both
// halves have to make the account unusable.
func TestDeleteAccountEndToEnd(t *testing.T) {
	engine := newTestContainer(t).Router
	csrf := csrfToken(t, engine)

	const email, password = "goodbye@example.com", "password123"
	access, refresh := registered(t, engine, email, password)

	del := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodDelete, "/api/v1/auth/me", nil),
		middleware.AccessTokenCookie, access,
	)
	del.Header.Set("X-CSRF-Token", csrf)
	rec := testutil.Do(engine, del)

	testutil.Equal(t, rec.Code, http.StatusOK, "delete status")

	cookies := testutil.Cookies(rec)
	testutil.Equal(t, cookies[middleware.AccessTokenCookie].MaxAge, -1, "access cookie is cleared")
	testutil.Equal(t, cookies[middleware.RefreshTokenCookie].MaxAge, -1, "refresh cookie is cleared")

	// 1. The account cannot sign in: the soft-deleted row is invisible to
	// FindByEmail, so this is indistinguishable from an unknown address.
	login := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}))
	testutil.Equal(t, login.Code, http.StatusUnauthorized, "a deleted account cannot log in")

	// 2. And the refresh token it still holds is refused, even though the JWT
	// itself has not expired: the row backing it was really deleted.
	refreshReq := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/refresh", nil),
		middleware.RefreshTokenCookie, refresh,
	)
	refreshReq.Header.Set("X-CSRF-Token", csrf)
	testutil.Equal(t, testutil.Do(engine, refreshReq).Code, http.StatusUnauthorized,
		"a deleted account cannot refresh")

	// 3. The access token it still holds outlives the account until it expires,
	// because validating one is a signature check and touches no database. The
	// route it reaches then reports the user as missing rather than serving
	// them, which is what bounds the exposure to the access token TTL.
	me := testutil.Do(engine, testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodGet, "/api/v1/auth/me", nil),
		middleware.AccessTokenCookie, access,
	))
	testutil.Equal(t, me.Code, http.StatusNotFound, "a deleted account is not served")

	// 4. The address stays reserved, permanently. UserEntity carries
	// gorm.DeletedAt, so the deleted row keeps its slot in the unique index on
	// email and nobody — the original owner included — can ever register that
	// address again. That is a real limitation of the soft delete, recorded
	// here rather than papered over.
	//
	// The status is 409 rather than 500 because gorm.Config.TranslateError is
	// on, which turns the driver's constraint violation into
	// gorm.ErrDuplicatedKey; Register recognises that and returns the same
	// conflict a live duplicate would. Without the translation this path
	// reported a server fault for what is plainly a client-visible conflict.
	reRegister := testutil.Do(engine, testutil.JSONRequest(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": email, "password": password, "name": "Back Again",
	}))
	testutil.Equal(t, reRegister.Code, http.StatusConflict, "re-registering a deleted address")
}

// TestNewRoutesAreWiredWithTheirMiddleware checks the router rather than the
// handlers: that the auth middleware and the per-route CSRF middleware are
// actually attached to the endpoints added for the account lifecycle.
func TestNewRoutesAreWiredWithTheirMiddleware(t *testing.T) {
	engine := newTestContainer(t).Router

	const email, password = "wiring@example.com", "password123"
	access, _ := registered(t, engine, email, password)

	t.Run("anonymous requests are refused", func(t *testing.T) {
		routes := []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/v1/auth/sessions"},
			{http.MethodPatch, "/api/v1/auth/password"},
			{http.MethodDelete, "/api/v1/auth/me"},
		}

		for _, route := range routes {
			t.Run(route.method+" "+route.path, func(t *testing.T) {
				rec := testutil.Do(engine, testutil.JSONRequest(t, route.method, route.path, nil))
				testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
			})
		}
	})

	t.Run("state-changing routes demand a CSRF token", func(t *testing.T) {
		routes := []struct {
			method string
			path   string
		}{
			{http.MethodPatch, "/api/v1/auth/password"},
			{http.MethodDelete, "/api/v1/auth/me"},
		}

		for _, route := range routes {
			t.Run(route.method+" "+route.path, func(t *testing.T) {
				rec := testutil.Do(engine, testutil.WithCookie(
					testutil.JSONRequest(t, route.method, route.path, nil),
					middleware.AccessTokenCookie, access,
				))
				testutil.Equal(t, rec.Code, http.StatusForbidden, "status")
			})
		}
	})

	t.Run("listing sessions is a read and needs no CSRF token", func(t *testing.T) {
		// Requiring one on a GET would make the endpoint a two-request dance
		// for no gain: there is no action for a third-party page to perform.
		rec := testutil.Do(engine, testutil.WithCookie(
			testutil.JSONRequest(t, http.MethodGet, "/api/v1/auth/sessions", nil),
			middleware.AccessTokenCookie, access,
		))
		testutil.Equal(t, rec.Code, http.StatusOK, "status")
	})
}

// TestTheUnversionedAPISurfaceIsGone pins the decision not to keep /api/...
// aliases alongside /api/v1/.... Two live surfaces is a maintenance tax paid
// forever to avoid a one-line client change, and this service has no external
// consumers to spare.
func TestTheUnversionedAPISurfaceIsGone(t *testing.T) {
	engine := newTestContainer(t).Router

	for _, path := range []string{"/api/message", "/api/csrf", "/api/auth/me", "/api/counter"} {
		t.Run(path, func(t *testing.T) {
			rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, path, nil))
			testutil.Equal(t, rec.Code, http.StatusNotFound, "status")
		})
	}
}

// TestHealthRoutesStayUnversioned is the other half of that decision. Health
// endpoints are a contract with the orchestrator, not with an API client: they
// are wired into liveness probes and uptime monitors outside this repository,
// so versioning them would mean editing every one of those on a version bump.
func TestHealthRoutesStayUnversioned(t *testing.T) {
	engine := newTestContainer(t).Router

	for _, path := range []string{"/api/health", "/api/health/live", "/api/health/ready"} {
		t.Run(path, func(t *testing.T) {
			testutil.Equal(t, testutil.Do(engine,
				testutil.JSONRequest(t, http.MethodGet, path, nil)).Code, http.StatusOK, "status")
		})
	}

	// And there is deliberately no versioned copy of them.
	testutil.Equal(t, testutil.Do(engine,
		testutil.JSONRequest(t, http.MethodGet, "/api/v1/health", nil)).Code,
		http.StatusNotFound, "no versioned health endpoint")
}

// assertNoStoredDigestInBody reads every refresh-token digest the database
// actually holds and fails if any of them, or any prefix long enough to matter,
// appears in the response body.
//
// This is the shape a leak test has to take. A test that searches for a
// constant it invented itself, or for a field *name*, cannot fail when the leak
// is real — which is exactly how the first version of this assertion passed
// while proving nothing.
func assertNoStoredDigestInBody(t *testing.T, db *gorm.DB, body string) {
	t.Helper()

	var digests []string
	if err := db.Model(&entity.RefreshTokenEntity{}).Pluck("token_hash", &digests).Error; err != nil {
		t.Fatalf("reading stored digests: %v", err)
	}
	if len(digests) == 0 {
		t.Fatal("no digests stored, so this assertion would prove nothing")
	}

	for _, digest := range digests {
		if strings.Contains(body, digest) {
			t.Errorf("a stored refresh-token digest appears in the response body")
		}
		// Not even a prefix: eight hex characters is eight an attacker no
		// longer has to guess.
		if strings.Contains(body, digest[:8]) {
			t.Errorf("a stored digest's prefix %q appears in the response body", digest[:8])
		}
	}
}
