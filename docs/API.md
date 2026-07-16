# API Reference

## Table of Contents

- [REST API](#rest-api)
- [WebSocket Protocol](#websocket-protocol)
- [Data Types](#data-types)
- [Error Handling](#error-handling)

---

## REST API

Base path: `/api`

All request and response bodies are JSON (`Content-Type: application/json`).

### `GET /api/version`

Returns the build version.

**Response 200**
```json
{
  "version": "v1.2.3"
}
```

---

### `GET /api/algorithms`

Returns the list of supported scheduling algorithm identifiers.

**Response 200**
```json
{
  "algorithms": [
    "fcfs", "sjf", "srtf", "rr",
    "priority", "priority_np",
    "cfs", "mlfq", "lottery", "mlq"
  ]
}
```

---

### `POST /api/simulate`

Runs a complete simulation synchronously and stores the result. Returns a
summary plus the simulation ID for later retrieval.

**Concurrent request limit:** configurable via `SIM_CONCURRENCY_LIMIT` (default 10).
Requests over the limit receive `503 Service Unavailable`.

**Request body**
```json
{
  "algorithm":   "rr",
  "timeQuantum": 4,
  "speed":       1,
  "processes": [
    {
      "pid":         1,
      "name":        "P1",
      "arrivalTime": 0,
      "burstTime":   8,
      "priority":    2,
      "ioBursts": [
        { "afterCPUTime": 4, "duration": 3 }
      ]
    }
  ]
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `algorithm` | string | yes | One of the values from `/api/algorithms` |
| `timeQuantum` | int | no | Used by `rr`, `mlfq`, `lottery`, `mlq` (default: 4) |
| `speed` | int | no | Ignored for REST (simulation runs at max speed) |
| `processes` | array | yes | At least 1 process required |
| `processes[].pid` | int | yes | Must be ≥ 0 and unique |
| `processes[].name` | string | no | Defaults to `"P{pid}"` |
| `processes[].arrivalTime` | int | yes | Must be ≥ 0 |
| `processes[].burstTime` | int | yes | Must be ≥ 1 |
| `processes[].priority` | int | no | Lower number = higher priority (default: 0) |
| `processes[].ioBursts` | array | no | I/O interrupts; see below |
| `ioBursts[].afterCPUTime` | int | yes | Trigger after this many CPU ticks used by the process |
| `ioBursts[].duration` | int | yes | Must be ≥ 1; ticks spent in I/O wait |

**Response 200**
```json
{
  "simulationId": "sim_1720000000000",
  "algorithm":    "rr",
  "ganttChart":   [ ... ],
  "processes":    [ ... ],
  "metrics":      { ... }
}
```

See [SimulationResult](#simulationresult) for full field definitions.

**Error responses**

| Status | Condition |
|--------|-----------|
| 400 | Malformed JSON, missing required fields, invalid values |
| 503 | Concurrency limit reached |

---

### `GET /api/simulations`

Lists all stored simulation summaries (most recent first, up to `STORE_CAPACITY`).

**Response 200**
```json
{
  "simulations": [
    {
      "simulationId": "sim_1720000000000",
      "algorithm":    "rr",
      "processCount": 4
    }
  ]
}
```

---

### `GET /api/simulations/{id}`

Returns the full result for a previously run simulation.

**Response 200** — same shape as `POST /api/simulate` response.

**Response 404** — simulation not found.

---

### `GET /health`

Health probe. Returns `200 OK` when the server is healthy; `503` when shutting
down. Used by the Docker `HEALTHCHECK` and load-balancer probes.

**Response 200**
```json
{ "status": "ok" }
```

**Response 503** (during graceful shutdown)
```json
{ "status": "degraded", "reason": "shutting down" }
```

---

### `GET /metrics`

Prometheus-format metrics. Enabled by default; disable with `METRICS_ENABLED=false`.

---

### `GET /version`

Alias for `/api/version` (also accessible at the root path level).

---

## WebSocket Protocol

Connect to `ws://host:port/ws` (or `wss://` in production).

The protocol is **message-pair**: the client sends a command, the server responds
with a `SimulationUpdate` snapshot (for state-changing commands) or an explicit
`success`/`error` frame (for acknowledgements).

### Client → Server Messages

All messages are JSON objects with a `"type"` field.

#### `init`

Initialize a new simulation. Must be sent before any other command.

```json
{
  "type":        "init",
  "algorithm":   "rr",
  "timeQuantum": 4,
  "processes": [
    {
      "pid":         1,
      "name":        "P1",
      "arrivalTime": 0,
      "burstTime":   8,
      "priority":    2,
      "ioBursts": [
        { "afterCPUTime": 4, "duration": 3 }
      ]
    }
  ]
}
```

On success, the server immediately pushes the initial `SimulationUpdate`.

#### `start`

Begin (or restart) the simulation ticker.

```json
{ "type": "start" }
```

#### `pause`

Pause the running simulation. State is preserved.

```json
{ "type": "pause" }
```

#### `resume`

Resume a paused simulation.

```json
{ "type": "resume" }
```

#### `stop`

Stop the simulation permanently. State is not preserved.

```json
{ "type": "stop" }
```

#### `reset`

Reset the simulation back to the initial state (before `start`).

```json
{ "type": "reset" }
```

#### `step`

Advance the simulation by exactly one tick (only valid when paused or idle).

```json
{ "type": "step" }
```

#### `speed`

Change the playback speed while the simulation is running.

```json
{ "type": "speed", "speed": 2 }
```

`speed` is an integer ≥ 1. Higher values run more ticks per second.

#### `addProcess`

Add a process to a running or paused simulation.

```json
{
  "type":    "addProcess",
  "process": {
    "pid":         5,
    "name":        "P5",
    "arrivalTime": 10,
    "burstTime":   6,
    "priority":    1
  }
}
```

#### `getState`

Request the current simulation state without changing anything.

```json
{ "type": "getState" }
```

---

### Server → Client Messages

#### `SimulationUpdate` (no `"type"` field)

Pushed automatically after every tick and after every state-changing command.

```json
{
  "state":            "running",
  "currentTime":      5,
  "currentProcess":   { "pid": 1, "name": "P1", ... },
  "readyQueue":       [ ... ],
  "waitingQueue":     [ ... ],
  "completedProcesses": [ ... ],
  "ganttChart":       [ ... ],
  "events":           [ ... ],
  "metrics":          { ... }
}
```

> **Note:** State update frames do NOT have a `"type"` field. Detect them by
> the absence of `"type"`, or by checking `msg.state !== undefined`.

#### `success`

Acknowledgement for commands that don't trigger a full state push.

```json
{
  "type":    "success",
  "message": "simulation paused"
}
```

#### `error`

Sent when a command fails (invalid state, parse error, unknown type, etc.).

```json
{
  "type":    "error",
  "message": "burstTime must be >= 1"
}
```

---

## Data Types

### `Process`

```json
{
  "pid":            1,
  "name":           "P1",
  "arrivalTime":    0,
  "burstTime":      8,
  "remainingTime":  3,
  "priority":       2,
  "color":          "#4A90D9",
  "state":          "running",
  "waitingTime":    2,
  "turnaroundTime": 11,
  "responseTime":   2,
  "completionTime": 11,
  "cpuTimeUsed":    5
}
```

`state` values: `"ready"`, `"running"`, `"waiting"` (I/O), `"completed"`.

### `GanttEntry`

```json
{
  "pid":   1,
  "name":  "P1",
  "start": 0,
  "end":   4,
  "color": "#4A90D9"
}
```

### `Event`

```json
{
  "time":        4,
  "type":        "io_start",
  "pid":         1,
  "description": "P1 started I/O (duration: 3)"
}
```

Common `type` values: `"arrival"`, `"start"`, `"preempt"`, `"io_start"`,
`"io_complete"`, `"complete"`.

### `SchedulingMetrics`

```json
{
  "averageWaitingTime":    3.5,
  "averageTurnaroundTime": 9.0,
  "averageResponseTime":   2.0,
  "cpuUtilization":        85.0,
  "throughput":            0.5
}
```

### `SimulationResult`

Full result returned by `POST /api/simulate` and `GET /api/simulations/{id}`:

```json
{
  "simulationId":       "sim_1720000000000",
  "algorithm":          "rr",
  "currentTime":        20,
  "state":              "completed",
  "ganttChart":         [ GanttEntry, ... ],
  "readyQueue":         [ Process, ... ],
  "waitingQueue":       [ Process, ... ],
  "completedProcesses": [ Process, ... ],
  "events":             [ Event, ... ],
  "metrics":            SchedulingMetrics
}
```

---

## Error Handling

### REST API

All errors return a JSON object:

```json
{ "error": "human-readable message" }
```

Standard HTTP status codes are used: `400` (client error), `404` (not found),
`503` (capacity), `500` (server bug — should not happen in normal operation).

### WebSocket

Errors are sent as `{ "type": "error", "message": "..." }` frames. The
connection is NOT closed on error — the client may retry the command.

### Common Validation Errors

| Message | Cause |
|---------|-------|
| `"burstTime must be >= 1"` | `burstTime` ≤ 0 |
| `"arrivalTime must be >= 0"` | Negative arrival time |
| `"pid must be >= 0"` | Negative PID |
| `"at least one process is required"` | Empty processes array |
| `"unknown algorithm"` | Unrecognized algorithm string |
| `"Invalid message format: missing 'type'"` | WS message has no `"type"` key |
| `"server busy: too many concurrent simulations"` | Semaphore full (503) |
