# Stage 1: Build
FROM golang:1.22-alpine AS builder
WORKDIR /app

# Download dependencies first (layer cache)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 go build -o server ./cmd/server

# Stage 2: Runtime image
FROM alpine:3.19
WORKDIR /app

# Copy binary and migrations
COPY --from=builder /app/server .
COPY migrations/ migrations/

EXPOSE 8080
CMD ["./server"]
