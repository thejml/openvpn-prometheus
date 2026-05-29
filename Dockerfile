# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o openvpn-exporter .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS if needed
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/openvpn-exporter .

# Create a non-root user
RUN addgroup -g 1000 exporter && \
    adduser -D -u 1000 -G exporter exporter

USER exporter

# Expose metrics port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the exporter
CMD ["./openvpn-exporter"]
