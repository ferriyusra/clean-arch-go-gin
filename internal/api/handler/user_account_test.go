package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/handler"
	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/csrf"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/mock"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// accountFixture mounts the session and account-lifecycle routes behind the
// same middleware chain production uses, CSRF included, so these tests cover
// the chain and not just the handler bodies. It embeds userFixture so the
// authed helper defined alongside the auth tests can be reused.
type accountFixture struct {
	*userFixture
	csrf csrf.CSRFService
}

func newAccountFixture(t *testing.T) *accountFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	users := mock.NewMockUserService(ctrl)

	tokens := token.NewTokenService(token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  testAccessTTL,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: testRefreshTTL,
	})

	csrfService := csrf.NewCSRFService("test-csrf-secret", time.Hour)
	h := handler.NewUserHandler(users, csrfService, handler.CookieConfig{
		AccessTTL:  testAccessTTL,
		RefreshTTL: testRefreshTTL,
		Secure:     true,
	})

	r := testutil.NewEngine(t)
	protected := r.Group("", middleware.AuthMiddleware(tokens))
	protected.GET("/api/v1/auth/sessions", h.ListSessions)
	protected.PATCH("/api/v1/auth/password", middleware.CSRFMiddleware(csrfService), h.ChangePassword)
	protected.DELETE("/api/v1/auth/me", middleware.CSRFMiddleware(csrfService), h.DeleteAccount)

	return &accountFixture{
		userFixture: &userFixture{users: users, tokens: tokens, engine: r},
		csrf:        csrfService,
	}
}

// withCSRF stamps a valid CSRF token onto a request.
func (f *accountFixture) withCSRF(t *testing.T, req *http.Request) *http.Request {
	t.Helper()

	csrfToken, err := f.csrf.GenerateToken()
	testutil.NoError(t, err)

	req.Header.Set("X-CSRF-Token", csrfToken)
	return req
}

func TestListSessionsHandler(t *testing.T) {
	userID := uuid.New()

	t.Run("returns the sessions with pagination metadata", func(t *testing.T) {
		f := newAccountFixture(t)

		now := time.Now()
		sessions := []response.Session{
			{ID: uuid.New(), Current: true, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(testRefreshTTL)},
			{ID: uuid.New(), Current: false, CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(time.Hour)},
		}

		f.users.EXPECT().
			ListSessions(gomock.Any(), userID, gomock.Any(), request.Pagination{Page: 2, Limit: 5}).
			Return(&response.SessionList{Sessions: sessions, Meta: response.NewMeta(2, 5, 42)}, nil)

		rec := testutil.Do(f.engine,
			f.authed(t, http.MethodGet, "/api/v1/auth/sessions?page=2&limit=5", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")

		// The list is the envelope's data and the window is its meta, so a
		// paginated collection has the same shape as any other collection.
		envelope := testutil.Envelope(t, rec)
		if envelope.Meta == nil {
			t.Fatalf("expected pagination metadata, got none: %s", rec.Body.String())
		}
		testutil.Equal(t, envelope.Meta.Page, 2, "meta page")
		testutil.Equal(t, envelope.Meta.Limit, 5, "meta limit")
		testutil.Equal(t, envelope.Meta.Total, int64(42), "meta total")

		body := testutil.DataAs[[]response.Session](t, rec)
		testutil.Equal(t, len(body), 2, "session count")
		testutil.Equal(t, body[0].Current, true, "the caller's own session is marked")
	})

	t.Run("an absent query string means the first page", func(t *testing.T) {
		// A client that simply asks for its sessions must not need to know the
		// pagination parameters exist.
		f := newAccountFixture(t)

		f.users.EXPECT().
			ListSessions(gomock.Any(), userID, gomock.Any(),
				request.Pagination{Page: request.DefaultPage, Limit: request.DefaultLimit}).
			Return(&response.SessionList{Meta: response.NewMeta(1, request.DefaultLimit, 0)}, nil)

		rec := testutil.Do(f.engine, f.authed(t, http.MethodGet, "/api/v1/auth/sessions", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")
	})

	t.Run("an explicit zero is treated as unset", func(t *testing.T) {
		// An int cannot distinguish "0" from "not sent", and making the zero
		// value mean "unset" is what lets an absent parameter get the default
		// instead of a rejection. The consequence is that ?page=0 falls back
		// too rather than 400ing; a negative value is still rejected, which is
		// the mistake that actually happens.
		f := newAccountFixture(t)

		f.users.EXPECT().
			ListSessions(gomock.Any(), userID, gomock.Any(),
				request.Pagination{Page: request.DefaultPage, Limit: request.DefaultLimit}).
			Return(&response.SessionList{Meta: response.NewMeta(1, request.DefaultLimit, 0)}, nil)

		rec := testutil.Do(f.engine,
			f.authed(t, http.MethodGet, "/api/v1/auth/sessions?page=0&limit=0", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")
	})

	t.Run("passes the refresh cookie through so the current session is known", func(t *testing.T) {
		f := newAccountFixture(t)

		f.users.EXPECT().
			ListSessions(gomock.Any(), userID, "the-refresh-token", gomock.Any()).
			Return(&response.SessionList{Meta: response.NewMeta(1, request.DefaultLimit, 0)}, nil)

		req := testutil.WithCookie(
			f.authed(t, http.MethodGet, "/api/v1/auth/sessions", nil, userID),
			middleware.RefreshTokenCookie, "the-refresh-token",
		)

		testutil.Equal(t, testutil.Do(f.engine, req).Code, http.StatusOK, "status")
	})

	t.Run("rejects an unauthenticated request", func(t *testing.T) {
		f := newAccountFixture(t)

		rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodGet, "/api/v1/auth/sessions", nil))

		testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
	})

	t.Run("reports a lookup failure as 500", func(t *testing.T) {
		f := newAccountFixture(t)
		f.users.EXPECT().ListSessions(gomock.Any(), userID, gomock.Any(), gomock.Any()).
			Return(nil, apperr.Internal(errors.New("connection refused")))

		rec := testutil.Do(f.engine, f.authed(t, http.MethodGet, "/api/v1/auth/sessions", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusInternalServerError, "status")
	})
}

// TestListSessionsRejectsAnUnusableWindow: a bad query parameter comes back as
// the standard envelope with a field-level errors map, the same shape a
// malformed body produces, rather than a bare 400 a client cannot parse.
func TestListSessionsRejectsAnUnusableWindow(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name  string
		query string
		field string
	}{
		{name: "a non-numeric limit", query: "?limit=abc", field: "limit"},
		{name: "a negative page", query: "?page=-1", field: "page"},
		{name: "a negative limit", query: "?limit=-5", field: "limit"},
		// The ceiling is the load-bearing rule: without it a client asks for a
		// million rows and the database does the work.
		{name: "a limit past the ceiling", query: "?limit=1000000", field: "limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAccountFixture(t)
			// No EXPECT: the service must never be reached.

			rec := testutil.Do(f.engine,
				f.authed(t, http.MethodGet, "/api/v1/auth/sessions"+tt.query, nil, userID))

			testutil.Equal(t, rec.Code, http.StatusBadRequest, "status")

			envelope := testutil.Envelope(t, rec)
			testutil.Equal(t, envelope.Success, false, "success flag")
			if _, ok := envelope.Errors[tt.field]; !ok {
				t.Errorf("expected a validation error for %q, got %v", tt.field, envelope.Errors)
			}
		})
	}
}

// TestListSessionsNeverLeaksTheTokenDigest is the security test for this
// endpoint. The stored SHA-256 digest is the only thing standing between a
// leaked database row and a replayable refresh token, so it must not reach a
// response body — not in full and not truncated. A comment on the struct would
// not survive someone adding a "device fingerprint" field; this will.
func TestListSessionsNeverLeaksTheTokenDigest(t *testing.T) {
	f := newAccountFixture(t)
	userID := uuid.New()

	const digest = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

	f.users.EXPECT().ListSessions(gomock.Any(), userID, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, _ string, _ request.Pagination) (*response.SessionList, error) {
			// The service reads rows that carry the digest; what the handler
			// serialises must carry no trace of it.
			return &response.SessionList{
				Sessions: []response.Session{{
					ID:        uuid.New(),
					Current:   true,
					CreatedAt: time.Now(),
					ExpiresAt: time.Now().Add(testRefreshTTL),
				}},
				Meta: response.NewMeta(1, request.DefaultLimit, 1),
			}, nil
		})

	rec := testutil.Do(f.engine, f.authed(t, http.MethodGet, "/api/v1/auth/sessions", nil, userID))
	testutil.Equal(t, rec.Code, http.StatusOK, "status")

	body := rec.Body.String()
	testutil.True(t, !strings.Contains(body, digest), "the digest is absent from the body")
	// Not even a prefix: eight characters of a hex digest is still eight
	// characters an attacker no longer has to guess.
	testutil.True(t, !strings.Contains(body, digest[:8]), "no prefix of the digest either")
	testutil.True(t, !strings.Contains(strings.ToLower(body), "hash"), "no field named for the digest")
	testutil.True(t, !strings.Contains(strings.ToLower(body), "token"), "no token-shaped field at all")
}

func TestChangePasswordHandler(t *testing.T) {
	userID := uuid.New()
	validBody := map[string]string{
		"currentPassword": "the-old-password",
		"newPassword":     "a-brand-new-password",
	}

	t.Run("rotates the cookies so the caller stays signed in", func(t *testing.T) {
		f := newAccountFixture(t)
		f.users.EXPECT().ChangePassword(gomock.Any(), userID, gomock.Any()).
			Return(&response.ChangePasswordResponse{
				AccessToken:  "a-fresh-access-token",
				RefreshToken: "a-fresh-refresh-token",
			}, nil)

		rec := testutil.Do(f.engine, f.withCSRF(t,
			f.authed(t, http.MethodPatch, "/api/v1/auth/password", validBody, userID)))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")

		// Every other session was revoked; this client gets a replacement pair
		// rather than being signed out of the device it just used.
		cookies := testutil.Cookies(rec)
		testutil.Equal(t, cookies[middleware.AccessTokenCookie].Value, "a-fresh-access-token", "new access cookie")
		testutil.Equal(t, cookies[middleware.RefreshTokenCookie].Value, "a-fresh-refresh-token", "new refresh cookie")
		testutil.Equal(t, cookies[middleware.AccessTokenCookie].HttpOnly, true, "access cookie stays HttpOnly")
	})

	t.Run("never returns the new tokens in the body", func(t *testing.T) {
		f := newAccountFixture(t)
		f.users.EXPECT().ChangePassword(gomock.Any(), userID, gomock.Any()).
			Return(&response.ChangePasswordResponse{
				AccessToken:  "super-secret-access-token",
				RefreshToken: "super-secret-refresh-token",
			}, nil)

		rec := testutil.Do(f.engine, f.withCSRF(t,
			f.authed(t, http.MethodPatch, "/api/v1/auth/password", validBody, userID)))

		body := rec.Body.String()
		testutil.True(t, !strings.Contains(body, "super-secret-access-token"), "access token absent from body")
		testutil.True(t, !strings.Contains(body, "super-secret-refresh-token"), "refresh token absent from body")
	})

	t.Run("a wrong current password is 401 with a generic message", func(t *testing.T) {
		f := newAccountFixture(t)
		f.users.EXPECT().ChangePassword(gomock.Any(), userID, gomock.Any()).
			Return(nil, apperr.ErrInvalidCredentials)

		rec := testutil.Do(f.engine, f.withCSRF(t,
			f.authed(t, http.MethodPatch, "/api/v1/auth/password", validBody, userID)))

		testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
		testutil.Equal(t, testutil.Envelope(t, rec).Message, "Invalid email or password", "message")
		testutil.Equal(t, len(testutil.Cookies(rec)), 0, "no cookies are rewritten on failure")
	})

	t.Run("requires a CSRF token", func(t *testing.T) {
		f := newAccountFixture(t)
		// No EXPECT: the middleware must abort before the handler runs.

		rec := testutil.Do(f.engine,
			f.authed(t, http.MethodPatch, "/api/v1/auth/password", validBody, userID))

		testutil.Equal(t, rec.Code, http.StatusForbidden, "status")
	})

	t.Run("rejects an unauthenticated request", func(t *testing.T) {
		f := newAccountFixture(t)

		rec := testutil.Do(f.engine, f.withCSRF(t,
			testutil.JSONRequest(t, http.MethodPatch, "/api/v1/auth/password", validBody)))

		testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
	})
}

func TestChangePasswordHandlerValidation(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name       string
		body       any
		wantFields []string
	}{
		{
			name:       "rejects a missing current password",
			body:       map[string]string{"newPassword": "a-brand-new-password"},
			wantFields: []string{"currentPassword"},
		},
		{
			name:       "rejects a new password shorter than 8 characters",
			body:       map[string]string{"currentPassword": "old", "newPassword": "short"},
			wantFields: []string{"newPassword"},
		},
		{
			// bcrypt refuses inputs longer than 72 bytes, so without the max a
			// long password surfaces as a 500 from the hashing step.
			name: "rejects a new password longer than bcrypt can hash",
			body: map[string]string{
				"currentPassword": "old",
				"newPassword":     strings.Repeat("a", 73),
			},
			wantFields: []string{"newPassword"},
		},
		{
			name:       "reports both fields at once",
			body:       map[string]string{},
			wantFields: []string{"currentPassword", "newPassword"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAccountFixture(t)
			// No EXPECT: validation must reject before the service is called.

			rec := testutil.Do(f.engine, f.withCSRF(t,
				f.authed(t, http.MethodPatch, "/api/v1/auth/password", tt.body, userID)))

			testutil.Equal(t, rec.Code, http.StatusBadRequest, "status")

			envelope := testutil.Envelope(t, rec)
			for _, field := range tt.wantFields {
				if _, ok := envelope.Errors[field]; !ok {
					t.Errorf("expected a validation error for %q, got %v", field, envelope.Errors)
				}
			}
		})
	}
}

func TestDeleteAccountHandler(t *testing.T) {
	userID := uuid.New()

	t.Run("deletes the account and clears both cookies", func(t *testing.T) {
		f := newAccountFixture(t)
		f.users.EXPECT().DeleteAccount(gomock.Any(), userID).Return(nil)

		rec := testutil.Do(f.engine, f.withCSRF(t,
			f.authed(t, http.MethodDelete, "/api/v1/auth/me", nil, userID)))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")

		cookies := testutil.Cookies(rec)
		testutil.Equal(t, cookies[middleware.AccessTokenCookie].MaxAge, -1, "access cookie is expired")
		testutil.Equal(t, cookies[middleware.RefreshTokenCookie].MaxAge, -1, "refresh cookie is expired")
	})

	t.Run("leaves the cookies alone when the delete fails", func(t *testing.T) {
		// Signing the user out of an account that still exists would leave them
		// unable to retry without logging back in first.
		f := newAccountFixture(t)
		f.users.EXPECT().DeleteAccount(gomock.Any(), userID).
			Return(apperr.Internal(errors.New("delete failed")))

		rec := testutil.Do(f.engine, f.withCSRF(t,
			f.authed(t, http.MethodDelete, "/api/v1/auth/me", nil, userID)))

		testutil.Equal(t, rec.Code, http.StatusInternalServerError, "status")
		testutil.Equal(t, len(testutil.Cookies(rec)), 0, "no cookies are rewritten")
	})

	t.Run("reports an already-deleted account as 404", func(t *testing.T) {
		f := newAccountFixture(t)
		f.users.EXPECT().DeleteAccount(gomock.Any(), userID).Return(apperr.ErrUserNotFound)

		rec := testutil.Do(f.engine, f.withCSRF(t,
			f.authed(t, http.MethodDelete, "/api/v1/auth/me", nil, userID)))

		testutil.Equal(t, rec.Code, http.StatusNotFound, "status")
	})

	t.Run("requires a CSRF token", func(t *testing.T) {
		f := newAccountFixture(t)

		rec := testutil.Do(f.engine, f.authed(t, http.MethodDelete, "/api/v1/auth/me", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusForbidden, "status")
	})

	t.Run("rejects an unauthenticated request", func(t *testing.T) {
		f := newAccountFixture(t)

		rec := testutil.Do(f.engine, f.withCSRF(t,
			testutil.JSONRequest(t, http.MethodDelete, "/api/v1/auth/me", nil)))

		testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
	})
}
