package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
)

func newTestTokenService() token.TokenService {
	return token.NewTokenService(token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})
}

func TestAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tokenSvc := newTestTokenService()
	userID := uuid.New()
	validToken, err := tokenSvc.GenerateAccessToken(userID, "test@example.com", "Test User")
	if err != nil {
		t.Fatalf("generating access token: %v", err)
	}

	// A refresh token is signed with a different secret, so it must not be
	// accepted as an access token.
	refreshToken, err := tokenSvc.GenerateRefreshToken(userID)
	if err != nil {
		t.Fatalf("generating refresh token: %v", err)
	}

	expiredSvc := token.NewTokenService(token.TokenConfig{
		AccessTokenSecret: "test-access-secret",
		AccessTokenExpiry: -1 * time.Minute,
	})
	expiredToken, err := expiredSvc.GenerateAccessToken(userID, "test@example.com", "Test User")
	if err != nil {
		t.Fatalf("generating expired token: %v", err)
	}

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "should allow request with a valid bearer token",
			authHeader:     "Bearer " + validToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "should accept a lowercase bearer scheme",
			authHeader:     "bearer " + validToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "should reject a missing Authorization header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "should reject a header without the Bearer scheme",
			authHeader:     validToken,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "should reject an unknown scheme",
			authHeader:     "Basic " + validToken,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "should reject a malformed token",
			authHeader:     "Bearer not-a-jwt",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "should reject an expired token",
			authHeader:     "Bearer " + expiredToken,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "should reject a refresh token used as an access token",
			authHeader:     "Bearer " + refreshToken,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/protected", AuthMiddleware(tokenSvc), func(c *gin.Context) {
				c.JSON(http.StatusOK, response.OK("ok", nil))
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d (body: %s)", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectedStatus == http.StatusUnauthorized {
				var body response.APIResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatalf("unmarshalling response: %v", err)
				}
				if body.Success {
					t.Error("expected success=false on a rejected request")
				}
			}
		})
	}
}

func TestAuthMiddlewarePopulatesContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tokenSvc := newTestTokenService()
	userID := uuid.New()
	accessToken, err := tokenSvc.GenerateAccessToken(userID, "test@example.com", "Test User")
	if err != nil {
		t.Fatalf("generating access token: %v", err)
	}

	var (
		gotUserID uuid.UUID
		gotEmail  string
		gotName   string
	)

	r := gin.New()
	r.GET("/protected", AuthMiddleware(tokenSvc), func(c *gin.Context) {
		id, err := GetUserIDFromContext(c)
		if err != nil {
			t.Errorf("GetUserIDFromContext: %v", err)
		}
		gotUserID = id
		gotEmail = GetEmailFromContext(c)
		if claims := GetClaimsFromContext(c); claims != nil {
			gotName = claims.Name
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if gotUserID != userID {
		t.Errorf("expected user id %s, got %s", userID, gotUserID)
	}
	if gotEmail != "test@example.com" {
		t.Errorf("expected email test@example.com, got %q", gotEmail)
	}
	if gotName != "Test User" {
		t.Errorf("expected name 'Test User', got %q", gotName)
	}
}

func TestOptionalAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tokenSvc := newTestTokenService()
	userID := uuid.New()
	accessToken, err := tokenSvc.GenerateAccessToken(userID, "test@example.com", "Test User")
	if err != nil {
		t.Fatalf("generating access token: %v", err)
	}

	tests := []struct {
		name              string
		authHeader        string
		expectAuthedUser  bool
		expectedUserEmail string
	}{
		{
			name:              "should populate context when a valid token is present",
			authHeader:        "Bearer " + accessToken,
			expectAuthedUser:  true,
			expectedUserEmail: "test@example.com",
		},
		{
			name:             "should proceed anonymously with no token",
			authHeader:       "",
			expectAuthedUser: false,
		},
		{
			name:             "should proceed anonymously with an invalid token",
			authHeader:       "Bearer not-a-jwt",
			expectAuthedUser: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var authed bool
			var email string

			r := gin.New()
			r.GET("/optional", OptionalAuthMiddleware(tokenSvc), func(c *gin.Context) {
				_, err := GetUserIDFromContext(c)
				authed = err == nil
				email = GetEmailFromContext(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/optional", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// Anonymous callers must still reach the handler.
			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", w.Code)
			}
			if authed != tt.expectAuthedUser {
				t.Errorf("expected authenticated=%v, got %v", tt.expectAuthedUser, authed)
			}
			if email != tt.expectedUserEmail {
				t.Errorf("expected email %q, got %q", tt.expectedUserEmail, email)
			}
		})
	}
}
