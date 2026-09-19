package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// NewEngine returns a gin engine in test mode with no middleware, ready for a
// test to install exactly the chain it wants to exercise.
func NewEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// JSONRequest builds a request whose body is body marshalled as JSON. Pass nil
// for no body. A string or []byte body is sent verbatim, which is how a
// malformed-JSON case is written.
func JSONRequest(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()

	var reader *bytes.Reader
	switch v := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(v))
	case []byte:
		reader = bytes.NewReader(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshalling request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// WithCookie attaches a cookie to a request and returns it, so calls chain.
func WithCookie(req *http.Request, name, value string) *http.Request {
	req.AddCookie(&http.Cookie{Name: name, Value: value})
	return req
}

// Do runs a request against the engine and returns the recorder.
func Do(engine *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// Envelope decodes the standard APIResponse wrapper.
//
// Asserting on decoded fields rather than on raw JSON text means adding a field
// to the envelope does not break every test that touches it.
func Envelope(t *testing.T, rec *httptest.ResponseRecorder) response.APIResponse {
	t.Helper()

	var envelope response.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding response envelope from %q: %v", rec.Body.String(), err)
	}
	return envelope
}

// DataAs decodes the envelope's data field into T.
func DataAs[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var wrapper struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapper); err != nil {
		t.Fatalf("decoding response data from %q: %v", rec.Body.String(), err)
	}
	return wrapper.Data
}

// Cookies returns the response's Set-Cookie entries keyed by name.
func Cookies(rec *httptest.ResponseRecorder) map[string]*http.Cookie {
	cookies := make(map[string]*http.Cookie)
	for _, cookie := range rec.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	return cookies
}
