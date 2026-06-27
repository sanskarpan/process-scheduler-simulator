# syntax=docker/dockerfile:1.7
# Multi-stage build for the Process Scheduler Simulator.
# Build context: repo root.

FROM golang:1.22-alpine AS builder
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a stripped, statically-linked binary.
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/server ./cmd/server

# Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/server /app/server
COPY web/static /app/web/static

# Non-root, read-only filesystem capable.
USER nonroot:nonroot
EXPOSE 8082
ENTRYPOINT ["/app/server"]
