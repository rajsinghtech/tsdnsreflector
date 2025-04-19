FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o tsdnsreflector -ldflags="-s -w" .

# Create a minimal runtime image
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/tsdnsreflector .

# Default to standard DNS port, but allow override
ENV PORT="53"

# Dynamically expose the port based on PORT env var
EXPOSE ${PORT}/udp
EXPOSE ${PORT}/tcp

# Run as non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

ENTRYPOINT ["/app/tsdnsreflector"] 