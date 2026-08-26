# Build stage
FROM golang:1.25-alpine AS builder

# Version metadata, stamped into the binary so the running service can report
# which build it is. Defaults keep a plain `docker build` working.
ARG VERSION=dev
ARG REVISION=""

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary for linux/amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X caltrain-gateway/internal/app/caltrain-gateway.buildVersion=${VERSION} -X caltrain-gateway/internal/app/caltrain-gateway.buildRevision=${REVISION}" \
    -o caltrain-gateway ./cmd/caltrain-gateway

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/caltrain-gateway .

# Expose port 8080
EXPOSE 8080

# Run the binary
ENTRYPOINT ["./caltrain-gateway"]
