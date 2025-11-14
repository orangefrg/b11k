# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o b11k ./cmd

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /build/b11k .

# Copy web templates and static files
COPY --from=builder /build/web ./web

# Copy config.yaml if it exists (user should mount their own config.yaml at runtime)
# Note: config.yaml is typically mounted as a volume, but we copy it here if available
COPY --from=builder /build/config.yaml* ./

# Expose web port (default 8080, configurable via config.yaml)
EXPOSE 8080

# Run the application
CMD ["./b11k"]

