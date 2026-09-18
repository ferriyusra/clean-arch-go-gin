package handler

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

func init() {
	// Report validation failures under the field's JSON name ("email") rather
	// than its Go name ("Email"), so clients can map them back to their payload.
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" || name == "" {
				return fld.Name
			}
			return name
		})
	}
}

// OK writes a success envelope.
func OK(c *gin.Context, status int, message string, data any) {
	c.JSON(status, response.OK(message, data))
}

// Fail is the single place where an error becomes an HTTP response.
//
// The full error chain — including wrapped internal causes — goes to the log;
// the client only ever sees the message carried by a known *apperr.Error, or a
// generic one for anything unrecognised. That is what keeps internal details
// such as driver errors out of response bodies.
func Fail(c *gin.Context, err error) {
	status := apperr.HTTPStatus(err)
	log := logging.FromContext(c.Request.Context())

	attrs := []any{"status", status, "error", err.Error()}
	if appErr, ok := apperr.From(err); ok {
		attrs = append(attrs, "code", string(appErr.Code))
	}

	if status >= http.StatusInternalServerError {
		log.Error("request failed", attrs...)
	} else {
		log.Warn("request rejected", attrs...)
	}

	if fields := apperr.Fields(err); len(fields) > 0 {
		c.AbortWithStatusJSON(status, response.ValidationErr(apperr.ClientMessage(err), fields))
		return
	}
	c.AbortWithStatusJSON(status, response.Err(apperr.ClientMessage(err)))
}

// BindJSON decodes and validates a request body, reporting field-level errors.
// It returns false when a response has already been written.
func BindJSON(c *gin.Context, req any) bool {
	err := c.ShouldBindJSON(req)
	if err == nil {
		return true
	}

	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		Fail(c, apperr.New(apperr.CodeInvalidInput, "Request body is too large").WithCause(err))
		return false
	}

	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		Fail(c, apperr.ErrValidation.WithFields(validationFields(validationErrs)).WithCause(err))
		return false
	}

	Fail(c, apperr.ErrBadRequest.WithCause(err))
	return false
}

// validationFields turns validator's output into the field->message map that
// response.ValidationErr expects.
func validationFields(errs validator.ValidationErrors) map[string]string {
	fields := make(map[string]string, len(errs))
	for _, fe := range errs {
		fields[fe.Field()] = validationMessage(fe)
	}
	return fields
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "This field is required"
	case "email":
		return "Must be a valid email address"
	case "min":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("Must be at least %s characters", fe.Param())
		}
		return fmt.Sprintf("Must be at least %s", fe.Param())
	case "max":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("Must be at most %s characters", fe.Param())
		}
		return fmt.Sprintf("Must be at most %s", fe.Param())
	case "len":
		return fmt.Sprintf("Must be exactly %s characters", fe.Param())
	case "oneof":
		return fmt.Sprintf("Must be one of: %s", strings.ReplaceAll(fe.Param(), " ", ", "))
	case "uuid", "uuid4":
		return "Must be a valid UUID"
	case "url":
		return "Must be a valid URL"
	case "eqfield":
		return fmt.Sprintf("Must match %s", fe.Param())
	default:
		return fmt.Sprintf("Failed the %q rule", fe.Tag())
	}
}
