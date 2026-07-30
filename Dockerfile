# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Dependencies are copied first so `go mod download` stays cached until go.mod
# or go.sum actually changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is off so the binary is fully static and can run in a scratch image.
# Note: this selects the pure-Go SQLite driver already used by the project.
# -trimpath and -ldflags="-s -w" drop local paths and debug info from the binary.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Runs as the distroless `nonroot` user (uid 65532), never as root.
COPY --from=builder --chown=nonroot:nonroot /out/server /app/server

EXPOSE 8080

# The readiness probe belongs in the orchestrator (Kubernetes, ECS, compose),
# which can reach GET /api/health/ready — distroless has no shell for HEALTHCHECK.
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
