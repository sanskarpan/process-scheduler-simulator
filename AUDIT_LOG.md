# AUDIT LOG

Running audit of Process Scheduler Simulator per REVIEW_PROMPT.md.
All times local. Evidence captured via `go test`, `go vet`, `go build -race`.

## Environment
- Go 1.26.1 darwin/arm64
- Module: github.com/sanskar/scheduler-simulator
- Deps: gorilla/websocket v1.5.3 (only external dep)
- Build: OK. Vet: OK. Tests: PASS (7.9s) at baseline.

## Phase 1 — System Map
Single Go binary (`cmd/server`) serving:
- HTTP static files (`web/static`) at `/`
- WebSocket `/ws` (`web/server.go`)
- Health `/health`
- Simulator engine (`internal/simulator`) runs in a goroutine, broadcasts `SimulationUpdate` to all WS clients.
- Schedulers (`internal/scheduler`): FCFS, SJF, SRTF, RR, Priority (preempt/non-preempt), CFS, MLFQ.
- Process model (`internal/process`): PID, arrival, burst, priority, vruntime, nice, IO bursts (unused).

No DB, no queues, no migrations, no CI, no Docker. Pure in-memory simulator.

## Phase 2 — Startup
- Builds clean. Tests pass. `go vet` clean.
- Port hardcoded `:8082` (ISSUE-014).
- Static path relative `./web/static` (fragile, ISSUE-015).
- No graceful shutdown (ISSUE-013).

## Phase 3 — Static Audit Findings (see ISSUES.md)
- VRuntime mixed atomic/non-atomic (ISSUE-001).
- CFS heap maintained but ignored in Schedule; RemoveProcess index bug (ISSUE-006).
- MLFQ queues + DemoteProcess dead; Schedule misuses Priority field (ISSUE-007).
- RR internal queue + index dead (ISSUE-008).
- IOBursts / SetNice dead features (ISSUE-009).
- Color offset mismatch server vs client (ISSUE-010).
- calculateMetrics writes under RLock (ISSUE-002).
- Reset doesn't stop run goroutine (ISSUE-003).
- sendUpdate spawns unbounded goroutines; callback may block forever (ISSUE-004).
- Step during running double-executes (ISSUE-005).

## Phase 5/9 — Web Layer
- `s.simulator` field unsynchronized (ISSUE-011).
- `clients` map mutated during range under RLock (ISSUE-012).
- Input validation panics on missing/wrong-typed fields (ISSUE-016).
- CheckOrigin always true (ISSUE-017).
- No WS deadlines/ping — stuck clients (ISSUE-018).
- handleInit leaks old simulator goroutine (ISSUE-019).
- XSS via innerHTML in app.js (ISSUE-020).
- No HTTP graceful shutdown (ISSUE-013).

## Phase 4 — Frontend
- Gantt time-marker math broken (ISSUE-021).
- Color offset mismatch (ISSUE-010).
- Button init state not set on load (ISSUE-022).
- No reconnect backoff cap (ISSUE-023).

## Phase 12 — Concurrency
- All ISSUE-001/002/003/004/011/012 are concurrency defects.
- Race test added in TEST_RESULTS.
