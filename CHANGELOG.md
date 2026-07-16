# Changelog

All notable changes to this project are documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- `docs/ARCHITECTURE.md` — component diagram, goroutine lifecycles, data flow
- `docs/API.md` — complete WebSocket protocol specification and REST reference
- `docs/ALGORITHMS.md` — all 10 algorithms with complexity and behavioural notes
- `SECURITY.md` — vulnerability disclosure policy and security design notes
- `CHANGELOG.md` — this file

---

## [1.1.0] — 2026-07

### Added
- **I/O burst simulation** — processes can declare `ioBursts` with
  `afterCPUTime` and `duration`; the simulator moves processes between the
  ready and waiting queues automatically each tick.
- **Priority aging** — `NewPrioritySchedulerWithAging` computes effective
  priority statlessly from idle time, preventing starvation without mutable
  state.
- **Lottery scheduling** — proportional-share scheduling with injectable RNG
  for deterministic tests.
- **MLQ (Multi-Level Queue)** — fixed-priority multi-queue with Round Robin
  within each level.
- **Concurrency semaphore** — `SIM_CONCURRENCY_LIMIT` env var caps parallel
  REST simulations; returns 503 when at capacity.
- **`SecureHeaders` middleware** — X-Content-Type-Options, X-Frame-Options,
  CSP, Referrer-Policy on every response.
- **CRLF injection prevention** — `X-Request-ID` reflection sanitized to
  printable ASCII.
- **`--health` self-probe** — binary accepts `--health` flag for distroless
  Docker HEALTHCHECK without wget/curl.
- **`/health` shutdown-aware** — returns 503 during graceful shutdown.
- **`WS_*` timeout configuration** — per-message write deadline, pong wait,
  and ping period are now environment-configurable.
- **`ALLOW_LOCAL_ORIGIN`** — environment flag to disable localhost WebSocket
  origins in production.
- **Release workflow** — `.github/workflows/release.yml` builds cross-platform
  binaries (linux/darwin, amd64/arm64), publishes to GHCR, and creates a
  GitHub Release on `v*` tags.
- **Dependabot** — weekly automated PRs for Go modules and GitHub Actions
  (patch + minor only).
- **Coverage gate in CI** — build fails if total test coverage drops below 70%.
- **`govulncheck` in CI** — vulnerability scan on every push.
- **`cover` and `vuln` Makefile targets**.
- **`web` package tests** — `web/server_test.go` covering WebSocket init, start,
  pause, resume, step, stop, reset, addProcess, getState, speed, and invalid
  messages.

### Changed
- `WriteTimeout` default changed from `0` (disabled) to `30s` for REST security.
- `burstTime` validation: `< 0` changed to `<= 0` (burst of 0 is now rejected
  with "burstTime must be >= 1").
- `NewHandler` now accepts a `concurrencyLimit` fourth parameter.
- Distroless Docker image HEALTHCHECK uses `["/app/server", "--health"]`
  instead of `wget` (which is absent in distroless).
- `docker-compose.yml` healthcheck updated to match.
- golangci-lint pinned to `v2.1.6` in both CI and Makefile.

### Fixed
- `CompletedProces` struct field renamed to `CompletedProcesses` (typo; JSON
  tag was already correct, so the API shape is unchanged).
- Dockerfile `-ldflags` linker flag corrected from `-X main.version` to
  `-X main.buildVersion` (the actual variable name in `cmd/server/main.go`).
- `handleInit` now validates that at least one process is provided *before*
  stopping the old simulator, so a bad `init` message leaves the previous
  simulation running.
- `CalculateMetrics` dead-code method removed from `internal/process/process.go`
  (it was unreachable and computed duplicate metrics).
- `clientWg sync.WaitGroup` added to `web.Server` so `Shutdown()` waits for
  all WebSocket handler goroutines to exit before closing the broadcast channel.
- `buildScheduler` function corrected to pass the time quantum properly to all
  quantum-based schedulers.

---

## [1.0.0] — 2026-05

### Added
- Initial release with FCFS, SJF, SRTF, Round Robin, Priority (preemptive and
  non-preemptive), CFS, and MLFQ scheduling algorithms.
- Real-time WebSocket simulation with start, pause, resume, stop, reset, step,
  and speed controls.
- REST API for stateless batch simulation with full result history.
- Gantt chart generation, per-process metrics (waiting time, turnaround time,
  response time), and aggregate metrics (CPU utilization, throughput).
- Prometheus `/metrics` endpoint.
- Static file server for the browser-based frontend.
- Docker and docker-compose support.
- `golangci-lint` and `go test -race` in CI.
- README, CONTRIBUTING, and LICENSE.

[Unreleased]: https://github.com/sanskar/scheduler-simulator/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/sanskar/scheduler-simulator/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/sanskar/scheduler-simulator/releases/tag/v1.0.0
