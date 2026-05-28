# =============================================================================
# Stage 1: Builder
# =============================================================================
# Use a specific Go version and Alpine for a lightweight build environment
FROM golang:1.21-alpine AS builder

# Install required build tools
RUN apk add --no-cache git ca-certificates

WORKDIR /workspace

# Layer Caching: Copy dependency manifests first
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Advanced Build Arguments for Multi-Architecture Support
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Build with static linking and strip debugging symbols to minimize size
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -extldflags '-static'" -o manager main.go

# =============================================================================
# Stage 2: Production Runtime
# =============================================================================
# Distroless images contain no shell, package manager, or standard Linux tools.
FROM gcr.io/distroless/static:nonroot

LABEL maintainer="Mohith Sai"
LABEL org.opencontainers.image.source="https://github.com/aperture-ai/operator"
LABEL org.opencontainers.image.description="Aperture AI Kubernetes Operator"

WORKDIR /

# Copy the statically compiled binary and CA certificates
COPY --from=builder /workspace/manager .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Security: Principle of Least Privilege (uid 65532 is 'nonroot' in distroless)
USER 65532:65532

# Note: Docker HEALTHCHECK is omitted here because distroless has no `curl` or `sh`.
# Production health checks will be managed natively by Kubernetes Liveness/Readiness probes.

ENTRYPOINT ["/manager"]
