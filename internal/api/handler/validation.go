package handler

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// RegisterValidationTagNames makes validation errors report the JSON field name
// instead of the Go struct field name, so clients see "email" rather than "Email".
// Call once during startup.
func RegisterValidationTagNames() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}

	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name == "" {
			return fld.Name
		}
		return name
	})
}

// validationErrors converts a binding error into field -> message pairs.
// Returns nil when the error is not a validation failure (e.g. malformed JSON),
// which the caller should report as a generic bad request instead.
func validationErrors(err error) map[string]string {
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return nil
	}

	out := make(map[string]string, len(verrs))
	for _, fe := range verrs {
		out[fe.Field()] = validationMessage(fe)
	}
	return out
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", fe.Field())
	case "email":
		return "Must be a valid email address"
	case "min":
		return fmt.Sprintf("Must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("Must be at most %s characters", fe.Param())
	default:
		return fmt.Sprintf("Failed the %q rule", fe.Tag())
	}
}
