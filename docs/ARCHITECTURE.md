# Architecture

## Overview

The simulator is a monolithic Go binary that serves two surfaces:

1. **WebSocket endpoint** (`/ws`) — real-time simulation control and state streaming
2. **REST API** (`/api/...`) — stateless simulation: submit config, receive complete result

Both share the same HTTP server started from `cmd/server/main.go`.

```
┌─────────────────────────────────────────────────┐
│  Browser / CLI client                           │
│  WebSocket (/ws)     REST (/api/...)            │
└────────────┬─────────────────┬──────────────────┘
             │                 │
             ▼                 ▼
┌────────────────────────────────────────────────────┐
│  cmd/server/main.go                                │
│  ┌──────────────────┐  ┌─────────────────────────┐ │
│  │  web.Server      │  │  api.Handler            │ │
│  │  (WebSocket hub) │  │  (stateless REST)       │ │
│  └────────┬─────────┘  └──────────┬──────────────┘ │
│           │                       │                 │
│           └────────┬──────────────┘                 │
│                    ▼                                 │
│           ┌────────────────┐                        │
│           │ simulator.Simulator                     │
│           │  • discrete tick loop (goroutine)       │
│           │  • ready/waiting/blocked queues         │
│           │  • Gantt chart builder                  │
│           └────────┬───────┘                        │
│                    │                                │
│           ┌────────▼───────┐                        │
│           │ scheduler.Scheduler (interface)         │
│           │  FCFS / SJF / SRTF / RR / Priority /   │
│           │  CFS / MLFQ / Lottery / MLQ + aging    │
│           └────────────────┘                        │
└─────────────────────────────────────────────────────┘
```

## Package Responsibilities

| Package | Path | Responsibility |
|---------|------|----------------|
| `main` | `cmd/server/` | Wire everything: config, mux, middleware, HTTP server |
| `web` | `web/` | WebSocket hub, message dispatch, shutdown coordination |
| `api` | `internal/api/` | Stateless REST handlers + concurrency semaphore |
| `simulator` | `internal/simulator/` | Tick-based simulation engine, I/O burst queue, Gantt chart |
| `scheduler` | `internal/scheduler/` | All scheduling algorithm implementations |
| `process` | `internal/process/` | `Process` type, I/O burst records, metrics structs |
| `store` | `internal/store/` | In-memory result history (bounded ring buffer) |
| `config` | `internal/config/` | Environment-variable based configuration |
| `middleware` | `internal/middleware/` | Recovery, RequestID, SecureHeaders, CORS, logging |
| `metrics` | `internal/metrics/` | Prometheus-compatible `/metrics` endpoint |
| `logging` | `internal/logging/` | Structured JSON logger (wraps `log/slog`) |
| `version` | `internal/version/` | Build-time version string |

## Goroutine Lifecycle

### WebSocket path

```
HandleWebSocket
  └─ goroutine A: reader loop (blocks on conn.ReadJSON)
       └─ handleMessage → calls Start() → spawns sim goroutine
                        → calls Pause/Resume/Stop/Step/etc.

  broadcast channel write (from sim update callback)
       │
  goroutine B: writer loop (blocks on broadcast receive)
       └─ writes JSON to ws conn

sim goroutine
  └─ run() ticker loop
       └─ executeTimeUnit() every tick
            ├─ tickIOQueue()       — advance I/O waiting
            ├─ checkArrivals()     — move arrived processes to ready queue
            ├─ checkIOBurst()      — trigger I/O for ready processes
            ├─ scheduleNextProcess() — ask scheduler for next CPU holder
            ├─ shouldPreempt()     — optional preemption check
            └─ executeProcess()    — decrement burst, build Gantt entry
       └─ sendUpdate()            — push SimulationUpdate through callback
```

`Server.Shutdown()` closes `s.closed`, which breaks reader/writer loops.
`clientWg.Wait()` then ensures all `HandleWebSocket` goroutines fully exit
before `close(s.broadcast)` is called — preventing a send-on-closed-channel panic.

### REST path

`handleSimulate` runs the full simulation synchronously in the HTTP handler
goroutine (no background goroutines). A semaphore (`simSem chan struct{}`)
limits parallel runs to `SIM_CONCURRENCY_LIMIT` (default: 10); excess requests
receive 503.

## Data Flow: WebSocket Simulation

```
Client                    Server                   Simulator
  │                          │                          │
  │──── {"type":"init"} ────►│                          │
  │                          │ create Scheduler         │
  │                          │ create Simulator ──────► │
  │                          │ SetUpdateCallback ──────►│
  │◄─── SimulationUpdate ────│◄──── callback fires ─────│
  │                          │                          │
  │──── {"type":"start"} ───►│                          │
  │                          │ sim.Start() ────────────►│ (spawns run goroutine)
  │◄─── SimulationUpdate ────│◄──── each tick ──────────│
  │◄─── SimulationUpdate ────│◄──── each tick ──────────│
  │                          │                          │
  │──── {"type":"pause"} ───►│                          │
  │                          │ sim.Pause() ────────────►│ (pauses ticker)
  │◄─── {"type":"success"} ──│                          │
```

## Data Flow: REST Simulation

```
Client                          API Handler
  │                                  │
  │── POST /api/simulate ───────────►│
  │   { algorithm, processes, ... }  │ validate input
  │                                  │ buildScheduler()
  │                                  │ sim := NewSimulator(sched)
  │                                  │ sim.AddProcesses(...)
  │                                  │ run full sim synchronously
  │                                  │ store.Add(result)
  │◄── 200 { simulationId, ... } ────│
  │                                  │
  │── GET /api/simulations/{id} ────►│
  │◄── 200 { full result } ──────────│
```

## State Machine: Simulator

```
          ┌─────────────────────────┐
          │         Idle            │◄──── initial / after Reset
          └───────────┬─────────────┘
                      │ Start()
                      ▼
          ┌─────────────────────────┐
    ┌────►│        Running          │◄──── Resume()
    │     └───────────┬─────────────┘
    │     Pause()     │              │ all processes complete
    │                 ▼              ▼
    │     ┌──────────────────┐  ┌──────────────────┐
    │     │     Paused       │  │    Completed     │
    │     └──────────────────┘  └──────────────────┘
    │                 │ Stop()        │ Stop()
    └─────────────────┴──────────────┘
                      ▼
          ┌─────────────────────────┐
          │        Stopped          │
          └─────────────────────────┘
```

## I/O Burst Simulation

Each process may carry a list of `IOBurst{AfterCPUTime, Duration}` records.
At the start of every tick:

1. `tickIOQueue()` — decrement remaining I/O time for all waiting processes;
   move those that reach 0 back to the ready queue.
2. `checkIOBurst()` — after the current CPU-holder runs, inspect its
   `CPUTimeUsed` against each unstarted `IOBurst`; if the threshold is met,
   move the process to the waiting queue.

Because `tickIOQueue` fires *before* `checkIOBurst`, a process can complete
I/O and be scheduled in the same tick.

## Security Layers

- **SecureHeaders middleware**: X-Content-Type-Options, X-Frame-Options, CSP, Referrer-Policy
- **RequestID middleware**: sanitizes reflected `X-Request-ID` to printable ASCII only (CRLF prevention)
- **CORS**: configurable origin allowlist; local origins controlled by `ALLOW_LOCAL_ORIGIN`
- **Concurrency semaphore**: non-blocking channel gate in `handleSimulate`
- **Read-only filesystem**: `docker-compose.yml` mounts the container FS read-only with `/tmp` as a tmpfs
- **Distroless image**: runtime has no shell or package manager; HEALTHCHECK uses binary self-probe (`--health`)

## Configuration

All tunables are environment variables (see `internal/config/config.go`):

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `:8082` | Listen address |
| `STATIC_DIR` | `web/static` | Frontend asset directory |
| `LOG_LEVEL` | `info` | `debug/info/warn/error` |
| `READ_TIMEOUT` | `10s` | HTTP read timeout |
| `WRITE_TIMEOUT` | `30s` | HTTP write timeout |
| `IDLE_TIMEOUT` | `60s` | HTTP idle timeout |
| `SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown window |
| `WS_WRITE_WAIT` | `10s` | Per-message WebSocket write deadline |
| `WS_PONG_WAIT` | `60s` | WebSocket pong idle timeout |
| `WS_PING_PERIOD` | `54s` | WebSocket ping interval (< pong wait) |
| `ALLOW_LOCAL_ORIGIN` | `true` | Allow localhost WebSocket origins |
| `SIM_CONCURRENCY_LIMIT` | `10` | Max parallel REST simulations |
| `DEFAULT_TIME_QUANTUM` | `4` | Default RR/MLFQ time quantum |
| `DEFAULT_SPEED` | `1` | Default simulation tick rate |
| `STORE_CAPACITY` | `100` | Max stored REST simulation results |
| `METRICS_ENABLED` | `true` | Expose `/metrics` endpoint |
