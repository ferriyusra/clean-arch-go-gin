package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/middleware"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	userSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/user"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	// Handlers report validation failures using JSON field names, which requires
	// the tag-name hook the application installs at startup.
	RegisterValidationTagNames()
	// Silence the error logs the handlers emit for unexpected service failures.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// stubUserService is a hand-written UserService whose every method returns
// whatever the test asked for. The handler layer only needs canned answers, so a
// stub keeps these tests readable next to the gomock-based service tests.
type stubUserService struct {
	registerResp *response.RegisterResponse
	registerErr  error
	loginResp    *response.LoginResponse
	loginErr     error
	refreshResp  *response.RefreshResponse
	refreshErr   error
	getUserResp  *response.GetUser
	getUserErr   error
	revokeErr    error

	// Captured call arguments.
	revokedUserID  uuid.UUID
	gotRefreshTok  string
	gotUserIDParam string
}

func (s *stubUserService) Register(_ context.Context, _ *request.RegisterUserRequest) (*response.RegisterResponse, error) {
	return s.registerResp, s.registerErr
}

func (s *stubUserService) Login(_ context.Context, _ *request.LoginRequest) (*response.LoginResponse, error) {
	return s.loginResp, s.loginErr
}

func (s *stubUserService) Refresh(_ context.Context, refreshToken string) (*response.RefreshResponse, error) {
	s.gotRefreshTok = refreshToken
	return s.refreshResp, s.refreshErr
}

func (s *stubUserService) GetUser(_ context.Context, userID string) (*response.GetUser, error) {
	s.gotUserIDParam = userID
	return s.getUserResp, s.getUserErr
}

func (s *stubUserService) RevokeRefreshTokens(_ context.Context, userID uuid.UUID) error {
	s.revokedUserID = userID
	return s.revokeErr
}

func (s *stubUserService) PurgeExpiredRefreshTokens(_ context.Context) (int64, error) {
	return 0, nil
}

// newRouter mounts the user handler. authedUserID, when non-empty, simulates the
// context an authenticated request arrives with from AuthMiddleware.
func newRouter(svc userSvc.UserService, authedUserID string) *gin.Engine {
	h := NewUserHandler(svc)

	r := gin.New()
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	r.POST("/refresh", h.Refresh)

	protected := r.Group("")
	if authedUserID != "" {
		protected.Use(func(c *gin.Context) {
			c.Set(middleware.UserIDCtxKey, authedUserID)
			c.Next()
		})
	}
	protected.GET("/me", h.GetMe)
	protected.POST("/logout", h.Logout)

	return r
}

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) response.APIResponse {
	t.Helper()
	var body response.APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshalling response %q: %v", w.Body.String(), err)
	}
	return body
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		expectedField string
	}{
		{
			name:          "should reject a malformed email",
			body:          `{"email":"not-an-email","password":"password123","name":"Test"}`,
			expectedField: "email",
		},
		{
			name:          "should reject a password below the minimum length",
			body:          `{"email":"test@example.com","password":"short","name":"Test"}`,
			expectedField: "password",
		},
		{
			name: "should reject a password longer than bcrypt's 72-byte limit",
			body: fmt.Sprintf(`{"email":"test@example.com","password":%q,"name":"Test"}`,
				strings.Repeat("a", 73)),
			expectedField: "password",
		},
		{
			name:          "should reject a missing name",
			body:          `{"email":"test@example.com","password":"password123"}`,
			expectedField: "name",
		},
		{
			name:          "should reject an empty body",
			body:          `{}`,
			expectedField: "email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter(&stubUserService{}, "")
			w := doJSON(r, http.MethodPost, "/register", tt.body)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
			}

			body := decodeBody(t, w)
			if body.Success {
				t.Error("expected success=false")
			}
			if _, ok := body.Errors[tt.expectedField]; !ok {
				t.Errorf("expected a validation error for %q, got %v", tt.expectedField, body.Errors)
			}
		})
	}
}

func TestRegisterMalformedJSON(t *testing.T) {
	r := newRouter(&stubUserService{}, "")
	w := doJSON(r, http.MethodPost, "/register", `{"email":`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body.Message != "Invalid request body" {
		t.Errorf("expected a generic bad-body message, got %q", body.Message)
	}
	if len(body.Errors) != 0 {
		t.Errorf("expected no field errors for unparseable JSON, got %v", body.Errors)
	}
}

func TestRegisterSuccess(t *testing.T) {
	userID := uuid.New()
	svc := &stubUserService{
		registerResp: &response.RegisterResponse{
			User: response.GetUser{ID: userID, Email: "test@example.com", Name: "Test"},
		},
	}

	r := newRouter(svc, "")
	w := doJSON(r, http.MethodPost, "/register", `{"email":"test@example.com","password":"password123","name":"Test"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if !body.Success {
		t.Error("expected success=true")
	}
}

func TestServiceErrorStatusMapping(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           string
		svc            *stubUserService
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "duplicate email is a conflict",
			path:           "/register",
			body:           `{"email":"test@example.com","password":"password123","name":"Test"}`,
			svc:            &stubUserService{registerErr: userSvc.ErrEmailAlreadyRegistered},
			expectedStatus: http.StatusConflict,
			expectedMsg:    userSvc.ErrEmailAlreadyRegistered.Error(),
		},
		{
			name:           "bad credentials are unauthorized",
			path:           "/login",
			body:           `{"email":"test@example.com","password":"password123"}`,
			svc:            &stubUserService{loginErr: userSvc.ErrInvalidCredentials},
			expectedStatus: http.StatusUnauthorized,
			expectedMsg:    userSvc.ErrInvalidCredentials.Error(),
		},
		{
			name:           "a revoked refresh token is unauthorized",
			path:           "/refresh",
			body:           `{"refreshToken":"some-token"}`,
			svc:            &stubUserService{refreshErr: userSvc.ErrRefreshTokenRevoked},
			expectedStatus: http.StatusUnauthorized,
			expectedMsg:    userSvc.ErrRefreshTokenRevoked.Error(),
		},
		{
			name:           "an expired refresh token is unauthorized",
			path:           "/refresh",
			body:           `{"refreshToken":"some-token"}`,
			svc:            &stubUserService{refreshErr: userSvc.ErrRefreshTokenExpired},
			expectedStatus: http.StatusUnauthorized,
			expectedMsg:    userSvc.ErrRefreshTokenExpired.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter(tt.svc, "")
			w := doJSON(r, http.MethodPost, tt.path, tt.body)

			if w.Code != tt.expectedStatus {
				t.Fatalf("expected %d, got %d (body: %s)", tt.expectedStatus, w.Code, w.Body.String())
			}
			body := decodeBody(t, w)
			if body.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, body.Message)
			}
		})
	}
}

// TestUnexpectedServiceErrorIsNotLeaked is the regression guard for the handler
// previously answering with err.Error(), which exposed wrapped database detail.
func TestUnexpectedServiceErrorIsNotLeaked(t *testing.T) {
	leaky := fmt.Errorf("finding user by email: %w",
		fmt.Errorf(`pq: relation "users" does not exist (host=db.internal user=admin)`))

	svc := &stubUserService{loginErr: leaky}
	r := newRouter(svc, "")
	w := doJSON(r, http.MethodPost, "/login", `{"email":"test@example.com","password":"password123"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (body: %s)", w.Code, w.Body.String())
	}

	body := decodeBody(t, w)
	if body.Message != "Internal server error" {
		t.Errorf("expected a generic message, got %q", body.Message)
	}

	for _, secret := range []string{"pq:", "relation", "db.internal", "admin", "finding user by email"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("response leaked internal detail %q: %s", secret, w.Body.String())
		}
	}
}

func TestRefreshRequiresToken(t *testing.T) {
	r := newRouter(&stubUserService{}, "")
	w := doJSON(r, http.MethodPost, "/refresh", `{}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if _, ok := body.Errors["refreshToken"]; !ok {
		t.Errorf("expected a validation error keyed by the JSON field name, got %v", body.Errors)
	}
}

func TestProtectedEndpointsRejectMissingContext(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "GetMe", method: http.MethodGet, path: "/me"},
		{name: "Logout", method: http.MethodPost, path: "/logout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No auth middleware ran, so no user is in the context.
			r := newRouter(&stubUserService{}, "")
			w := doJSON(r, tt.method, tt.path, "")

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d (body: %s)", w.Code, w.Body.String())
			}
		})
	}
}

func TestGetMeReturnsCurrentUser(t *testing.T) {
	userID := uuid.New()
	svc := &stubUserService{
		getUserResp: &response.GetUser{ID: userID, Email: "test@example.com", Name: "Test"},
	}

	r := newRouter(svc, userID.String())
	w := doJSON(r, http.MethodGet, "/me", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if svc.gotUserIDParam != userID.String() {
		t.Errorf("expected the service to be asked for %s, got %s", userID, svc.gotUserIDParam)
	}
}

func TestLogoutRevokesTokensForTheAuthenticatedUser(t *testing.T) {
	userID := uuid.New()
	svc := &stubUserService{}

	r := newRouter(svc, userID.String())
	w := doJSON(r, http.MethodPost, "/logout", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if svc.revokedUserID != userID {
		t.Errorf("expected tokens revoked for %s, got %s", userID, svc.revokedUserID)
	}
}
