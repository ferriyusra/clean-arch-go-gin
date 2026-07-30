MOCKGEN := mockgen
REPO_INTERFACES_DIR := internal/repository/interfaces
REPO_MOCK_DIR := internal/repository/mock

# Detect OS and Architecture
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BINARY_NAME := server
BINARY_PATH := ./bin/$(BINARY_NAME)-$(GOOS)-$(GOARCH)
ifeq ($(GOOS),windows)
	BINARY_PATH := ./bin/$(BINARY_NAME)-$(GOOS)-$(GOARCH).exe
endif

.PHONY: help install-deps dev server build \
	test test-verbose test-coverage \
	fmt fmt-check vet lint tidy-check check \
	docker-build docker-up docker-down \
	clean repository-mocks

help:
	@echo "Available commands:"
	@echo "  make install-deps     - Install Go dependencies"
	@echo "  make dev              - Start server with hot-reload (air)"
	@echo "  make server           - Run server directly"
	@echo "  make build            - Build production binary"
	@echo "  make test             - Run all tests (race + coverage)"
	@echo "  make test-verbose     - Run all tests, stop at the first failure"
	@echo "  make test-coverage    - Run tests and write coverage.html"
	@echo "  make fmt              - Format all Go code"
	@echo "  make fmt-check        - Fail if any file is not gofmt-clean"
	@echo "  make vet              - Run go vet"
	@echo "  make lint             - Run golangci-lint"
	@echo "  make tidy-check       - Fail if go.mod/go.sum are not tidy"
	@echo "  make check            - fmt-check + vet + lint + test (what CI runs)"
	@echo "  make docker-build     - Build the production container image"
	@echo "  make docker-up        - Start the API + Postgres stack"
	@echo "  make docker-down      - Stop the stack and remove volumes"
	@echo "  make repository-mocks - Regenerate repository mocks"
	@echo "  make clean            - Clean build artifacts"

install-deps:
	go mod tidy
	go mod download

dev:
	DEV_MODE=true air

server:
	DEV_MODE=true go run ./cmd/server

build:
	@echo "Building server binary for $(GOOS)/$(GOARCH)..."
	@mkdir -p ./bin
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags="-s -w" -o $(BINARY_PATH) ./cmd/server
	@echo "Binary created at: $(BINARY_PATH)"

test:
	go test -v -cover -race ./...

test-verbose:
	go test -v -cover -race -failfast ./...

test-coverage:
	go test -cover -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"

fmt:
	gofmt -w .

# Mirrors the CI formatting gate.
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-clean:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed."; \
		echo "Install it: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run

tidy-check:
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak
	@go mod tidy
	@if ! diff -q go.mod go.mod.bak >/dev/null || ! diff -q go.sum go.sum.bak >/dev/null; then \
		mv go.mod.bak go.mod; mv go.sum.bak go.sum; \
		echo "go.mod/go.sum are not tidy — run 'go mod tidy'"; \
		exit 1; \
	fi
	@rm -f go.mod.bak go.sum.bak

check: fmt-check vet lint test

docker-build:
	docker build -t $(BINARY_NAME):latest .

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v

clean:
	rm -rf ./bin
	rm -f coverage.out coverage.html
	go clean -testcache

repository-mocks:
	@rm -rf $(REPO_MOCK_DIR)
	@mkdir -p $(REPO_MOCK_DIR)
	@for f in $(REPO_INTERFACES_DIR)/*.go; do \
		base=$$(basename $$f .go); \
		name=$${base%_interface}; \
		echo "Generating mock for interface: $$name"; \
		$(MOCKGEN) \
			-source=$$f \
			-destination=$(REPO_MOCK_DIR)/$${name}_mock.go \
			-package=mock; \
	done
