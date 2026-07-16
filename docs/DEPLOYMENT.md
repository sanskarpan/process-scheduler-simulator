# Deployment Guide

## Quick start

```bash
git clone https://github.com/sanskar/scheduler-simulator.git
cd scheduler-simulator
make build
./bin/scheduler-server
# → listening on :8082
```

## Docker (recommended)

```bash
# Build and run with docker-compose
docker compose up --build

# Or build and run the image manually
docker build -t scheduler-simulator .
docker run -p 8082:8082 scheduler-simulator
```

The container runs as a non-root user on a read-only filesystem.
`/tmp` is mounted as tmpfs for any temporary writes.

## Environment Variables

All configuration is via environment variables. No config files are required.

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `:8082` | Listen address (`:PORT` or `HOST:PORT`) |
| `STATIC_DIR` | `web/static` | Directory to serve frontend assets from |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `READ_TIMEOUT` | `10s` | HTTP read timeout (e.g. `15s`, `1m`) |
| `WRITE_TIMEOUT` | `30s` | HTTP write timeout |
| `IDLE_TIMEOUT` | `60s` | HTTP idle timeout |
| `SHUTDOWN_TIMEOUT` | `15s` | Graceful-shutdown window |
| `WS_WRITE_WAIT` | `10s` | Per-message WebSocket write deadline |
| `WS_PONG_WAIT` | `60s` | WebSocket pong idle timeout |
| `WS_PING_PERIOD` | `54s` | WebSocket ping interval (must be < `WS_PONG_WAIT`) |
| `ALLOW_LOCAL_ORIGIN` | `true` | Allow `localhost` / `127.0.0.1` WebSocket origins |
| `SIM_CONCURRENCY_LIMIT` | `10` | Max parallel `POST /api/simulate` calls |
| `DEFAULT_TIME_QUANTUM` | `4` | Default time quantum for RR/MLFQ/Lottery/MLQ |
| `DEFAULT_SPEED` | `1` | Default simulation tick speed |
| `STORE_CAPACITY` | `100` | Max simulation results kept in memory |
| `METRICS_ENABLED` | `true` | Expose `/metrics` (Prometheus format) |
| `CORS_ALLOWED_ORIGINS` | _(empty)_ | Comma-separated list of allowed CORS origins |

### Example production overrides

```bash
docker run \
  -p 8082:8082 \
  -e ALLOW_LOCAL_ORIGIN=false \
  -e LOG_LEVEL=warn \
  -e SIM_CONCURRENCY_LIMIT=20 \
  -e STORE_CAPACITY=500 \
  scheduler-simulator
```

## Health Check

The server exposes `GET /health`. It returns:

- `200 OK` — `{"status":"ok"}` when healthy
- `503 Service Unavailable` — `{"status":"degraded","reason":"shutting down"}` during graceful shutdown

The binary also accepts a `--health` self-probe flag used by the Docker
HEALTHCHECK:

```bash
./scheduler-server --health   # exits 0 (healthy) or 1 (unhealthy)
```

## Production Checklist

- [ ] Set `ALLOW_LOCAL_ORIGIN=false` to prevent unauthorized localhost WebSocket upgrades
- [ ] Put the server behind a reverse proxy (nginx, Caddy) for TLS termination
- [ ] Set `LOG_LEVEL=warn` or `error` to reduce log volume
- [ ] Configure `SIM_CONCURRENCY_LIMIT` to match available CPU cores × desired factor
- [ ] Enable log shipping (the logger writes structured JSON to stdout)
- [ ] Verify `/metrics` is not publicly exposed if Prometheus is internal-only

## Reverse Proxy Example (nginx)

```nginx
server {
    listen 443 ssl;
    server_name scheduler.example.com;

    ssl_certificate     /etc/ssl/certs/scheduler.crt;
    ssl_certificate_key /etc/ssl/private/scheduler.key;

    location / {
        proxy_pass http://127.0.0.1:8082;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 3600s;  # keep WebSocket connections alive
    }
}
```

## Graceful Shutdown

The server listens for `SIGINT` and `SIGTERM`. On receipt:

1. HTTP listener is closed (new connections rejected).
2. In-flight WebSocket connections are given `SHUTDOWN_TIMEOUT` to finish.
3. The process exits 0.

Docker's default stop signal is `SIGTERM`, so `docker stop` triggers a clean shutdown.

## Cross-Platform Builds

```bash
make build-all
# Produces:
#   bin/github.com/sanskar/scheduler-simulator-linux-amd64
#   bin/github.com/sanskar/scheduler-simulator-linux-arm64
#   bin/github.com/sanskar/scheduler-simulator-darwin-amd64
#   bin/github.com/sanskar/scheduler-simulator-darwin-arm64
```

Pre-built binaries and a Docker image are published automatically on each
`v*` tag via the Release GitHub Actions workflow.

```bash
docker pull ghcr.io/sanskar/scheduler-simulator:latest
```

## Observability

- **Structured logs** — JSON written to stdout; ingest with any log aggregator.
- **Prometheus metrics** — available at `GET /metrics`; scrape with Prometheus
  or a compatible agent.
- **Request IDs** — every request gets an `X-Request-ID` header (generated or
  echoed from the client) for distributed tracing correlation.
