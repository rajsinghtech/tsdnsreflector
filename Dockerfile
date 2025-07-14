# Build stage
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

# Build arguments for cross-compilation
ARG TARGETOS
ARG TARGETARCH

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates git

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with cross-compilation support
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -a -installsuffix cgo -ldflags='-w -s' -o tsdnsreflector ./cmd/tsdnsreflector

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS and DNS resolution
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S tsdns && \
    adduser -u 1001 -S tsdns -G tsdns

# Create directories with proper permissions
RUN mkdir -p /app /var/lib/tailscale && \
    chmod 755 /var/lib/tailscale

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/tsdnsreflector .
COPY --from=builder /app/config.hujson .

# Set ownership for app directory only
RUN chown -R tsdns:tsdns /app

# Don't set USER here as StatefulSet runs as root for DNS port 53

# Expose ports
EXPOSE 53/udp
EXPOSE 8080/tcp
EXPOSE 9090/tcp

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD nc -zu localhost 53 || exit 1

# Set the entrypoint
ENTRYPOINT ["./tsdnsreflector"]

# Default arguments
CMD ["-config", "/app/config.hujson"]