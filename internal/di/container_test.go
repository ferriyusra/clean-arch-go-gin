package di

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/api/middleware"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/platform"
	tokenSvc "github.com/ferriyusra/boilerplate-golang-gin/internal/service/token"
)

// newTestConfig builds a dev-mode config backed by a throwaway SQLite file, so
// each test gets a real database and a fully wired container.
func newTestConfig(t *testing.T) *platform.Config {
	t.Helper()

	gin.SetMode(gin.TestMode)

	return &platform.Config{
		Server: platform.ServerConfig{
			Port:     8080,
			LogLevel: "error",
		},
		Database: platform.DatabaseConfig{
			Type:            "sqlite",
			DSN:             filepath.Join(t.TempDir(), "test.db"),
			MaxOpenConns:    1,
			MaxIdleConns:    1,
			ConnMaxLifetime: time.Minute,
			AutoMigrate:     true,
		},
		Auth: platform.AuthConfig{
			DevMode:            true,
			JWTIssuer:          "test-issuer",
			AccessTokenExpiry:  15 * time.Minute,
			RefreshTokenExpiry: 7 * 24 * time.Hour,
			AllowedOrigins:     []string{"http://localhost:5173"},
		},
		RateLimit: platform.RateLimitConfig{
			LoginAttempts: 100,
			LoginWindow:   time.Minute,
		},
	}
}

func newTestContainer(t *testing.T, cfg *platform.Config) *Container {
	t.Helper()

	container, err := NewContainer(cfg)
	if err != nil {
		t.Fatalf("building container: %v", err)
	}
	t.Cleanup(func() {
		if err := platform.CloseDatabase(container.DB); err != nil {
			t.Errorf("closing database: %v", err)
		}
	})
	return container
}

type apiCall struct {
	status int
	body   response.APIResponse
	raw    string
	header http.Header
}

func call(t *testing.T, r *gin.Engine, method, path, body, bearer string) apiCall {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	result := apiCall{status: w.Code, raw: w.Body.String(), header: w.Header()}
	if result.raw != "" {
		if err := json.Unmarshal(w.Body.Bytes(), &result.body); err != nil {
			t.Fatalf("%s %s: unmarshalling %q: %v", method, path, result.raw, err)
		}
	}
	return result
}

// authTokens is the token pair returned by login and refresh.
type authTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func decodeTokens(t *testing.T, c apiCall) authTokens {
	t.Helper()

	raw, err := json.Marshal(c.body.Data)
	if err != nil {
		t.Fatalf("re-marshalling data: %v", err)
	}
	var tokens authTokens
	if err := json.Unmarshal(raw, &tokens); err != nil {
		t.Fatalf("unmarshalling tokens from %q: %v", string(raw), err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("expected both tokens to be set, got %+v", tokens)
	}
	return tokens
}

// TestAuthFlow walks the whole authentication lifecycle against a real database:
// register, login, access a protected route, rotate, and log out.
func TestAuthFlow(t *testing.T) {
	container := newTestContainer(t, newTestConfig(t))
	r := container.Router

	const (
		email    = "flow@example.com"
		password = "supersecret123"
		regBody  = `{"email":"flow@example.com","password":"supersecret123","name":"Flow User"}`
	)
	loginBody := `{"email":"` + email + `","password":"` + password + `"}`

	// Register
	got := call(t, r, http.MethodPost, "/api/auth/register", regBody, "")
	if got.status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d (%s)", got.status, got.raw)
	}
	if got.header.Get(middleware.RequestIDHeader) == "" {
		t.Error("expected every response to carry a request ID header")
	}

	// Registering the same email again must conflict, not create a second account.
	got = call(t, r, http.MethodPost, "/api/auth/register", regBody, "")
	if got.status != http.StatusConflict {
		t.Errorf("duplicate register: expected 409, got %d (%s)", got.status, got.raw)
	}

	// Wrong password
	got = call(t, r, http.MethodPost, "/api/auth/login", `{"email":"`+email+`","password":"wrongpassword"}`, "")
	if got.status != http.StatusUnauthorized {
		t.Errorf("bad password: expected 401, got %d (%s)", got.status, got.raw)
	}

	// Unknown email must answer exactly like a wrong password, so the response
	// cannot be used to discover which addresses have accounts.
	unknown := call(t, r, http.MethodPost, "/api/auth/login", `{"email":"nobody@example.com","password":"supersecret123"}`, "")
	if unknown.status != http.StatusUnauthorized {
		t.Errorf("unknown email: expected 401, got %d (%s)", unknown.status, unknown.raw)
	}
	if unknown.body.Message != got.body.Message {
		t.Errorf("login error messages differ between unknown email (%q) and wrong password (%q), which enables account enumeration",
			unknown.body.Message, got.body.Message)
	}

	// Login
	got = call(t, r, http.MethodPost, "/api/auth/login", loginBody, "")
	if got.status != http.StatusOK {
		t.Fatalf("login: expected 200, got %d (%s)", got.status, got.raw)
	}
	tokens := decodeTokens(t, got)

	// Protected route without a token
	got = call(t, r, http.MethodGet, "/api/auth/me", "", "")
	if got.status != http.StatusUnauthorized {
		t.Errorf("me without token: expected 401, got %d (%s)", got.status, got.raw)
	}

	// Protected route with the access token
	got = call(t, r, http.MethodGet, "/api/auth/me", "", tokens.AccessToken)
	if got.status != http.StatusOK {
		t.Fatalf("me: expected 200, got %d (%s)", got.status, got.raw)
	}
	if data, ok := got.body.Data.(map[string]any); !ok || data["email"] != email {
		t.Errorf("me: expected email %q in the payload, got %v", email, got.body.Data)
	}

	// Refresh rotates both tokens
	got = call(t, r, http.MethodPost, "/api/auth/refresh", `{"refreshToken":"`+tokens.RefreshToken+`"}`, "")
	if got.status != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d (%s)", got.status, got.raw)
	}
	rotated := decodeTokens(t, got)
	if rotated.RefreshToken == tokens.RefreshToken {
		t.Error("refresh returned the same refresh token; rotation did not happen")
	}

	// The rotated token works for protected routes.
	got = call(t, r, http.MethodGet, "/api/auth/me", "", rotated.AccessToken)
	if got.status != http.StatusOK {
		t.Errorf("me with rotated access token: expected 200, got %d (%s)", got.status, got.raw)
	}

	// Logout revokes everything.
	got = call(t, r, http.MethodPost, "/api/auth/logout", "", rotated.AccessToken)
	if got.status != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d (%s)", got.status, got.raw)
	}
	got = call(t, r, http.MethodPost, "/api/auth/refresh", `{"refreshToken":"`+rotated.RefreshToken+`"}`, "")
	if got.status != http.StatusUnauthorized {
		t.Errorf("refresh after logout: expected 401, got %d (%s)", got.status, got.raw)
	}
}

// TestRefreshTokenReuseRevokesFamily covers the theft case: replaying a token that
// was already rotated must invalidate every token the user holds.
func TestRefreshTokenReuseRevokesFamily(t *testing.T) {
	container := newTestContainer(t, newTestConfig(t))
	r := container.Router

	call(t, r, http.MethodPost, "/api/auth/register",
		`{"email":"reuse@example.com","password":"supersecret123","name":"Reuse"}`, "")
	login := call(t, r, http.MethodPost, "/api/auth/login",
		`{"email":"reuse@example.com","password":"supersecret123"}`, "")
	original := decodeTokens(t, login)

	rotated := decodeTokens(t, call(t, r, http.MethodPost, "/api/auth/refresh",
		`{"refreshToken":"`+original.RefreshToken+`"}`, ""))

	// Replay the consumed token.
	replay := call(t, r, http.MethodPost, "/api/auth/refresh",
		`{"refreshToken":"`+original.RefreshToken+`"}`, "")
	if replay.status != http.StatusUnauthorized {
		t.Fatalf("replayed token: expected 401, got %d (%s)", replay.status, replay.raw)
	}

	// The legitimate rotated token must also be dead now — the family was revoked.
	after := call(t, r, http.MethodPost, "/api/auth/refresh",
		`{"refreshToken":"`+rotated.RefreshToken+`"}`, "")
	if after.status != http.StatusUnauthorized {
		t.Errorf("expected the whole token family revoked after a replay, got %d (%s)", after.status, after.raw)
	}
}

// TestRefreshTokensAreStoredHashed asserts the database never holds a usable token.
func TestRefreshTokensAreStoredHashed(t *testing.T) {
	container := newTestContainer(t, newTestConfig(t))
	r := container.Router

	call(t, r, http.MethodPost, "/api/auth/register",
		`{"email":"hash@example.com","password":"supersecret123","name":"Hash"}`, "")
	tokens := decodeTokens(t, call(t, r, http.MethodPost, "/api/auth/login",
		`{"email":"hash@example.com","password":"supersecret123"}`, ""))

	var stored []entity.RefreshTokenEntity
	if err := container.DB.Find(&stored).Error; err != nil {
		t.Fatalf("reading refresh tokens: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected exactly 1 stored token, got %d", len(stored))
	}

	row := stored[0]
	if row.TokenHash == tokens.RefreshToken {
		t.Error("the raw refresh token was written to the database")
	}
	if row.TokenHash != tokenSvc.HashToken(tokens.RefreshToken) {
		t.Errorf("stored value is not the SHA-256 hash of the issued token: %q", row.TokenHash)
	}
	if strings.Contains(row.TokenHash, ".") {
		t.Errorf("stored value looks like a JWT rather than a digest: %q", row.TokenHash)
	}
}

// TestConcurrentLoginsIssueDistinctTokens guards the unique token_hash index:
// logins within the same second must still produce different tokens.
func TestConcurrentLoginsIssueDistinctTokens(t *testing.T) {
	container := newTestContainer(t, newTestConfig(t))
	r := container.Router

	call(t, r, http.MethodPost, "/api/auth/register",
		`{"email":"multi@example.com","password":"supersecret123","name":"Multi"}`, "")

	const loginBody = `{"email":"multi@example.com","password":"supersecret123"}`
	seen := make(map[string]bool)

	for i := 0; i < 3; i++ {
		got := call(t, r, http.MethodPost, "/api/auth/login", loginBody, "")
		if got.status != http.StatusOK {
			t.Fatalf("login %d: expected 200, got %d (%s)", i, got.status, got.raw)
		}
		tokens := decodeTokens(t, got)
		if seen[tokens.RefreshToken] {
			t.Fatalf("login %d reissued an identical refresh token", i)
		}
		seen[tokens.RefreshToken] = true
	}
}

func TestHealthEndpoints(t *testing.T) {
	container := newTestContainer(t, newTestConfig(t))
	r := container.Router

	live := call(t, r, http.MethodGet, "/api/health", "", "")
	if live.status != http.StatusOK {
		t.Errorf("liveness: expected 200, got %d (%s)", live.status, live.raw)
	}

	ready := call(t, r, http.MethodGet, "/api/health/ready", "", "")
	if ready.status != http.StatusOK {
		t.Fatalf("readiness: expected 200, got %d (%s)", ready.status, ready.raw)
	}
	if !strings.Contains(ready.raw, `"database"`) {
		t.Errorf("readiness payload should report the database check, got %s", ready.raw)
	}
	if !strings.Contains(ready.raw, `"healthy"`) {
		t.Errorf("readiness payload should mark the database healthy, got %s", ready.raw)
	}
}

// TestReadinessFailsWhenDatabaseIsDown checks readiness reports 503 rather than
// claiming healthy while the database is unreachable.
func TestReadinessFailsWhenDatabaseIsDown(t *testing.T) {
	container := newTestContainer(t, newTestConfig(t))
	r := container.Router

	// Closing the pool makes every subsequent query fail. sql.DB.Close is
	// idempotent, so the cleanup registered by newTestContainer stays harmless.
	if err := platform.CloseDatabase(container.DB); err != nil {
		t.Fatalf("closing database: %v", err)
	}

	ready := call(t, r, http.MethodGet, "/api/health/ready", "", "")
	if ready.status != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when the database is down, got %d (%s)", ready.status, ready.raw)
	}
	if ready.body.Success {
		t.Error("expected success=false when a dependency is unhealthy")
	}

	// Liveness must still pass — the process itself is fine.
	live := call(t, r, http.MethodGet, "/api/health", "", "")
	if live.status != http.StatusOK {
		t.Errorf("liveness should stay 200 when only the database is down, got %d", live.status)
	}
}

func TestAuthEndpointsAreRateLimited(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.RateLimit.LoginAttempts = 2
	cfg.RateLimit.LoginWindow = time.Minute

	container := newTestContainer(t, cfg)
	r := container.Router

	const body = `{"email":"ratelimit@example.com","password":"supersecret123"}`

	for i := 1; i <= 2; i++ {
		got := call(t, r, http.MethodPost, "/api/auth/login", body, "")
		if got.status == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited too early", i)
		}
	}

	got := call(t, r, http.MethodPost, "/api/auth/login", body, "")
	if got.status != http.StatusTooManyRequests {
		t.Errorf("expected 429 once the limit is exceeded, got %d (%s)", got.status, got.raw)
	}

	// Health probes must never be throttled, or an orchestrator would see the
	// instance as down during an attack.
	health := call(t, r, http.MethodGet, "/api/health", "", "")
	if health.status != http.StatusOK {
		t.Errorf("health should not be rate limited, got %d", health.status)
	}
}

func TestNewContainerRejectsUnsafeProductionSecrets(t *testing.T) {
	tests := []struct {
		name           string
		accessSecret   string
		refreshSecret  string
		expectedSubstr string
	}{
		{
			name:           "missing secrets",
			expectedSubstr: "must be set",
		},
		{
			name:           "secrets that are too short",
			accessSecret:   "short",
			refreshSecret:  "also-short",
			expectedSubstr: "at least",
		},
		{
			name:           "identical access and refresh secrets",
			accessSecret:   strings.Repeat("a", 40),
			refreshSecret:  strings.Repeat("a", 40),
			expectedSubstr: "must differ",
		},
		{
			name:           "the built-in dev secrets",
			accessSecret:   devAccessSecret,
			refreshSecret:  devRefreshSecret,
			expectedSubstr: "dev JWT secrets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			cfg.Auth.DevMode = false
			cfg.Auth.JWTAccessSecret = tt.accessSecret
			cfg.Auth.JWTRefreshSecret = tt.refreshSecret

			_, err := NewContainer(cfg)
			if err == nil {
				t.Fatal("expected NewContainer to refuse to start")
			}
			if !strings.Contains(err.Error(), tt.expectedSubstr) {
				t.Errorf("expected the error to mention %q, got %q", tt.expectedSubstr, err)
			}
		})
	}
}

func TestNewContainerAcceptsValidProductionSecrets(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Auth.DevMode = false
	cfg.Auth.JWTAccessSecret = strings.Repeat("a", 40)
	cfg.Auth.JWTRefreshSecret = strings.Repeat("b", 40)

	container := newTestContainer(t, cfg)

	if gin.Mode() != gin.ReleaseMode {
		t.Errorf("expected Gin to run in release mode outside dev, got %q", gin.Mode())
	}
	if container.Router == nil {
		t.Error("expected a router to be wired")
	}
}
