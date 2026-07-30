# Service Layer - Test-Driven Development Guide

This guide explains how to create and test services using Test-Driven Development (TDD) in this project.

## Overview

The service layer contains all business logic. Each service:
- Accepts request models from the `internal/model/request` package
- Returns response models from the `internal/model/response` package
- Uses repositories for data access
- Is fully tested with unit tests

## TDD Workflow

### Step 1: Define Request Models

Create request models in `internal/model/request/<name>.go`:

```go
package request

type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}
```

### Step 2: Define Response Models

Create response models in `internal/model/response/<name>.go`:

```go
package response

import "github.com/google/uuid"

type CreateUserResponse struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}
```

### Step 3: Write the Test First

Start by creating a test file `<name>.service_test.go` before implementing the service.

Example: `user_service_test.go`

```go
package user

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/repository/mock"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

func TestCreateUser(t *testing.T) {
	tests := []struct {
		name          string
		mockReturn    interface{}
		mockError     error
		expectedError bool
	}{
		{
			name:          "should create user successfully",
			mockReturn:    1,
			mockError:     nil,
			expectedError: false,
		},
		{
			name:          "should return error when repository fails",
			mockReturn:    0,
			mockError:     errors.New("database error"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Setup mock repository
			mockRepo := mock.NewMockUserRepository(ctrl)
			mockRepo.EXPECT().
				CreateUser(gomock.Any(), gomock.Any()).
				Return(tt.mockReturn, tt.mockError).
				Times(1)

			// Create service with mocked repository
			svc := NewUserService(mockRepo)
			
			// Call the method being tested
			result, err := svc.CreateUser(context.Background(), &request.CreateUserRequest{
				Email:    "test@example.com",
				Password: "password123",
				Name:     "Test User",
			})

			// Assert results
			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected non-nil result")
				}
			}
		})
	}
}
```

### Step 4: Create Service Interface

Create the service interface in `<name>.service.go`:

```go
package user

import (
	"context"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/repository/interfaces"
)

// UserService defines the interface for user operations
type UserService interface {
	CreateUser(ctx context.Context, req *request.CreateUserRequest) (*response.CreateUserResponse, error)
}

// userService is the concrete implementation of UserService
type userService struct {
	repo interfaces.UserRepository
}

// NewUserService creates a new instance of UserService
func NewUserService(repo interfaces.UserRepository) UserService {
	return &userService{
		repo: repo,
	}
}
```

### Step 5: Implement the Service Method

Implement the actual service method in `create_user.service.go`:

```go
package user

import (
	"context"

	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/request"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

// CreateUser creates a new user
func (s *userService) CreateUser(ctx context.Context, req *request.CreateUserRequest) (*response.CreateUserResponse, error) {
	// Call repository to persist user
	userID, err := s.repo.CreateUser(ctx, req)
	if err != nil {
		return nil, err
	}

	// Build and return response
	return &response.CreateUserResponse{
		ID:    userID,
		Email: req.Email,
		Name:  req.Name,
	}, nil
}
```

### Step 6: Run Tests

```bash
go test ./internal/service/user -v
```

Expected output:
```
=== RUN   TestCreateUser
=== RUN   TestCreateUser/should_create_user_successfully
--- PASS: TestCreateUser/should_create_user_successfully (0.00s)
=== RUN   TestCreateUser/should_return_error_when_repository_fails
--- PASS: TestCreateUser/should_return_error_when_repository_fails (0.00s)
--- PASS: TestCreateUser (0.00s)
PASS
ok  	github.com/ferriyusra/boilerplate-golang-gin/internal/service/user	0.001s
```

## Best Practices

### 1. Always Use Context

All service methods should accept `context.Context` as the first parameter:

```go
func (s *userService) CreateUser(ctx context.Context, req *request.CreateUserRequest) (*response.CreateUserResponse, error) {
```

### 2. Pass Request Objects, Not Individual Fields

```go
// ✅ Good
func (s *userService) CreateUser(ctx context.Context, req *request.CreateUserRequest) (*response.CreateUserResponse, error)

// ❌ Bad
func (s *userService) CreateUser(ctx context.Context, email, password, name string) (*response.CreateUserResponse, error)
```

### 3. Use Pointers for Responses

Always return pointers to response structs:

```go
// ✅ Good
return &response.CreateUserResponse{...}, nil

// ❌ Bad
return response.CreateUserResponse{...}, nil
```

### 4. Test Error Cases

Include tests for:
- Expected domain failures — assert the exact sentinel with `errors.Is`
- Repository errors — assert that an error surfaces, since the wrapped text is not a contract
- Context cancellation
- Context deadline exceeded

```go
{
	name:          "should handle cancelled context",
	mockError:     context.Canceled,
	expectedError: true,
},
{
	name:          "should handle deadline exceeded",
	mockError:     context.DeadlineExceeded,
	expectedError: true,
},
```

### 5. Use Table-Driven Tests

Organize tests using table-driven patterns for better readability and coverage:

```go
tests := []struct {
	name          string
	mockReturn    interface{}
	mockError     error
	expectedError bool
}{
	{...},
	{...},
}

for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		// test implementation
	})
}
```

### 6. Mock External Dependencies

Always mock repository calls using `gomock`:

```go
mockRepo.EXPECT().
	CreateUser(gomock.Any(), gomock.Any()).
	Return(userID, nil).
	Times(1)
```

### 7. Test with Context Values

Test context propagation to repositories:

```go
func TestCreateUserWithContext(t *testing.T) {
	// Create a context with a value
	ctx := context.WithValue(context.Background(), "key", "value")
	
	// Setup mock to expect this specific context
	mockRepo.EXPECT().
		CreateUser(ctx, gomock.Any()).
		Return(userID, nil)
	
	// Call service
	svc.CreateUser(ctx, req)
}
```

## Example Service Structure

```
internal/service/user/
├── user.service.go              # Interface and constructor
├── user.service_test.go         # Constructor tests
├── create_user.service.go       # Implementation
├── create_user.service_test.go  # Implementation tests
├── get_user.service.go          # Another implementation
└── get_user.service_test.go     # Tests for get_user
```

## Running All Service Tests

```bash
# Run all service tests
go test ./internal/service/... -v

# Run with coverage
go test ./internal/service/... -cover

# Run specific service
go test ./internal/service/user -v

# Run with test output
go test ./internal/service/user -v -run TestCreateUser
```

## Common Patterns

### Pattern 1: Simple CRUD Operation

```go
// Test
mockRepo.EXPECT().GetUserByID(gomock.Any(), userID).Return(&user, nil)

// Implementation
func (s *userService) GetUser(ctx context.Context, id uuid.UUID) (*response.GetUser, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &response.GetUser{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
	}, nil
}
```

### Pattern 2: Error Handling — Sentinels vs Wrapping

This is the contract the whole error path depends on, so it is worth getting right.

Every **expected** failure gets a sentinel error in the package's `errors.go`. Every
**unexpected** failure is wrapped with context. Never return a bare repository error.

```go
// internal/service/user/errors.go
var ErrUserNotFound = errors.New("user not found")

// Implementation
func (s *userService) GetUser(ctx context.Context, id uuid.UUID) (*response.GetUser, error) {
	user, err := s.userRepository.FindByID(ctx, id)
	if err != nil {
		// Unexpected: wrap it. The handler logs this and answers a generic 500.
		return nil, fmt.Errorf("finding user by id: %w", err)
	}
	if user == nil {
		// Expected: a sentinel. The handler maps it to 404 and echoes the message.
		return nil, ErrUserNotFound
	}
	return &response.GetUser{ID: user.ID, Email: user.Email, Name: user.Name}, nil
}
```

Assert with `errors.Is`, not on message strings — wrapping changes the message but
never the identity:

```go
tests := []struct {
	name           string
	mockFindByID   *entity.UserEntity
	mockErr        error
	expectedError  error // the sentinel we expect, if any
	expectAnyError bool  // for wrapped internal failures
}{
	{
		name:          "should return ErrUserNotFound when the user is absent",
		expectedError: ErrUserNotFound,
	},
	{
		name:           "should wrap repository failures",
		mockErr:        errors.New("database error"),
		expectAnyError: true,
	},
}

// ...
if tt.expectedError != nil && !errors.Is(err, tt.expectedError) {
	t.Errorf("expected error %v, got %v", tt.expectedError, err)
}
```

Two rules that keep this honest:

1. **Sentinel messages are shown to clients.** Do not put internal detail in them.
2. **Register every new sentinel** in `serviceErrorStatus` in
   [`api/handler/errors.go`](../api/handler/errors.go), or the handler falls through to
   a 500 for a failure you fully expected.

### Pattern 3: Multiple Repository Calls

```go
// Test
mockRepo.EXPECT().GetUser(gomock.Any(), userID).Return(&user, nil)
mockRepo.EXPECT().UpdateUser(gomock.Any(), &user).Return(nil)

// Implementation
func (s *userService) UpdateUser(ctx context.Context, id uuid.UUID, req *request.UpdateUserRequest) (*response.GetUser, error) {
	user, err := s.repo.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	
	user.Email = req.Email
	user.Name = req.Name
	
	if err := s.repo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	
	return &response.GetUser{...}, nil
}
```

## Troubleshooting

### Mock Not Being Called

Ensure `Times(1)` or `Times(gomock.Any())` matches actual calls:

```go
// If called once
mockRepo.EXPECT().CreateUser(...).Times(1)

// If may not be called
mockRepo.EXPECT().CreateUser(...).Times(0, 1)
```

### Type Mismatch in Tests

Use `gomock.Any()` for flexible matching:

```go
mockRepo.EXPECT().
	CreateUser(gomock.Any(), gomock.Any()).
	Return(userID, nil)
```

### Context Propagation

Always pass context through the call chain:

```go
func (s *userService) CreateUser(ctx context.Context, req *request.CreateUserRequest) error {
	return s.repo.CreateUser(ctx, req)  // ✅ Pass ctx
}
```
