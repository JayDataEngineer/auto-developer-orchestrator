# Go Backend Production Dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/server

# ===========================================
# Final runtime image
# ===========================================
FROM alpine:latest

RUN apk add --no-cache git openssh-client ca-certificates sqlite

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/main .

# Create data directory
RUN mkdir -p /data /app/projects

EXPOSE 3847

ENV SERVER_PORT=3847
ENV DATABASE_URL=/data/orchestrator.db

CMD ["./main"]
