package response

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// camelCase is the naming rule for every JSON key this API emits: a leading
// lowercase letter, then letters and digits only. It rejects snake_case
// (request_id), PascalCase (RequestId) and kebab-case (request-id).
var camelCase = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// responseTypes lists every struct that can be serialized into a response body.
//
// Go cannot enumerate a package types at run time, so this list is maintained
// by hand. Adding a response type without adding it here means it is not
// covered — the README and CLAUDE.md both say to add it.
func responseTypes() []any {
	return []any{
		APIResponse{},
		Meta{},
		GetUser{},
		LoginResponse{},
		RegisterResponse{},
		RefreshResponse{},
		CSRFTokenResponse{},
		HealthStatus{},
		GetCounter{},
		GetMessage{},
		CommonIDResponse{},
		Session{},
		SessionList{},
		ChangePasswordResponse{},
	}
}

// TestResponseFieldsAreCamelCase keeps the wire format consistent. A single
// snake_case key forces every client to special-case it, and the mistake is
// invisible in review because the Go field name looks fine either way.
func TestResponseFieldsAreCamelCase(t *testing.T) {
	t.Parallel()

	for _, target := range responseTypes() {
		typ := reflect.TypeOf(target)
		t.Run(typ.Name(), func(t *testing.T) {
			t.Parallel()

			assertCamelCaseFields(t, typ)
		})
	}
}

func assertCamelCaseFields(t *testing.T, typ reflect.Type) {
	t.Helper()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		tag, ok := field.Tag.Lookup("json")
		if !ok {
			// An untagged exported field is serialized under its Go name, which
			// is PascalCase and therefore always wrong.
			if field.IsExported() {
				t.Errorf("%s.%s has no json tag, so it would serialize as %q",
					typ.Name(), field.Name, field.Name)
			}
			continue
		}

		name := strings.SplitN(tag, ",", 2)[0]
		// "-" means the field is deliberately never serialized, which is how the
		// auth tokens stay out of response bodies.
		if name == "" || name == "-" {
			continue
		}

		if !camelCase.MatchString(name) {
			t.Errorf("%s.%s is tagged %q; JSON keys must be camelCase",
				typ.Name(), field.Name, name)
		}
	}
}
