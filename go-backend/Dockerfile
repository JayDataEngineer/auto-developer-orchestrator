# Go Backend Production Dockerfile
FROM golang:1.24-alpine AS builder

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
FROM node:22-alpine

# Install runtime dependencies for Go binary + Pi agent
RUN apk add --no-cache git openssh-client ca-certificates sqlite

WORKDIR /app

# Copy Go binary from builder
COPY --from=builder /app/main .

# Install Pi coding agent globally
RUN npm install -g @mariozechner/pi-coding-agent@latest

# Create data directory
RUN mkdir -p /data /app/projects

EXPOSE 3847

ENV SERVER_PORT=3847
ENV DATABASE_URL=/data/orchestrator.db

CMD ["./main"]
