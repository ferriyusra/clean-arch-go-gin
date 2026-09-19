// Package testutil holds the shared test harness: assertions, an in-memory
// database, and helpers for driving the HTTP layer.
//
// The assertions below are deliberately hand-written rather than pulled from
// testify. They are about thirty lines, they keep the module's test dependency
// count at zero, and they make the mechanics of an assertion visible to anyone
// reading the suite as a worked example.
package testutil

import (
	"errors"
	"reflect"
	"testing"
)

// Equal fails the test when got != want.
func Equal[T comparable](t *testing.T, got, want T, what string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

// DeepEqual compares values that are not comparable with ==, such as slices.
func DeepEqual(t *testing.T, got, want any, what string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", what, got, want)
	}
}

// True fails the test when cond is false.
func True(t *testing.T, cond bool, what string) {
	t.Helper()
	if !cond {
		t.Errorf("%s: expected true", what)
	}
}

// NoError fails the test immediately when err is non-nil.
func NoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Error fails the test when err is nil.
func Error(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", what)
	}
}

// ErrorIs asserts that err matches target anywhere in its chain. Prefer this
// over comparing err.Error() strings: a message is presentation, a sentinel is
// the contract.
func ErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("expected error %v, got %v", target, err)
	}
}
