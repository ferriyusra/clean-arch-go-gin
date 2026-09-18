// Package apperr defines the application error type shared by every layer.
//
// Services return *apperr.Error (or wrap an internal cause in one) so that the
// HTTP layer can map a failure to a status code without string matching and
// without leaking internal details to clients.
package apperr

import (
	"context"
	"errors"
	"net/http"
)

// Code is a stable, transport-agnostic classification of a failure.
type Code string

const (
	CodeInvalidInput Code = "INVALID_INPUT"
	CodeUnauthorized Code = "UNAUTHORIZED"
	CodeForbidden    Code = "FORBIDDEN"
	CodeNotFound     Code = "NOT_FOUND"
	CodeConflict     Code = "CONFLICT"
	CodeTimeout      Code = "TIMEOUT"
	CodeUnavailable  Code = "UNAVAILABLE"
	CodeInternal     Code = "INTERNAL"
)

// Error carries a client-safe message plus an optional internal cause.
//
// Message is always safe to return to a client. cause is never returned to a
// client; it exists so the logging middleware can record what actually failed.
type Error struct {
	Code    Code
	Message string
	Fields  map[string]string

	cause error
	base  *Error
}

// New builds a sentinel or ad-hoc error with a client-safe message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap builds an error that hides cause behind a client-safe message.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

// Internal wraps an unexpected failure. The cause is logged, never returned.
func Internal(cause error) *Error {
	return ErrInternal.WithCause(cause)
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.Message + ": " + e.cause.Error()
	}
	return e.Message
}

// Unwrap exposes the internal cause to errors.Is/errors.As.
func (e *Error) Unwrap() error { return e.cause }

// Is lets errors.Is match a derived copy against the sentinel it came from.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e == t || (e.base != nil && e.base == t)
}

// WithCause returns a copy carrying an internal cause. The receiver is
// unchanged, so package-level sentinels stay safe to share.
func (e *Error) WithCause(cause error) *Error {
	c := e.clone()
	c.cause = cause
	return c
}

// WithFields returns a copy carrying field-level validation detail.
func (e *Error) WithFields(fields map[string]string) *Error {
	c := e.clone()
	c.Fields = fields
	return c
}

// WithMessage returns a copy with a different client-safe message.
func (e *Error) WithMessage(message string) *Error {
	c := e.clone()
	c.Message = message
	return c
}

func (e *Error) clone() *Error {
	base := e.base
	if base == nil {
		base = e
	}
	return &Error{
		Code:    e.Code,
		Message: e.Message,
		Fields:  e.Fields,
		cause:   e.cause,
		base:    base,
	}
}

// From extracts the *Error from an error chain, if there is one.
func From(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// HTTPStatus maps any error to the status code the client should see.
func HTTPStatus(err error) int {
	if e, ok := From(err); ok {
		return statusForCode(e.Code)
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout
	}
	return http.StatusInternalServerError
}

// ClientMessage returns a message that is safe to send to a client. Errors that
// are not *Error are treated as internal and deliberately made opaque.
func ClientMessage(err error) string {
	if e, ok := From(err); ok {
		return e.Message
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return ErrTimeout.Message
	case errors.Is(err, context.Canceled):
		return ErrCanceled.Message
	}
	return ErrInternal.Message
}

// Fields returns the field-level detail attached to err, if any.
func Fields(err error) map[string]string {
	if e, ok := From(err); ok {
		return e.Fields
	}
	return nil
}

func statusForCode(c Code) int {
	switch c {
	case CodeInvalidInput:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeTimeout:
		return http.StatusGatewayTimeout
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
