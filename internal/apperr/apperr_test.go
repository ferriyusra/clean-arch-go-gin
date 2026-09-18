package apperr_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestHTTPStatusMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"invalid input", apperr.ErrValidation, http.StatusBadRequest},
		{"unauthorized", apperr.ErrInvalidCredentials, http.StatusUnauthorized},
		{"forbidden", apperr.ErrInvalidCSRFToken, http.StatusForbidden},
		{"not found", apperr.ErrUserNotFound, http.StatusNotFound},
		{"conflict", apperr.ErrUserAlreadyExists, http.StatusConflict},
		{"unavailable", apperr.ErrUnhealthy, http.StatusServiceUnavailable},
		{"internal", apperr.ErrInternal, http.StatusInternalServerError},
		// An error from outside this package is treated as internal, which is
		// what stops an unclassified failure from leaking as a 200 or a 400.
		{"unknown error", errors.New("something"), http.StatusInternalServerError},
		{"deadline exceeded", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"canceled", context.Canceled, http.StatusRequestTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Equal(t, apperr.HTTPStatus(tt.err), tt.want, "status")
		})
	}
}

// TestDerivedErrorsStillMatchTheirSentinel is the property the whole design
// rests on: wrapping a cause must not break errors.Is at the call site.
func TestDerivedErrorsStillMatchTheirSentinel(t *testing.T) {
	cause := errors.New("connection refused")

	withCause := apperr.ErrUserNotFound.WithCause(cause)
	testutil.ErrorIs(t, withCause, apperr.ErrUserNotFound)
	testutil.ErrorIs(t, withCause, cause)

	withFields := apperr.ErrValidation.WithFields(map[string]string{"email": "required"})
	testutil.ErrorIs(t, withFields, apperr.ErrValidation)

	// Distinct sentinels must not collide.
	if errors.Is(withCause, apperr.ErrUserAlreadyExists) {
		t.Errorf("ErrUserNotFound must not match ErrUserAlreadyExists")
	}
}

// TestDerivingDoesNotMutateTheSentinel guards against one request's error
// detail bleeding into another's, since the sentinels are package-level values.
func TestDerivingDoesNotMutateTheSentinel(t *testing.T) {
	_ = apperr.ErrValidation.WithFields(map[string]string{"email": "required"})
	_ = apperr.ErrUserNotFound.WithCause(errors.New("boom"))

	if fields := apperr.Fields(apperr.ErrValidation); len(fields) != 0 {
		t.Errorf("the shared sentinel gained fields: %v", fields)
	}
	testutil.Equal(t, apperr.ErrUserNotFound.Error(), "User not found", "sentinel message")
}

// TestClientMessageHidesInternalDetail is the guarantee that motivated this
// package: a wrapped driver error must never reach a client.
func TestClientMessageHidesInternalDetail(t *testing.T) {
	err := apperr.Internal(fmt.Errorf("finding user by email: %w",
		errors.New("dial tcp 10.0.0.5:5432: connection refused")))

	message := apperr.ClientMessage(err)

	testutil.Equal(t, message, "Internal server error", "client message")
	testutil.True(t, !strings.Contains(message, "10.0.0.5"), "no host in the client message")

	// The detail is still there for the logs.
	testutil.True(t, strings.Contains(err.Error(), "connection refused"), "cause retained for logging")
}

func TestClientMessagePassesThroughKnownErrors(t *testing.T) {
	testutil.Equal(t, apperr.ClientMessage(apperr.ErrInvalidCredentials),
		"Invalid email or password", "sentinel message reaches the client")
	testutil.Equal(t, apperr.ClientMessage(errors.New("raw")),
		apperr.ErrInternal.Message, "unclassified error is made opaque")
}

func TestFieldsAreCarriedThroughTheChain(t *testing.T) {
	err := apperr.ErrValidation.
		WithFields(map[string]string{"email": "Must be a valid email address"}).
		WithCause(errors.New("binding failed"))

	fields := apperr.Fields(err)
	testutil.Equal(t, len(fields), 1, "field count")
	testutil.Equal(t, fields["email"], "Must be a valid email address", "field message")
}

func TestFromExtractsTheApplicationError(t *testing.T) {
	wrapped := fmt.Errorf("layer above: %w", apperr.ErrUserNotFound)

	appErr, ok := apperr.From(wrapped)
	testutil.True(t, ok, "extracted through fmt.Errorf wrapping")
	testutil.Equal(t, appErr.Code, apperr.CodeNotFound, "code")

	if _, ok := apperr.From(errors.New("plain")); ok {
		t.Errorf("a plain error must not be reported as an *apperr.Error")
	}
}
