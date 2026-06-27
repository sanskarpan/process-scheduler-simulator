# ISSUES

All issues found during the audit. IDs are stable references for FIXES.md and
FINAL_REPORT.md. Severity: Critical / High / Medium / Low.

---

## ISSUE-001 — Critical — VRuntime data race (mixed atomic + plain access)
- **Affected components:** `internal/process/process.go`, `internal/simulator/simulator.go`
- **Description:** `Process.VRuntime` was an `int64` updated with
  `atomic.AddInt64` inside `Process.Execute`, but read non-atomically everywhere
  else (`CFSScheduler.Schedule`, `Preempt`, and the per-tick clone in
  `sendUpdate`). Mixing atomic and non-atomic access on the same word is a
  data race per the Go memory model.
- **Root cause:** Misunderstanding of where synchronization happens. All
  `Process` mutation is already serialized by the simulator's `mu`, so the
  atomic was both unnecessary and incorrect (it gave a false sense of safety
  while the reads remained racy).
- **Impact:** Race detector reports a data race under concurrent reads (e.g.
  `GetCurrentState` while the engine is running). On weakly-ordered CPUs the
  read could observe a partially-updated value, producing incorrect CFS
  scheduling decisions.
- **Reproduction:** `go test -race ./internal/simulator/...` against the
  original code with a concurrent `GetCurrentState` loop would flag it.
- **Validation:** Removed `sync/atomic`, made `VRuntime` a plain `int64`.
  `go test -race -count=1 ./...` passes clean.

---

## ISSUE-002 — Critical — `calculateMetrics` wrote to process state under RLock
- **Affected components:** `internal/simulator/simulator.go`
- **Description:** `calculateMetrics` called `p.CalculateMetrics(currentTime)`
  on incomplete processes, which writes `p.WaitingTime`. This ran under the
  read lock in `sendUpdate`, concurrent with the engine's writes under the
  write lock → data race and lost updates.
- **Root cause:** Mixing read-only metrics aggregation with state mutation.
- **Impact:** Race detector failure; waiting-time could be overwritten with a
  stale value, corrupting displayed metrics.
- **Validation:** Removed the mutating call. Waiting time is now maintained
  incrementally and only read by `calculateMetrics`. `-race` clean.

---

## ISSUE-003 — High — `Reset` did not stop the running goroutine
- **Affected components:** `internal/simulator/simulator.go`
- **Description:** `Reset()` cleared state under the lock but never signaled
  the run goroutine to stop. The goroutine kept ticking, racing the reset and
  potentially re-populating `readyQueue`/`ganttChart` from in-flight arrivals.
- **Root cause:** `Reset` and `Stop` shared no shutdown path.
- **Validation:** `Reset` now calls `Stop` first and drains control channels.
  Regression test `TestResetRestoresProcesses` confirms clean reset.

---

## ISSUE-004 — High — Unbounded callback goroutines + shared slices in updates
- **Affected components:** `internal/simulator/simulator.go`
- **Description:** `sendUpdate` did `go s.updateCallback(update)` per tick, and
  passed the *shared* `ganttChart`/`events` slices (not clones) to the
  callback. A slow/blocked consumer (full broadcast channel) → unbounded
  goroutine growth and concurrent read/write on the shared slices.
- **Root cause:** Optimistic assumption that the callback returns quickly.
- **Validation:** `sendUpdate` now calls the callback synchronously (outside
  the lock) with fully cloned slices. The web layer's broadcast send is
  non-blocking with a bounded 64-buffer.

---

## ISSUE-005 — High — `Step` during `running` double-executed time units
- **Affected components:** `internal/simulator/simulator.go`
- **Description:** `Step()` sent to `stepChan` whenever state was running or
  paused. While running, the ticker *and* the step signal both fired
  `executeTimeUnit`, double-counting time.
- **Validation:** `Step` is now synchronous and ignored while running. It
  directly executes a unit when paused/idle. `TestStepFromIdle` covers it.

---

## ISSUE-006 — High — CFS heap was maintained but ignored; `RemoveProcess` index bug
- **Affected components:** `internal/scheduler/scheduler.go`
- **Description:** `CFSScheduler` maintained a `ProcessHeap` via `AddProcess`/
  `RemoveProcess`, but `Schedule` linearly scanned the engine's `readyQueue`
  instead — so the heap was dead state. `RemoveProcess` used a loop index with
  `heap.Remove`, which is invalid after heap reordering and could remove the
  wrong element or panic on out-of-range.
- **Validation:** Removed the dead `ProcessHeap` entirely; CFS now selects
  directly from the engine-owned ready queue (O(n), allocation-free, matches
  the other schedulers). `TestCFSFairness` verifies alternation.

---

## ISSUE-007 — High — MLFQ misused the user's `Priority` field as queue level
- **Affected components:** `internal/scheduler/scheduler.go`
- **Description:** `MLFQScheduler.AddProcess` overwrote `p.Priority = 0` and
  `DemoteProcess` did `p.Priority++`. This (a) destroyed the user-supplied
  priority, (b) coupled scheduler-internal level to a public field, and (c)
  meant MLFQ could not coexist with any priority-based display. The per-queue
  `queues [][]` slices were also dead (Schedule read from `readyQueue`).
  `GetTimeQuantum` returned only the level-0 quantum, so demoted processes
  never got their larger quantum.
- **Validation:** Level is now tracked internally by PID in a `map[int]int`.
  `QuantumFor(p)` returns the per-level quantum. `OnQuantumExpired` demotes.
  User `Priority` is untouched. `TestMLFQDemotion` verifies behavior.

---

## ISSUE-008 — Medium — RR scheduler's internal queue/index was dead state
- **Affected components:** `internal/scheduler/scheduler.go`
- **Description:** `RoundRobinScheduler` stored `queue` and `index` fields
  that were appended to in `AddProcess` but never read by `Schedule` (which
  returned `readyQueue[0]`). The engine owns the ready queue and rotates on
  quantum expiry, so these fields were pure dead state.
- **Validation:** Removed. RR is now stateless aside from the configured
  quantum.

---

## ISSUE-009 — Medium — Dead features: IOBursts, SetNice, LastExecuted, TimeQuantum field
- **Affected components:** `internal/process/process.go`, `internal/simulator/simulator.go`
- **Description:** `IOBursts`, `CurrentIOIndex`, `SetNice`, `Nice`, `Weight`
  (set via `SetNice`), the `TimeQuantum` field on `Process`, and
  `lastGanttUpdate` were never used by the engine. They gave the appearance of
  I/O and CFS-nice support that did not exist.
- **Validation:** Documented as "future" in the report. `Reset` now also
  clears `LastExecuted`/`TimeQuantum`/`CurrentIOIndex` for completeness. No
  behavior change. Left in place to avoid breaking the public struct shape.

---

## ISSUE-010 — Medium — Color offset mismatch between server and client
- **Affected components:** `internal/process/process.go`, `web/static/app.js`
- **Description:** Server `generateColor(pid)` used `pid % len`, client used
  `(pid-1) % len`. A process with PID 1 got color[1] on the server but
  color[0] on the client, so the Gantt chart and the process chips used
  different colors for the same process.
- **Validation:** Both now use `(pid-1) % len`. E2E confirmed consistent
  colors.

---

## ISSUE-011 — Critical — `Server.simulator` field unsynchronized
- **Affected components:** `web/server.go`
- **Description:** `s.simulator` was read and written from multiple goroutines
  (WebSocket handlers, broadcast goroutine, health handler) with no
  synchronization. `handleInit` replaced it while `handleBroadcasts`/`HandleHealth`
  read it.
- **Validation:** All access now goes through `getSimulator()`/`s.mu`. `-race`
  clean on the web E2E.

---

## ISSUE-012 — High — `clients` map mutated under RLock during range
- **Affected components:** `web/server.go`
- **Description:** `handleBroadcasts` did `s.mu.RLock()` then `delete(s.clients,
  client)` inside the loop — a write under a read lock, which is a race and
  could corrupt the map.
- **Validation:** Broadcast now snapshots the client set under RLock, then
  writes; deletion goes through `unregisterClient` which takes the write lock.

---

## ISSUE-013 — High — No graceful HTTP shutdown; `ListenAndServe` blocks forever
- **Affected components:** `cmd/server/main.go`
- **Description:** `http.ListenAndServe` had no signal handling, so SIGINT/
  SIGTERM killed the process immediately, dropping in-flight WebSocket frames
  and not closing the simulator engine.
- **Validation:** Added `signal.Notify`, `Server.Shutdown`, and `http.Server`
  with timeouts. E2E confirms clean "Shutdown signal received... Server stopped".

---

## ISSUE-014 — Medium — Hardcoded port `:8082`
- **Affected components:** `cmd/server/main.go`
- **Description:** Port was hardcoded, preventing deployment on platforms that
  require `PORT` env var (Heroku, Fly, Render, container orchestrators).
- **Validation:** `web.DefaultPort` reads `PORT` env, falls back to `:8082`.

---

## ISSUE-015 — Medium — Static file path relative to CWD
- **Affected components:** `cmd/server/main.go`
- **Description:** `http.Dir("./web/static")` broke if the binary was launched
  from any directory other than the repo root.
- **Validation:** `STATIC_DIR` env override + `filepath.Abs` resolution. Path
  is logged at startup.

---

## ISSUE-016 — Critical — Input validation panics on missing/wrong-typed fields
- **Affected components:** `web/server.go`
- **Description:** `handleInit`/`handleAddProcess` did `pMap["pid"].(float64)`
  unconditionally. A missing or non-numeric field, or a non-object process
  entry, caused a panic → the whole server crashed (no recovery middleware).
- **Validation:** New `parseProcess` validates each field, returns errors.
  E2E with `{'pid':1}` (missing arrivalTime) and `{}` returns graceful JSON
  errors; server survives.

---

## ISSUE-017 — Medium — WebSocket `CheckOrigin` always returns true
- **Affected components:** `web/server.go`
- **Description:** `CheckOrigin: func(r *http.Request) bool { return true }`
  allows any website to open a WebSocket to the simulator, enabling
  cross-site WebSocket hijacking (CSWSH).
- **Validation:** Now allows same-origin and explicit localhost dev.
  Cross-origin requests from arbitrary domains are rejected.

---

## ISSUE-018 — High — No WebSocket deadlines/ping; stuck clients leak
- **Affected components:** `web/server.go`
- **Description:** No read deadline, no ping handler. A client that disappeared
  without sending a Close frame (network drop, laptop closed) left the
  connection and its map entry forever, leaking FDs and memory.
- **Validation:** Added `SetReadLimit`, `wsPongWait`/`SetReadDeadline`,
  `SetPongHandler`, and a pinger goroutine. Dead connections are reaped within
  ~60s.

---

## ISSUE-019 — High — `handleInit` leaked the old simulator's engine goroutine
- **Affected components:** `web/server.go`
- **Description:** Re-initializing a simulator replaced `s.simulator` without
  stopping the old one, leaving its `run()` goroutine ticking forever (one per
  re-init) → goroutine leak and CPU drift.
- **Validation:** `handleInit` now calls `old.Stop()` before replacing. The
  old engine exits via `stopChan`.

---

## ISSUE-020 — High — XSS via `innerHTML` injection of user-controlled names
- **Affected components:** `web/static/app.js`
- **Description:** Process names (and event descriptions, colors, etc.) were
  interpolated directly into `innerHTML`. A name like
  `<img src=x onerror=alert(1)>` executed on every client viewing the
  simulation.
- **Validation:** Added `escapeHtml()` applied to every user-derived value in
  all `innerHTML` assignments. E2E with that exact payload completes without
  executing.

---

## ISSUE-021 — Medium — Broken Gantt time-marker math
- **Affected components:** `web/static/app.js`
- **Description:** The time-marker loop computed `width` from a convoluted
  filter/reduce that produced `NaN` for the first marker and incorrect widths
  thereafter, producing a visually broken axis.
- **Validation:** Rewrote to render one fixed-width marker per time unit with a
  readable step interval.

---

## ISSUE-022 — Low — Buttons not initialized to disabled state on page load
- **Affected components:** `web/static/app.js`
- **Description:** `startBtn` etc. relied on the first server message to set
  their disabled state, leaving them briefly clickable (and erroring) before
  init.
- **Validation:** `updateSimulationState('idle')` called at startup.

---

## ISSUE-023 — Low — Reconnect retry had no backoff cap
- **Affected components:** `web/static/app.js`
- **Description:** `setInterval(connectWebSocket, 3000)` retried every 3s
  forever, even if the server was down for hours.
- **Validation:** Exponential backoff capped at 30s.

---

## ISSUE-024 — Low — Zero-burst process "costs" one time unit
- **Affected components:** `internal/simulator/simulator.go`
- **Description:** A process with `burstTime == 0` arrives, is scheduled, and
  `executeProcess(1)` decrements `RemainingTime` to -1 then clamps to 0 and
  completes — but `currentTime` still advances by 1, so the simulation reports
  `TotalTime=1` for a process that needed no CPU.
- **Why acceptable:** The engine executes in discrete 1-unit ticks; a
  zero-cost process still consumes a tick slot. This is a simulator-level
  modeling choice, not a crash. Documented here as a known limitation.
- **Validation:** `TestZeroBurstProcessHandled` confirms completion without
  hang.

---

## ISSUE-025 — Low — `ReadyQueue` type and `DemoteProcess` were dead exported API
- **Affected components:** `internal/scheduler/scheduler.go`
- **Description:** `ReadyQueue` (a `heap.Interface` type) was defined but never
  used. `MLFQScheduler.DemoteProcess` was exported but never called by the
  engine.
- **Validation:** Both removed. The engine now drives demotion via the
  `OnQuantumExpired` interface method.
