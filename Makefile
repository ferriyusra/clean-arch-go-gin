# Recipes are POSIX shell. On Windows run make from Git Bash or WSL.
SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

# Host tools are declared in go.mod via the `tool` directive, so a fresh clone
# needs nothing installed beyond Go itself.
MOCKGEN := go tool mockgen
AIR     := go tool air

REPO_INTERFACES_DIR := internal/repository/interfaces
REPO_MOCK_DIR       := internal/repository/mock
SERVICE_DIR         := internal/service
SERVICE_MOCK_DIR    := internal/service/mock

# Detect OS and Architecture
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BINARY_NAME := server
BINARY_PATH := ./bin/$(BINARY_NAME)-$(GOOS)-$(GOARCH)
ifeq ($(GOOS),windows)
	BINARY_PATH := ./bin/$(BINARY_NAME)-$(GOOS)-$(GOARCH).exe
endif

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help install-deps dev server build test test-verbose test-race test-coverage \
        lint fmt vet tidy-check mocks repository-mocks service-mocks verify-mocks \
        docker-build docker-up docker-down clean ci

help:
	@echo "Available commands:"
	@echo "  make install-deps   - Install Go dependencies"
	@echo "  make dev            - Start server with hot-reload"
	@echo "  make server         - Run the server directly"
	@echo "  make build          - Build production binary"
	@echo "  make test           - Run all tests (works without a C toolchain)"
	@echo "  make test-race      - Run all tests with the race detector (needs CGO + gcc/clang)"
	@echo "  make test-coverage  - Run tests and write coverage.html"
	@echo "  make lint           - Run golangci-lint"
	@echo "  make fmt            - Format all Go files"
	@echo "  make vet            - Run go vet"
	@echo "  make mocks          - Regenerate repository and service mocks"
	@echo "  make verify-mocks   - Fail if generated mocks are out of date"
	@echo "  make docker-up      - Start the app plus postgres via docker compose"
	@echo "  make ci             - Everything CI runs, locally"
	@echo "  make clean          - Clean build artifacts"

install-deps:
	go mod download

dev:
	DEV_MODE=true $(AIR)

server:
	DEV_MODE=true go run ./cmd/server

build:
	@echo "Building server binary for $(GOOS)/$(GOARCH) ($(VERSION))..."
	@mkdir -p ./bin
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY_PATH) ./cmd/server
	@echo "Binary created at: $(BINARY_PATH)"

# The race detector needs cgo and a C compiler, which many Windows setups lack.
# Keeping it out of the default target means `make test` is green everywhere,
# while CI still runs `make test-race` on Linux where it is free.
test:
	go test -cover ./...

test-verbose:
	go test -v -cover -failfast ./...

test-race:
	CGO_ENABLED=1 go test -race -cover ./...

test-coverage:
	go test -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy-check:
	go mod tidy
	@git diff --exit-code -- go.mod go.sum \
		|| (echo "go.mod/go.sum are not tidy; run 'go mod tidy'" && exit 1)

mocks: repository-mocks service-mocks

repository-mocks:
	@rm -rf $(REPO_MOCK_DIR)
	@mkdir -p $(REPO_MOCK_DIR)
	@for f in $(REPO_INTERFACES_DIR)/*.go; do \
		base=$$(basename $$f .go); \
		name=$${base%_interface}; \
		echo "Generating repository mock: $$name"; \
		$(MOCKGEN) \
			-source=$$f \
			-destination=$(REPO_MOCK_DIR)/$${name}_mock.go \
			-package=mock \
			-typed; \
	done

# Convention: every service interface lives at
# internal/service/<pkg>/<pkg>.service.go, which is what makes this loop work.
service-mocks:
	@rm -rf $(SERVICE_MOCK_DIR)
	@mkdir -p $(SERVICE_MOCK_DIR)
	@for d in $(SERVICE_DIR)/*/; do \
		pkg=$$(basename $$d); \
		src=$$d$$pkg.service.go; \
		[ -f "$$src" ] || continue; \
		echo "Generating service mock: $$pkg"; \
		$(MOCKGEN) \
			-source=$$src \
			-destination=$(SERVICE_MOCK_DIR)/$${pkg}.service_mock.go \
			-package=mock \
			-typed; \
	done

# Stale mocks are a silent source of green-but-wrong tests, so CI regenerates
# them and fails if anything changed.
verify-mocks: mocks
	@git diff --exit-code -- $(REPO_MOCK_DIR) $(SERVICE_MOCK_DIR) \
		|| (echo "Generated mocks are out of date; run 'make mocks'" && exit 1)

docker-build:
	docker build -t clean-arch-go-gin:$(VERSION) .

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v

ci: tidy-check vet verify-mocks test-race

clean:
	rm -rf ./bin
	rm -f coverage.out coverage.html
	go clean -testcache
