package csrf

import (
	"testing"
)

func TestNewCSRFService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		secret string
	}{
		{
			name:   "should create csrf service successfully",
			secret: "test-secret",
		},
		{
			name:   "should return non-nil service with different secret",
			secret: "another-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewCSRFService(tt.secret, testCSRFTTL)

			if service == nil {
				t.Errorf("expected non-nil service, got nil")
			}

			_, ok := service.(*csrfService)
			if !ok {
				t.Errorf("expected *csrfService, got %T", service)
			}
		})
	}
}
