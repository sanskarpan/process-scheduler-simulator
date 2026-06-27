# Process Scheduler Simulator

A production-ready CPU scheduling simulator built in Go with a real-time WebSocket UI and a synchronous REST API. It simulates 10 scheduling algorithms with live Gantt charts, process-state tracking, and comprehensive metrics.

## Features

- **10 Scheduling Algorithms**: FCFS, SJF, SRTF, Round-Robin, Priority (Preemptive & Non-Preemptive), CFS, MLFQ, Lottery (proportional share), and MLQ (fixed multi-level queue)
- **Real-Time Visualization**: WebSocket-based live updates with Gantt charts and process state tracking
- **Synchronous REST API**: `POST /api/simulate` runs a full simulation and returns the final state; `GET /api/simulations` lists recent runs
- **Interactive Web UI**: Dark theme, animations, pause/resume/step/reset controls
- **Comprehensive Metrics**: Turnaround, waiting, response times, CPU utilization, throughput, context switches
- **Production Hardened**: race-detector clean, structured logging (slog), Prometheus metrics, graceful shutdown, input validation, XSS-safe, WebSocket keepalive
- **Configurable**: Environment-driven config (`PORT`, `LOG_LEVEL`, `ENABLE_METRICS`, ...)
- **Container Ready**: Multi-stage Dockerfile, distroless runtime, docker-compose, healthcheck
- **CI**: GitHub Actions (build, vet, lint, race tests, benchmarks, Docker build)

## Quick Start

```bash
# Build and run (defaults to :8082)
make run

# Or use Docker
docker compose up --build
```

Then open http://localhost:8082.

## REST API

### Run a simulation synchronously

```bash
curl -X POST http://localhost:8082/api/simulate \
  -H 'Content-Type: application/json' \
  -d '{
    "algorithm": "rr",
    "timeQuantum": 2,
    "processes": [
      {"pid": 1, "name": "P1", "arrivalTime": 0, "burstTime": 5, "priority": 0},
      {"pid": 2, "name": "P2", "arrivalTime": 0, "burstTime": 4, "priority": 0}
    ]
  }'
```

### List supported algorithms

```bash
curl http://localhost:8082/api/algorithms
```

### List recent simulations

```bash
curl http://localhost:8082/api/simulations
```

### Retrieve a specific simulation

```bash
curl http://localhost:8082/api/simulations/<id>
```

### Health, metrics, version

```bash
curl http://localhost:8082/health
curl http://localhost:8082/metrics   # Prometheus format
curl http://localhost:8082/version
```

Full OpenAPI spec: [`api/openapi.yaml`](api/openapi.yaml).

## Configuration

All configuration is via environment variables. Defaults are production-safe.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8082` | HTTP listen port |
| `STATIC_DIR` | `./web/static` | Static file directory |
| `LOG_LEVEL` | `info` | debug, info, warn, error |
| `LOG_FORMAT` | `json` | json or text |
| `ENABLE_METRICS` | `true` | Expose `/metrics` |
| `ALLOWED_ORIGINS` | (localhost) | Comma-separated CORS/WS origins |
| `DEFAULT_SPEED` | `100` | Default ms per tick |
| `DEFAULT_TIME_QUANTUM` | `4` | Default RR/lottery/mlq quantum |
| `BROADCAST_BUFFER_SIZE` | `64` | WS broadcast channel size |
| `MAX_CLIENTS` | `0` | Max WS clients (0 = unlimited) |
| `WS_READ_LIMIT_BYTES` | `4096` | Max inbound WS message bytes |

## Development

```bash
make ci          # full CI pipeline: build + vet + lint + race tests + benchmarks
make test-race   # tests with race detector
make benchmark   # run benchmarks
make fmt         # format code
make help        # list all targets
```

## Architecture

```
cmd/server/main.go             Entrypoint: config, logging, middleware, graceful shutdown
internal/
  config/                      Env-based config with validation
  logging/                     slog structured logger with request IDs
  metrics/                     Prometheus metrics (HTTP, WS, simulation)
  middleware/                   RequestID, logging, metrics, recovery, CORS
  api/                          REST API: /api/simulate, /api/algorithms, /api/simulations
  store/                        In-memory simulation history
  scheduler/                    10 scheduling-algorithm implementations
  simulator/                    Simulation engine (tick loop, gantt, metrics, snapshots)
  process/                      Process model (PCB, states, events, gantt entries)
  version/                      Build-time version
web/
  server.go                     WebSocket server (broadcast, keepalive, validation)
  static/                       Frontend (index.html, app.js, style.css)
```

### Data flow

```
Browser ──WS──▶ web.Server ──▶ simulator.Engine (tick loop)
                                  │ snapshot (immutable clone)
                                  ▼
                              broadcast channel ──▶ all WS clients ──▶ browser

HTTP client ──REST──▶ api.Handler ──▶ simulator.Engine (max-speed run)
                                      │ final snapshot
                                      ▼
                                  store.Store ──▶ /api/simulations
```

## Scheduling Algorithms

| ID | Name | Preemptive | Notes |
|---|---|---|---|
| `fcfs` | First-Come-First-Served | no | Runs in arrival order |
| `sjf` | Shortest Job First | no | Picks shortest burst |
| `srtf` | Shortest Remaining Time First | yes | Preemptive SJF |
| `rr` | Round-Robin | yes | Time-sliced; needs `timeQuantum` |
| `priority` | Priority (Preemptive) | yes | Preempts on higher-priority arrival |
| `priority_np` | Priority (Non-Preemptive) | no | Runs to completion |
| `cfs` | Completely Fair Scheduler | yes | Linux-like; vruntime + weight |
| `mlfq` | Multi-Level Feedback Queue | yes | Demotes on quantum expiry; per-level quantum (2,4,8) |
| `lottery` | Lottery (Proportional Share) | yes | Random draw by ticket weight |
| `mlq` | Multi-Level Queue (Fixed) | yes | Strict priority between queues; no demotion |

## Testing

- **38+ tests** across config, store, api, scheduler, and simulator packages
- **Race-detector clean** (`go test -race`)
- **E2E harness** exercises all algorithms + malformed input + XSS + shutdown
- **Benchmarks** for scheduler selection, state snapshot, and full simulation

```bash
make test-race
```

## License

MIT
