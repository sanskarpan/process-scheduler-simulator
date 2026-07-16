# syntax=docker/dockerfile:1.7
# Multi-stage build for the Process Scheduler Simulator.
# Build context: repo root.

FROM golang:1.23-alpine AS builder
WORKDIR /src

# Cache module downloads separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a stripped, statically-linked binary.
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.buildVersion=${VERSION}" \
    -o /out/server ./cmd/server

# Minimal runtime image — no shell, no package manager, minimal attack surface.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/server /app/server
COPY web/static /app/web/static

# Non-root, read-only filesystem capable.
USER nonroot:nonroot
EXPOSE 8082

# Health check using the binary's built-in --health flag, which performs
# a GET /health HTTP request to localhost. Distroless has no wget/curl,
# so the server binary itself acts as the health-check client.
HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/app/server", "--health"]

ENTRYPOINT ["/app/server"]
