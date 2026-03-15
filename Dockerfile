# Stage 1: Build binary
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Fetch dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the static binary with CGO disabled (required for scratch)
# Using -trimpath and ldflags to strip debug info and reduce size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-w -s" \
    -o /floe \
    ./cmd/floe

# Create a non-root user and group
RUN addgroup -g 65534 nobody && \
    adduser -D -u 65534 -G nobody -H -h / nonroot

# Set up data directories with correct permissions
RUN mkdir -p /data/audit /data/vault && \
    chown -R nonroot:nobody /data

# Stage 2: Minimal runtime environment
FROM scratch

# Copy CA certificates so we can make HTTPS requests to OpenAI/Anthropic
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the non-root user information
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

# Copy the statically compiled binary
COPY --from=builder /floe /floe

# Copy the pre-created data directories
COPY --from=builder --chown=nonroot:nobody /data /data

# Run as non-root user
USER nonroot:nobody

# Default environment variables
ENV FLOE_DATA_DIR=/data
ENV FLOE_CONFIG=/data/floe.yaml

WORKDIR /data

# Expose default HTTP gateway port
EXPOSE 4400

# Healthcheck utilizing the built-in demo logic as a rapid ping
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/floe", "version"]

ENTRYPOINT ["/floe"]
CMD ["start"]
