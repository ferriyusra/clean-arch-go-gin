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
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/server /app/server

# distroless/nonroot runs as uid 65532 with no shell and no package manager.
USER nonroot:nonroot

EXPOSE 8080
ENTRYPOINT ["/app/server"]
