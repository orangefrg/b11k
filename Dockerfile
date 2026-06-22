# Build stage
FROM golang:1.26.4-alpine3.23 AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /build

# Copy go module files first so dependencies are cached
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o b11k ./cmd

# Final stage
FROM alpine:3.23

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates \
    && addgroup -S b11k \
    && adduser -S -D -H -h /app -s /sbin/nologin -G b11k b11k

WORKDIR /app

# Copy the binary from builder
COPY --from=builder --chown=b11k:b11k /build/b11k .

# Copy web templates and static files
COPY --from=builder --chown=b11k:b11k /build/web ./web

# Expose web port (default 8080, configurable via config.yaml)
EXPOSE 8080

USER b11k:b11k

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${B11K_WEB_PORT:-8080}/" >/dev/null || exit 1

# Run the application
CMD ["./b11k"]
