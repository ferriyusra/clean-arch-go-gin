# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
# The minor version must be at least go.mod's `go` directive. The official
# golang images set GOTOOLCHAIN=local, so an older toolchain refuses the module
# outright instead of downloading a newer one.
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Copy the manifests first so `go mod download` stays cached across source edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=docker
# CGO_ENABLED=0 produces a static binary that runs on a distroless base;
# -trimpath keeps build paths out of the binary so builds are reproducible.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/server ./cmd/server

# ---- runtime stage ---------------------------------------------------------
# Debian 13. The distroless project stopped updating the debian12 tags, and a
# base that no longer gets patches is worse than no base at all.
FROM gcr.io/distroless/static-debian13:nonroot

# `source` is what links a package pushed to ghcr.io back to this repository.
# Without it, GHCR does not link a command-line push to anything. The revision
# and creation time are CI facts, so CI adds them with --label.
ARG VERSION=docker
LABEL org.opencontainers.image.source="https://github.com/ferriyusra/clean-arch-go-gin" \
      org.opencontainers.image.description="Clean Architecture Go API built with Gin and GORM" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

WORKDIR /app
COPY --from=builder /out/server /app/server

# distroless/nonroot runs as uid 65532 with no shell and no package manager.
USER nonroot:nonroot

EXPOSE 8080
ENTRYPOINT ["/app/server"]
