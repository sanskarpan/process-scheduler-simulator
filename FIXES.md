# FIXES

Each fix references the issue ID(s) in ISSUES.md. All changes verified by
`go build`, `go vet`, and `go test -race -count=1 ./...` (all green) plus the
E2E smoke harness described in TEST_RESULTS.md.

---

## FIX-001 / FIX-002 / FIX-004 — Simulator concurrency hardening
- **Issues:** ISSUE-001, ISSUE-002, ISSUE-004
- **Files changed:** `internal/process/process.go`, `internal/simulator/simulator.go`
- **Rationale:** The engine mutated process state under a write lock, but
  `sendUpdate`/`calculateMetrics` ran under a read lock and either mutated
  state (`CalculateMetrics`) or handed shared slices to a goroutine. This was
  a data race and a goroutine-leak vector.
- **Before:** `Process.Execute` used `atomic.AddInt64` on `VRuntime` (mixed
  with plain reads); `calculateMetrics` called `p.CalculateMetrics(t)` under
  RLock; `sendUpdate` did `go s.updateCallback(update)` with shared slices.
- **After:** `VRuntime` is a plain `int64` (all access serialized by the
  engine mutex). `calculateMetrics` is read-only. New `snapshotState()` clones
  gantt/events/ready/current under the read lock and returns an immutable
  `*SimulationUpdate`; `sendUpdate` calls the callback synchronously and
  outside the lock.
- **Validation:** `go test -race -count=1 ./...` clean. New
  `TestConcurrentAccess` hammer-loads `GetCurrentState` while running.

---

## FIX-003 / FIX-005 — Run-loop lifecycle and `Step` semantics
- **Issues:** ISSUE-003, ISSUE-005
- **Files changed:** `internal/simulator/simulator.go`
- **Rationale:** `Reset` did not stop the run goroutine (race + state
  resurrection), and `Step` while running double-counted ticks.
- **Before:** `Reset` cleared state under the lock with the goroutine still
  alive; `Step` fired `stepChan` regardless of state, and the `run` loop's
  `stepChan` case executed a unit even while the ticker was also firing.
- **After:** `Reset` calls `Stop` first and drains `pauseChan`/`stopChan`.
  `Step` is now synchronous: it returns early if running/complete, otherwise
  executes one unit directly and checks completion. The `stepChan` field was
  removed entirely; the `run` loop is driven only by the ticker + pause/resume.
- **Validation:** `TestResetRestoresProcesses`, `TestStepFromIdle`,
  `TestSimulationPauseResume` all pass under `-race`.

---

## FIX-006 / FIX-007 / FIX-008 / FIX-025 — Scheduler correctness cleanup
- **Issues:** ISSUE-006, ISSUE-007, ISSUE-008, ISSUE-025
- **Files changed:** `internal/scheduler/scheduler.go`, `internal/simulator/simulator.go`
- **Rationale:** CFS maintained a dead heap with a buggy `RemoveProcess`; MLFQ
  mutated the user's `Priority` field and returned only the level-0 quantum;
  RR carried dead queue/index state; `ReadyQueue` and `DemoteProcess` were dead
  exported API.
- **Before:** `CFSScheduler` had an `rbTree ProcessHeap` updated but never read
  by `Schedule`; `MLFQScheduler` stored `queues [][]` (dead) and overwrote
  `p.Priority`; `GetTimeQuantum()` returned a single global quantum.
- **After:** Introduced `QuantumFor(p *Process) int` and
  `OnQuantumExpired(p *Process)` on the `Scheduler` interface, replacing
  `GetTimeQuantum()`. Each scheduler is now stateless w.r.t. the ready queue
  (the engine owns it). CFS selects by min-`VRuntime` with a PID tie-break. MLFQ
  tracks level internally by PID in a `map[int]int`, returns per-level quantum
  from `QuantumFor`, and demotes in `OnQuantumExpired`. RR and the dead
  `ReadyQueue`/`DemoteProcess` were removed.
- **Validation:** New `TestSRTFScheduling`, `TestPriorityPreemptive`,
  `TestMLFQDemotion`, `TestRoundRobinQuantum1`, `TestCFSFairness`. E2E
  exercises all 8 algorithms with identical inputs — all complete 4/4.

---

## FIX-010 — Color offset consistency
- **Issues:** ISSUE-010
- **Files changed:** `internal/process/process.go`
- **Before:** server `pid % len`, client `(pid-1) % len`.
- **After:** both use `(pid-1) % len` with a guard for negative PIDs.
- **Validation:** visual + E2E.

---

## FIX-011 / FIX-012 / FIX-016 / FIX-017 / FIX-018 / FIX-019 — Web layer hardening
- **Issues:** ISSUE-011, ISSUE-012, ISSUE-016, ISSUE-017, ISSUE-018, ISSUE-019
- **Files changed:** `web/server.go` (full rewrite), `cmd/server/main.go`
- **Rationale:** The web layer had a data race on `s.simulator`, a write-under-
  RLock on the `clients` map, panic-on-bad-input, an open CORS WebSocket policy,
  no keepalive/deadline (so dead clients leaked FDs forever), and re-init
  leaked the previous engine goroutine.
- **After:**
  - `getSimulator()`/`s.mu` gate all simulator access.
  - `handleBroadcasts` snapshots the client set under RLock, writes outside;
    `unregisterClient` takes the write lock.
  - New `parseProcess` validates every field and returns an error (no panics).
  - `CheckOrigin` allows same-origin + explicit localhost dev, rejects others.
  - `SetReadLimit`, `wsPongWait`/`SetReadDeadline`, `SetPongHandler`, and a
    pinger goroutine reap dead connections within ~60s.
  - `handleInit` calls `old.Stop()` before replacing the simulator.
  - Bounded broadcast channel (64) with non-blocking send → no goroutine leak
    under load.
- **Validation:** E2E sends `{'pid':1}` (missing field), `{}` (missing object),
  unknown message type, XSS-name, and zero-burst — all return graceful JSON
  errors or complete successfully; server stays alive. `go test -race` clean.

---

## FIX-013 / FIX-014 / FIX-015 — Server lifecycle
- **Issues:** ISSUE-013, ISSUE-014, ISSUE-015
- **Files changed:** `cmd/server/main.go`, `web/server.go`
- **After:** SIGINT/SIGTERM → `Server.Shutdown` (closes all WS, stops
  simulator) + `http.Server.Shutdown` with 5s drain. `PORT` env var via
  `DefaultPort`. `STATIC_DIR` env + `filepath.Abs`. `http.Server` has
  `ReadHeaderTimeout`/`ReadTimeout`/`IdleTimeout`.
- **Validation:** E2E confirms "Shutdown signal received... Server stopped".

---

## FIX-020 / FIX-021 / FIX-022 / FIX-023 — Frontend hardening
- **Issues:** ISSUE-020, ISSUE-021, ISSUE-022, ISSUE-023
- **Files changed:** `web/static/app.js`, `web/static/style.css`
- **After:**
  - `escapeHtml()` applied to every user-derived value in every `innerHTML`
    assignment (current process, ready queue, completed, gantt, event log,
    process table, process chips).
  - `num()` helper guards `undefined`/`null` before `.toFixed`.
  - Gantt time markers rewritten: one fixed-width marker per unit, labels at a
    readable step interval; the old `NaN`-producing reduce removed.
  - `updateSimulationState('idle')` called at startup to set initial disabled
    state.
  - WebSocket reconnect uses exponential backoff capped at 30s.
  - Added per-chip remove button (`removeProcess(index)`) and input validation
    in `addProcess`/`initializeSimulator`.
- **Validation:** E2E XSS payload `<img src=x onerror=alert(1)>` as a process
  name completes without executing.

---

## FIX-024 — Zero-burst handling (documented, not changed)
- **Issues:** ISSUE-024
- **Files changed:** none (test added)
- **After:** `TestZeroBurstProcessHandled` confirms a zero-burst process
  completes without hanging. The 1-tick cost is documented as a known modeling
  limitation.
