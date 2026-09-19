package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// protectedEngine mounts the middleware under test in front of a handler that
// reports what ended up in the request context.
func protectedEngine(t *testing.T, mw gin.HandlerFunc) *gin.Engine {
	t.Helper()

	r := testutil.NewEngine(t)
	r.GET("/protected", mw, func(c *gin.Context) {
		userID, err := middleware.GetUserIDFromContext(c)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"authenticated": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"authenticated": true,
			"user_id":       userID.String(),
			"email":         middleware.GetEmailFromContext(c),
		})
	})
	return r
}

func TestAuthMiddleware(t *testing.T) {
	tokens := newTokenService()
	userID := uuid.New()

	validToken, err := tokens.GenerateAccessToken(userID, "test@example.com", "Test User")
	testutil.NoError(t, err)

	expiredIssuer := token.NewTokenService(token.TokenConfig{
		AccessTokenSecret: "test-access-secret",
		AccessTokenExpiry: -time.Hour,
	})
	expiredToken, err := expiredIssuer.GenerateAccessToken(userID, "test@example.com", "Test User")
	testutil.NoError(t, err)

	otherIssuer := token.NewTokenService(token.TokenConfig{
		AccessTokenSecret: "a-completely-different-secret",
		AccessTokenExpiry: 15 * time.Minute,
	})
	foreignToken, err := otherIssuer.GenerateAccessToken(userID, "test@example.com", "Test User")
	testutil.NoError(t, err)

	refreshToken, err := tokens.GenerateRefreshToken(userID)
	testutil.NoError(t, err)

	tests := []struct {
		name       string
		cookie     string
		setCookie  bool
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "allows a valid access token",
			cookie:     validToken,
			setCookie:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "rejects a request with no cookie",
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "Missing authentication token",
		},
		{
			name:       "rejects a malformed token",
			cookie:     "not-a-jwt",
			setCookie:  true,
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "Invalid or expired token",
		},
		{
			name:       "rejects an expired token",
			cookie:     expiredToken,
			setCookie:  true,
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "Invalid or expired token",
		},
		{
			name:       "rejects a token signed with another secret",
			cookie:     foreignToken,
			setCookie:  true,
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "Invalid or expired token",
		},
		{
			// The two signing secrets must not be interchangeable, or a stolen
			// refresh token would grant immediate API access.
			name:       "rejects a refresh token used as an access token",
			cookie:     refreshToken,
			setCookie:  true,
			wantStatus: http.StatusUnauthorized,
			wantMsg:    "Invalid or expired token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := protectedEngine(t, middleware.AuthMiddleware(tokens))

			req := testutil.JSONRequest(t, http.MethodGet, "/protected", nil)
			if tt.setCookie {
				testutil.WithCookie(req, middleware.AccessTokenCookie, tt.cookie)
			}

			rec := testutil.Do(engine, req)

			testutil.Equal(t, rec.Code, tt.wantStatus, "status")
			if tt.wantMsg != "" {
				envelope := testutil.Envelope(t, rec)
				testutil.Equal(t, envelope.Success, false, "success flag")
				testutil.Equal(t, envelope.Message, tt.wantMsg, "message")
			}
		})
	}
}

func TestAuthMiddlewarePopulatesContext(t *testing.T) {
	tokens := newTokenService()
	userID := uuid.New()

	accessToken, err := tokens.GenerateAccessToken(userID, "test@example.com", "Test User")
	testutil.NoError(t, err)

	engine := protectedEngine(t, middleware.AuthMiddleware(tokens))
	req := testutil.WithCookie(
		testutil.JSONRequest(t, http.MethodGet, "/protected", nil),
		middleware.AccessTokenCookie, accessToken,
	)

	rec := testutil.Do(engine, req)
	testutil.Equal(t, rec.Code, http.StatusOK, "status")

	var body struct {
		Authenticated bool   `json:"authenticated"`
		UserID        string `json:"user_id"`
		Email         string `json:"email"`
	}
	decodeJSON(t, rec, &body)

	testutil.Equal(t, body.Authenticated, true, "authenticated")
	testutil.Equal(t, body.UserID, userID.String(), "user id in context")
	testutil.Equal(t, body.Email, "test@example.com", "email in context")
}

func TestOptionalAuthMiddleware(t *testing.T) {
	tokens := newTokenService()
	accessToken, err := tokens.GenerateAccessToken(uuid.New(), "test@example.com", "Test User")
	testutil.NoError(t, err)

	tests := []struct {
		name              string
		cookie            string
		setCookie         bool
		wantAuthenticated bool
	}{
		{name: "passes an anonymous request through"},
		{name: "passes an invalid token through as anonymous", cookie: "not-a-jwt", setCookie: true},
		{name: "populates the context for a valid token", cookie: accessToken, setCookie: true, wantAuthenticated: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := protectedEngine(t, middleware.OptionalAuthMiddleware(tokens))

			req := testutil.JSONRequest(t, http.MethodGet, "/protected", nil)
			if tt.setCookie {
				testutil.WithCookie(req, middleware.AccessTokenCookie, tt.cookie)
			}

			rec := testutil.Do(engine, req)

			// Anonymous or not, an optional-auth route always answers 200.
			testutil.Equal(t, rec.Code, http.StatusOK, "status")

			var body struct {
				Authenticated bool `json:"authenticated"`
			}
			decodeJSON(t, rec, &body)
			testutil.Equal(t, body.Authenticated, tt.wantAuthenticated, "authenticated")
		})
	}
}

// TestContextExtractorsSurviveWrongTypes covers what used to crash the process:
// a context value of an unexpected type must produce a zero value, not a failed
// type assertion.
func TestContextExtractorsSurviveWrongTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("user id of the wrong type", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(middleware.UserIDCtxKey, 42)

		_, err := middleware.GetUserIDFromContext(c)
		testutil.Error(t, err, "user id stored as an int")
	})

	t.Run("user id missing entirely", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())

		_, err := middleware.GetUserIDFromContext(c)
		testutil.ErrorIs(t, err, middleware.ErrNoUserInContext)
	})

	t.Run("email of the wrong type", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(middleware.UserEmailCtxKey, struct{}{})

		testutil.Equal(t, middleware.GetEmailFromContext(c), "", "email")
	})

	t.Run("claims of the wrong type", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(middleware.ClaimsCtxKey, "not-claims")

		if claims := middleware.GetClaimsFromContext(c); claims != nil {
			t.Errorf("expected nil claims, got %+v", claims)
		}
	})
}
