package handler_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/handler"
	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/csrf"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/mock"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

const (
	testAccessTTL  = 15 * time.Minute
	testRefreshTTL = 7 * 24 * time.Hour
)

type userFixture struct {
	users  *mock.MockUserService
	tokens token.TokenService
	engine *gin.Engine
}

// newUserFixture mounts the user routes behind the same middleware production
// uses, so these tests cover the chain (auth, CSRF) and not just handler bodies.
func newUserFixture(t *testing.T) *userFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	users := mock.NewMockUserService(ctrl)

	tokens := token.NewTokenService(token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  testAccessTTL,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: testRefreshTTL,
	})

	h := handler.NewUserHandler(users, csrf.NewCSRFService("test-csrf-secret", time.Hour), handler.CookieConfig{
		AccessTTL:  testAccessTTL,
		RefreshTTL: testRefreshTTL,
		Secure:     true,
	})

	r := testutil.NewEngine(t)
	r.POST("/api/auth/register", h.Register)
	r.POST("/api/auth/login", h.Login)
	r.POST("/api/auth/refresh", h.Refresh)
	r.GET("/api/csrf", h.GetCSRFToken)

	protected := r.Group("", middleware.AuthMiddleware(tokens))
	protected.GET("/api/auth/me", h.GetMe)
	protected.POST("/api/auth/logout", h.Logout)

	return &userFixture{users: users, tokens: tokens, engine: r}
}

// authed builds a request carrying a valid access token for userID.
func (f *userFixture) authed(t *testing.T, method, target string, body any, userID uuid.UUID) *http.Request {
	t.Helper()

	accessToken, err := f.tokens.GenerateAccessToken(userID, "test@example.com", "Test User")
	testutil.NoError(t, err)

	return testutil.WithCookie(
		testutil.JSONRequest(t, method, target, body),
		middleware.AccessTokenCookie, accessToken,
	)
}

func TestRegisterHandler(t *testing.T) {
	validBody := map[string]string{
		"email":    "test@example.com",
		"password": "password123",
		"name":     "Test User",
	}

	tests := []struct {
		name       string
		body       any
		expect     func(f *userFixture)
		wantStatus int
		wantMsg    string
		wantFields []string
	}{
		{
			name: "creates the account and sets auth cookies",
			body: validBody,
			expect: func(f *userFixture) {
				f.users.EXPECT().Register(gomock.Any(), gomock.Any()).
					Return(&response.RegisterResponse{
						User:         response.GetUser{ID: uuid.New(), Email: "test@example.com", Name: "Test User"},
						AccessToken:  "access-token-value",
						RefreshToken: "refresh-token-value",
					}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			// The old handler got this wrong: a duplicate email returned 400,
			// indistinguishable from malformed input.
			name: "reports a duplicate email as 409",
			body: validBody,
			expect: func(f *userFixture) {
				f.users.EXPECT().Register(gomock.Any(), gomock.Any()).
					Return(nil, apperr.ErrUserAlreadyExists)
			},
			wantStatus: http.StatusConflict,
			wantMsg:    "Email is already registered",
		},
		{
			// And this one: a database outage used to return 400 with the
			// driver error in the response body.
			name: "reports an internal failure as 500 with a generic message",
			body: validBody,
			expect: func(f *userFixture) {
				f.users.EXPECT().Register(gomock.Any(), gomock.Any()).
					Return(nil, apperr.Internal(errors.New("dial tcp 10.0.0.5:5432: connection refused")))
			},
			wantStatus: http.StatusInternalServerError,
			wantMsg:    "Internal server error",
		},
		{
			name:       "rejects a missing email",
			body:       map[string]string{"password": "password123", "name": "Test User"},
			expect:     func(*userFixture) {},
			wantStatus: http.StatusBadRequest,
			wantFields: []string{"email"},
		},
		{
			name:       "rejects a malformed email",
			body:       map[string]string{"email": "not-an-email", "password": "password123", "name": "Test User"},
			expect:     func(*userFixture) {},
			wantStatus: http.StatusBadRequest,
			wantFields: []string{"email"},
		},
		{
			name:       "rejects a password shorter than 8 characters",
			body:       map[string]string{"email": "test@example.com", "password": "short", "name": "Test User"},
			expect:     func(*userFixture) {},
			wantStatus: http.StatusBadRequest,
			wantFields: []string{"password"},
		},
		{
			// bcrypt refuses inputs longer than 72 bytes, so without this rule
			// a long password surfaces as a 500 from the hashing step.
			name: "rejects a password longer than bcrypt can hash",
			body: map[string]string{
				"email":    "test@example.com",
				"password": strings.Repeat("a", 73),
				"name":     "Test User",
			},
			expect:     func(*userFixture) {},
			wantStatus: http.StatusBadRequest,
			wantFields: []string{"password"},
		},
		{
			name:       "reports every invalid field at once",
			body:       map[string]string{},
			expect:     func(*userFixture) {},
			wantStatus: http.StatusBadRequest,
			wantFields: []string{"email", "password", "name"},
		},
		{
			name:       "rejects a malformed JSON body",
			body:       "{not json",
			expect:     func(*userFixture) {},
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Invalid request body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newUserFixture(t)
			tt.expect(f)

			rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", tt.body))

			testutil.Equal(t, rec.Code, tt.wantStatus, "status")

			envelope := testutil.Envelope(t, rec)
			if tt.wantMsg != "" {
				testutil.Equal(t, envelope.Message, tt.wantMsg, "message")
			}
			for _, field := range tt.wantFields {
				if _, ok := envelope.Errors[field]; !ok {
					t.Errorf("expected a validation error for %q, got %v", field, envelope.Errors)
				}
			}
		})
	}
}

// TestRegisterSetsSecureCookies pins the properties that make the cookie
// strategy safe. They are invisible in the response body and easy to break.
func TestRegisterSetsSecureCookies(t *testing.T) {
	f := newUserFixture(t)
	f.users.EXPECT().Register(gomock.Any(), gomock.Any()).
		Return(&response.RegisterResponse{
			User:         response.GetUser{ID: uuid.New(), Email: "test@example.com", Name: "Test User"},
			AccessToken:  "access-token-value",
			RefreshToken: "refresh-token-value",
		}, nil)

	rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "test@example.com", "password": "password123", "name": "Test User",
	}))

	cookies := testutil.Cookies(rec)

	access, ok := cookies[middleware.AccessTokenCookie]
	testutil.True(t, ok, "access token cookie is set")
	testutil.Equal(t, access.Value, "access-token-value", "access cookie value")
	testutil.Equal(t, access.HttpOnly, true, "access cookie is HttpOnly")
	testutil.Equal(t, access.Secure, true, "access cookie is Secure outside dev mode")
	testutil.Equal(t, access.MaxAge, int(testAccessTTL.Seconds()), "access cookie max-age tracks the token TTL")

	refresh, ok := cookies[middleware.RefreshTokenCookie]
	testutil.True(t, ok, "refresh token cookie is set")
	testutil.Equal(t, refresh.HttpOnly, true, "refresh cookie is HttpOnly")
	testutil.Equal(t, refresh.MaxAge, int(testRefreshTTL.Seconds()), "refresh cookie max-age tracks the token TTL")
}

// TestRegisterNeverReturnsTokensInTheBody guards the json:"-" tags: tokens must
// travel only as HttpOnly cookies, out of reach of JavaScript.
func TestRegisterNeverReturnsTokensInTheBody(t *testing.T) {
	f := newUserFixture(t)
	f.users.EXPECT().Register(gomock.Any(), gomock.Any()).
		Return(&response.RegisterResponse{
			User:         response.GetUser{ID: uuid.New(), Email: "test@example.com", Name: "Test User"},
			AccessToken:  "super-secret-access-token",
			RefreshToken: "super-secret-refresh-token",
		}, nil)

	rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "test@example.com", "password": "password123", "name": "Test User",
	}))

	body := rec.Body.String()
	testutil.True(t, !strings.Contains(body, "super-secret-access-token"), "access token absent from body")
	testutil.True(t, !strings.Contains(body, "super-secret-refresh-token"), "refresh token absent from body")
}

func TestLoginHandler(t *testing.T) {
	credentials := map[string]string{"email": "test@example.com", "password": "password123"}

	tests := []struct {
		name       string
		body       any
		expect     func(f *userFixture)
		wantStatus int
		wantMsg    string
	}{
		{
			name: "authenticates and sets cookies",
			body: credentials,
			expect: func(f *userFixture) {
				f.users.EXPECT().Login(gomock.Any(), gomock.Any()).
					Return(&response.LoginResponse{
						User:         response.GetUser{ID: uuid.New(), Email: "test@example.com"},
						AccessToken:  "access-token-value",
						RefreshToken: "refresh-token-value",
					}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "rejects bad credentials with 401",
			body: credentials,
			expect: func(f *userFixture) {
				f.users.EXPECT().Login(gomock.Any(), gomock.Any()).Return(nil, apperr.ErrInvalidCredentials)
			},
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "Invalid email or password",
		},
		{
			// The old handler answered 401 here too, disguising an outage as an
			// auth failure and sending users to reset passwords that were fine.
			name: "reports a database outage as 500, not 401",
			body: credentials,
			expect: func(f *userFixture) {
				f.users.EXPECT().Login(gomock.Any(), gomock.Any()).
					Return(nil, apperr.Internal(errors.New("connection refused")))
			},
			wantStatus: http.StatusInternalServerError,
			wantMsg:    "Internal server error",
		},
		{
			name:       "rejects a missing password",
			body:       map[string]string{"email": "test@example.com"},
			expect:     func(*userFixture) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newUserFixture(t)
			tt.expect(f)

			rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/login", tt.body))

			testutil.Equal(t, rec.Code, tt.wantStatus, "status")
			if tt.wantMsg != "" {
				testutil.Equal(t, testutil.Envelope(t, rec).Message, tt.wantMsg, "message")
			}
		})
	}
}

func TestRefreshHandler(t *testing.T) {
	t.Run("rotates both cookies", func(t *testing.T) {
		f := newUserFixture(t)
		f.users.EXPECT().Refresh(gomock.Any(), "the-refresh-token").
			Return(&response.RefreshResponse{
				AccessToken:  "a-fresh-access-token",
				RefreshToken: "a-fresh-refresh-token",
			}, nil)

		req := testutil.WithCookie(
			testutil.JSONRequest(t, http.MethodPost, "/api/auth/refresh", nil),
			middleware.RefreshTokenCookie, "the-refresh-token",
		)
		rec := testutil.Do(f.engine, req)

		testutil.Equal(t, rec.Code, http.StatusOK, "status")

		cookies := testutil.Cookies(rec)
		testutil.Equal(t, cookies[middleware.AccessTokenCookie].Value, "a-fresh-access-token", "new access cookie")

		// The refresh cookie has to be replaced too. Leaving the old one in
		// place would keep a token alive that the server has already deleted,
		// and the next refresh would look like a replay.
		refresh, ok := cookies[middleware.RefreshTokenCookie]
		testutil.True(t, ok, "the refresh cookie is rewritten")
		testutil.Equal(t, refresh.Value, "a-fresh-refresh-token", "new refresh cookie")
		testutil.Equal(t, refresh.HttpOnly, true, "refresh cookie stays HttpOnly")
	})

	t.Run("rejects a request with no refresh cookie", func(t *testing.T) {
		f := newUserFixture(t)

		rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodPost, "/api/auth/refresh", nil))

		testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
		testutil.Equal(t, testutil.Envelope(t, rec).Message, "Missing refresh token", "message")
	})

	t.Run("rejects a revoked refresh token", func(t *testing.T) {
		f := newUserFixture(t)
		f.users.EXPECT().Refresh(gomock.Any(), "revoked").Return(nil, apperr.ErrRefreshTokenRevoked)

		req := testutil.WithCookie(
			testutil.JSONRequest(t, http.MethodPost, "/api/auth/refresh", nil),
			middleware.RefreshTokenCookie, "revoked",
		)
		rec := testutil.Do(f.engine, req)

		testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
		testutil.Equal(t, testutil.Envelope(t, rec).Message, "Refresh token has been revoked", "message")
	})
}

func TestGetMeHandler(t *testing.T) {
	userID := uuid.New()

	t.Run("returns the authenticated user", func(t *testing.T) {
		f := newUserFixture(t)
		f.users.EXPECT().GetUser(gomock.Any(), userID.String()).
			Return(&response.GetUser{ID: userID, Email: "test@example.com", Name: "Test User"}, nil)

		rec := testutil.Do(f.engine, f.authed(t, http.MethodGet, "/api/auth/me", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")
		testutil.Equal(t, testutil.DataAs[response.GetUser](t, rec).Email, "test@example.com", "email")
	})

	t.Run("rejects an unauthenticated request", func(t *testing.T) {
		f := newUserFixture(t)

		rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodGet, "/api/auth/me", nil))

		testutil.Equal(t, rec.Code, http.StatusUnauthorized, "status")
	})

	t.Run("reports a lookup failure as 500, not 404", func(t *testing.T) {
		// The old handler answered 404 for every error GetUser could return.
		f := newUserFixture(t)
		f.users.EXPECT().GetUser(gomock.Any(), userID.String()).
			Return(nil, apperr.Internal(errors.New("connection refused")))

		rec := testutil.Do(f.engine, f.authed(t, http.MethodGet, "/api/auth/me", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusInternalServerError, "status")
	})

	t.Run("reports a genuinely missing user as 404", func(t *testing.T) {
		f := newUserFixture(t)
		f.users.EXPECT().GetUser(gomock.Any(), userID.String()).Return(nil, apperr.ErrUserNotFound)

		rec := testutil.Do(f.engine, f.authed(t, http.MethodGet, "/api/auth/me", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusNotFound, "status")
	})
}

func TestLogoutHandler(t *testing.T) {
	userID := uuid.New()

	t.Run("revokes tokens and clears cookies", func(t *testing.T) {
		f := newUserFixture(t)
		f.users.EXPECT().RevokeRefreshTokens(gomock.Any(), userID).Return(nil)

		rec := testutil.Do(f.engine, f.authed(t, http.MethodPost, "/api/auth/logout", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")

		cookies := testutil.Cookies(rec)
		testutil.Equal(t, cookies[middleware.AccessTokenCookie].MaxAge, -1, "access cookie is expired")
		testutil.Equal(t, cookies[middleware.RefreshTokenCookie].MaxAge, -1, "refresh cookie is expired")
	})

	t.Run("still clears cookies when revocation fails", func(t *testing.T) {
		// A client that cannot log out is worse than a stale database row, so
		// logout succeeds even when revocation does not.
		f := newUserFixture(t)
		f.users.EXPECT().RevokeRefreshTokens(gomock.Any(), userID).
			Return(apperr.Internal(errors.New("delete failed")))

		rec := testutil.Do(f.engine, f.authed(t, http.MethodPost, "/api/auth/logout", nil, userID))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")
		testutil.Equal(t, testutil.Cookies(rec)[middleware.AccessTokenCookie].MaxAge, -1, "access cookie is expired")
	})
}

func TestGetCSRFTokenHandler(t *testing.T) {
	f := newUserFixture(t)

	rec := testutil.Do(f.engine, testutil.JSONRequest(t, http.MethodGet, "/api/csrf", nil))

	testutil.Equal(t, rec.Code, http.StatusOK, "status")
	testutil.True(t, testutil.DataAs[response.CSRFTokenResponse](t, rec).Token != "", "a token is returned")
}
