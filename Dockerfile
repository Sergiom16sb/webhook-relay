# syntax=docker/dockerfile:1.7
# Multi-stage build for the webhook relay API.
# Final image is minimal (just the binary + ca-certs).

# ---------- Build stage ----------
FROM golang:1.27-alpine AS build

WORKDIR /src

# Cache go.mod/go.sum download as a separate layer.
COPY go.mod go.sum ./
RUN go mod download

# Build the binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# ---------- Runtime stage ----------
FROM gcr.io/distroless/static-debian12:nonroot

# Copy the binary from the build stage.
COPY --from=build /out/api /api

# Run as non-root (distroless default user is 65532).
USER 65532:65532

# Expose the API port.
EXPOSE 8080

# Healthcheck via /healthz.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/api", "--healthcheck-only"]

# Run the API.
ENTRYPOINT ["/api"]
