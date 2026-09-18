package middleware_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
)

// newTokenService builds the real token service; it is pure and deterministic,
// so mocking it would test the mock rather than the middleware.
func newTokenService() token.TokenService {
	return token.NewTokenService(token.TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})
}

// decodeJSON decodes an arbitrary response body, for the probe handlers that
// report context state instead of returning the standard envelope.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
}
