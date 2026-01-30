# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary for linux/amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o caltrain-gateway ./cmd/caltrain-gateway

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/caltrain-gateway .

# Expose port 8080
EXPOSE 8080

# Run the binary
ENTRYPOINT ["./caltrain-gateway"]
