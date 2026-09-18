package user

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// storedUser builds a user row whose password hash matches password.
func storedUser(t *testing.T, email, password string) *entity.UserEntity {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	testutil.NoError(t, err)

	return &entity.UserEntity{
		ID:       uuid.New(),
		Email:    email,
		Password: hash,
		Name:     "Test User",
	}
}

func TestLogin(t *testing.T) {
	const (
		email    = "test@example.com"
		password = "password123"
	)

	tests := []struct {
		name    string
		expect  func(deps *testDeps)
		wantErr error
	}{
		{
			name: "authenticates and issues tokens",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), email).
					Return(storedUser(t, email, password), nil)
				deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "rejects an unknown email",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), email).Return(nil, nil)
			},
			wantErr: apperr.ErrInvalidCredentials,
		},
		{
			name: "rejects a wrong password",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), email).
					Return(storedUser(t, email, "a-different-password"), nil)
			},
			wantErr: apperr.ErrInvalidCredentials,
		},
		{
			name: "reports a lookup failure as internal, not as bad credentials",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), email).
					Return(nil, errors.New("database error"))
			},
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			tt.expect(deps)

			result, err := deps.service.Login(context.Background(), &request.LoginRequest{
				Email:    email,
				Password: password,
			})

			if tt.wantErr != nil {
				testutil.ErrorIs(t, err, tt.wantErr)
				if result != nil {
					t.Errorf("expected nil result on error, got %+v", result)
				}
				return
			}

			testutil.NoError(t, err)
			if result == nil {
				t.Fatalf("expected a result")
			}
			testutil.Equal(t, result.User.Email, email, "email")
			testutil.True(t, result.AccessToken != "", "access token is issued")
			testutil.True(t, result.RefreshToken != "", "refresh token is issued")
		})
	}
}

// TestLoginDoesNotDistinguishUnknownEmailFromWrongPassword guards a security
// property that is easy to break by "improving" the error messages: if the two
// cases were distinguishable, the endpoint would become a user enumerator.
func TestLoginDoesNotDistinguishUnknownEmailFromWrongPassword(t *testing.T) {
	const email = "test@example.com"

	unknown := newTestDeps(t)
	unknown.users.EXPECT().FindByEmail(gomock.Any(), email).Return(nil, nil)
	_, unknownErr := unknown.service.Login(context.Background(),
		&request.LoginRequest{Email: email, Password: "password123"})

	wrongPassword := newTestDeps(t)
	wrongPassword.users.EXPECT().FindByEmail(gomock.Any(), email).
		Return(storedUser(t, email, "the-real-password"), nil)
	_, wrongPasswordErr := wrongPassword.service.Login(context.Background(),
		&request.LoginRequest{Email: email, Password: "password123"})

	testutil.Equal(t, apperr.ClientMessage(unknownErr), apperr.ClientMessage(wrongPasswordErr),
		"client-visible message for unknown email vs wrong password")
	testutil.Equal(t, apperr.HTTPStatus(unknownErr), apperr.HTTPStatus(wrongPasswordErr),
		"status for unknown email vs wrong password")
}

func TestLoginContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := deps.service.Login(ctx, &request.LoginRequest{
		Email: "test@example.com", Password: "password123",
	})

	testutil.ErrorIs(t, err, context.Canceled)
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}
