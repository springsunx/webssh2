# Multi-stage build for webssh2
# Build the frontend and Go binary within the Docker image.
# Usage:
#   docker build -t webssh2 .
#   docker run -d -p 8888:8888 -e USER=admin -e PASS=password webssh2
#   Or with custom port: -e PORT=8080 -p 8080:8080
# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.24-alpine AS go-builder
WORKDIR /app
# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download
# Copy the rest of the source code
COPY . .
# Copy the built frontend assets from previous stage
COPY --from=frontend-builder /app/public ./public
# Build the Go binary with static linking
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-extldflags -static" -o webssh .

# Stage 3: Final minimal image
FROM alpine:3.20
WORKDIR /webssh
# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata
# Copy the compiled binary and entrypoint script
COPY --from=go-builder /app/webssh .
COPY entrypoint.sh ./
RUN chmod +x entrypoint.sh webssh
# Expose the default port
EXPOSE 8888/tcp
# Set environment variables with defaults
ENV PORT=8888
ENV USER=""
ENV PASS=""
# Use entrypoint script to handle environment variables and run the binary
ENTRYPOINT ["./entrypoint.sh"]