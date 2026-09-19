package handler

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// BindPagination reads the `?page=` / `?limit=` window from the query string
// and returns it with the defaults applied. It returns false when a response
// has already been written.
//
// It exists so that a bad window is reported through the same path as any other
// validation failure. `?limit=abc` and `?limit=5000` come back as the standard
// envelope with a field-level `errors` map — the same shape a malformed request
// body produces — rather than as a bare 400 with an empty body that a client
// has no way to parse or explain to a user.
//
// Note ShouldBindQuery, not BindJSON: these are query parameters on a GET,
// which has no body to decode.
func BindPagination(c *gin.Context) (request.Pagination, bool) {
	var page request.Pagination

	err := c.ShouldBindQuery(&page)
	if err == nil {
		return page.Normalized(), true
	}

	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		Fail(c, apperr.ErrValidation.WithFields(validationFields(validationErrs)).WithCause(err))
		return request.Pagination{}, false
	}

	// Anything else is a value that could not be parsed into an int at all
	// ("?page=abc"), which gin reports before validation ever runs.
	Fail(c, apperr.ErrValidation.WithFields(map[string]string{
		"page":  "Must be a positive whole number",
		"limit": "Must be a whole number between 1 and 100",
	}).WithCause(err))
	return request.Pagination{}, false
}

// OKWithMeta writes a success envelope carrying pagination metadata.
//
// The metadata goes in the envelope's own `meta` field rather than inside
// `data`, so a paginated list has the same `data: [...]` shape as any other
// collection and a client does not have to unwrap a different container just
// because a response happens to be paginated.
func OKWithMeta(c *gin.Context, status int, message string, data any, meta *response.Meta) {
	c.JSON(status, response.OKWithMeta(message, data, meta))
}
