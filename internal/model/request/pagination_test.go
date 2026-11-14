package request_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// bindQuery runs the real gin binding path, which is what actually enforces the
// `binding:` tags. Calling Normalized directly would skip them entirely and
// test nothing about the ceilings.
func bindQuery(t *testing.T, query string) (request.Pagination, error) {
	t.Helper()

	var page request.Pagination
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+query, nil)

	return page, c.ShouldBindQuery(&page)
}

// TestTheLimitCeilingMatchesItsConstant exists because a Go struct tag cannot
// reference a constant: `max=100` and request.MaxLimit are written twice and
// nothing keeps them in step. Change MaxLimit alone and the real ceiling
// silently stays where it was. This test fails the moment they disagree.
func TestTheLimitCeilingMatchesItsConstant(t *testing.T) {
	t.Parallel()

	if _, err := bindQuery(t, fmt.Sprintf("limit=%d", request.MaxLimit)); err != nil {
		t.Errorf("limit=%d (MaxLimit) must be accepted, got: %v", request.MaxLimit, err)
	}
	if _, err := bindQuery(t, fmt.Sprintf("limit=%d", request.MaxLimit+1)); err == nil {
		t.Errorf("limit=%d (MaxLimit+1) must be rejected, it was accepted", request.MaxLimit+1)
	}
}

// TestThePageCeilingMatchesItsConstant is the same guard on the other field.
func TestThePageCeilingMatchesItsConstant(t *testing.T) {
	t.Parallel()

	if _, err := bindQuery(t, fmt.Sprintf("page=%d", request.MaxPage)); err != nil {
		t.Errorf("page=%d (MaxPage) must be accepted, got: %v", request.MaxPage, err)
	}
	if _, err := bindQuery(t, fmt.Sprintf("page=%d", request.MaxPage+1)); err == nil {
		t.Errorf("page=%d (MaxPage+1) must be rejected, it was accepted", request.MaxPage+1)
	}
}

// TestOffsetNeverGoesNegative pins the reason MaxPage exists.
//
// (page-1)*limit overflows a plain int at large page numbers. GORM only emits
// an OFFSET clause when the value is positive, so a negative offset does not
// error — it silently drops the clause and returns page 1's rows under another
// page's number, which is a wrong answer rather than a loud failure.
func TestOffsetNeverGoesNegative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		page request.Pagination
	}{
		{"the maximum window", request.Pagination{Page: request.MaxPage, Limit: request.MaxLimit}},
		{"a page past the maximum is clamped", request.Pagination{Page: request.MaxPage * 1000, Limit: request.MaxLimit}},
		{"the largest int the type can hold", request.Pagination{Page: int(^uint(0) >> 1), Limit: request.MaxLimit}},
		{"an unbound zero value", request.Pagination{}},
		{"a negative page", request.Pagination{Page: -5, Limit: 10}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if offset := tt.page.Offset(); offset < 0 {
				t.Errorf("Offset() = %d, must never be negative", offset)
			}
		})
	}
}

// TestNormalizedAppliesTheDefaults covers the other half: an absent parameter
// has to mean "default", not "reject", or `GET /resource` would be invalid.
func TestNormalizedAppliesTheDefaults(t *testing.T) {
	t.Parallel()

	got := request.Pagination{}.Normalized()

	testutil.Equal(t, got.Page, request.DefaultPage, "default page")
	testutil.Equal(t, got.Limit, request.DefaultLimit, "default limit")
	testutil.Equal(t, request.Pagination{}.Offset(), 0, "offset of an unbound window")
}
