# Multi-stage Docker build for the observability-toolkit exporter.
#
# Stage 1 (builder): Compiles the Go binary in a full Go environment.
# Stage 2 (runtime): Copies only the binary into a minimal image.
#
# Why multi-stage? The Go toolchain + module cache is ~1GB. The final
# runtime image is ~15MB (distroless) because it only contains the
# statically-linked binary. This reduces attack surface, speeds up
# pulls, and minimizes CVE exposure from unnecessary packages.

# --- Stage 1: Build ---
FROM golang:1.23-alpine AS builder

# Install git (needed if any dependencies use git-based versioning)
# and ca-certificates (for HTTPS module downloads).
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy dependency files first. Docker caches this layer independently,
# so `go mod download` only re-runs when go.mod/go.sum change — not on
# every source code edit. This dramatically speeds up rebuilds.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build. CGO_ENABLED=0 produces a statically-linked
# binary that runs without glibc — required for scratch/distroless images.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /exporter ./cmd/exporter/

# --- Stage 2: Runtime ---
# Using distroless for minimal attack surface. It contains only the binary
# and CA certificates (needed if the exporter ever makes HTTPS calls).
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /exporter /exporter

# Run as non-root user (UID 65532 in distroless). Never run containers as
# root — it's a security anti-pattern that violates least privilege.
USER nonroot:nonroot

EXPOSE 9090

ENTRYPOINT ["/exporter"]
