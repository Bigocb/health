# Multi-stage build
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy module files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o health-reporter ./cmd/health-reporter

# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS and procps for pgrep (liveness probe)
RUN apk --no-cache add ca-certificates procps

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/health-reporter /app/health-reporter

# Create non-root user
RUN addgroup -g 1000 health && \
    adduser -D -u 1000 -G health health

USER health

ENTRYPOINT ["/app/health-reporter"]
CMD ["--config", "/etc/health-reporter/config.yaml"]
