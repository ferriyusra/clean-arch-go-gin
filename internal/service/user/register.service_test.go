package user

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/request"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestRegister(t *testing.T) {
	validRequest := &request.RegisterUserRequest{
		Email:    "test@example.com",
		Password: "password123",
		Name:     "Test User",
	}

	tests := []struct {
		name string
		// expect declares the repository interactions this case should produce.
		expect func(deps *testDeps)
		// wantErr is the sentinel the caller must be able to match with
		// errors.Is. nil means the call is expected to succeed.
		wantErr error
	}{
		{
			name: "registers a new user and issues tokens",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).Return(nil, nil)
				deps.users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&uuid.UUID{}, nil)
				deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "rejects an email that is already registered",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).
					Return(&entity.UserEntity{ID: uuid.New(), Email: validRequest.Email}, nil)
			},
			wantErr: apperr.ErrUserAlreadyExists,
		},
		{
			name: "reports a lookup failure as internal, not as a duplicate",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).
					Return(nil, errors.New("database error"))
			},
			wantErr: apperr.ErrInternal,
		},
		{
			name: "reports a create failure as internal",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).Return(nil, nil)
				deps.users.EXPECT().Create(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("create failed"))
			},
			wantErr: apperr.ErrInternal,
		},
		{
			name: "fails when the refresh token cannot be persisted",
			expect: func(deps *testDeps) {
				deps.users.EXPECT().FindByEmail(gomock.Any(), validRequest.Email).Return(nil, nil)
				deps.users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&uuid.UUID{}, nil)
				deps.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
					Return(errors.New("insert failed"))
			},
			wantErr: apperr.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDeps(t)
			tt.expect(deps)

			result, err := deps.service.Register(context.Background(), validRequest)

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
			testutil.Equal(t, result.User.Email, validRequest.Email, "email")
			testutil.Equal(t, result.User.Name, validRequest.Name, "name")
			testutil.True(t, result.AccessToken != "", "access token is issued")
			testutil.True(t, result.RefreshToken != "", "refresh token is issued")
		})
	}
}

// TestRegisterDoesNotLeakInternalDetails pins the behaviour that motivated the
// apperr package: a driver error must never reach the client verbatim.
func TestRegisterDoesNotLeakInternalDetails(t *testing.T) {
	deps := newTestDeps(t)
	deps.users.EXPECT().FindByEmail(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("dial tcp 10.0.0.5:5432: connection refused"))

	_, err := deps.service.Register(context.Background(), &request.RegisterUserRequest{
		Email: "test@example.com", Password: "password123", Name: "Test User",
	})

	testutil.Error(t, err, "register with a broken database")
	testutil.Equal(t, apperr.ClientMessage(err), apperr.ErrInternal.Message, "client-visible message")
	testutil.Equal(t, apperr.HTTPStatus(err), 500, "status")
}

func TestRegisterContextCancellation(t *testing.T) {
	deps := newTestDeps(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := deps.service.Register(ctx, &request.RegisterUserRequest{
		Email: "test@example.com", Password: "password123", Name: "Test User",
	})

	testutil.ErrorIs(t, err, context.Canceled)
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}
