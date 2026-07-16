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

---

## ISSUE-026 — Critical — Three concurrent writers per WebSocket connection with no mutex
- **Affected components:** `web/server.go`
- **Description:** gorilla/websocket requires that at most one goroutine writes
  to a connection at a time. Three goroutines write to every `*websocket.Conn`
  concurrently: (1) `handleBroadcasts` via `WriteJSON`, (2) the pinger goroutine
  via `WriteMessage(PingMessage)`, and (3) the reader-loop goroutine via
  `sendSuccess`/`sendError`/`handleGetState`/`handleInit`. There is no per-
  connection mutex. The comment at line 83 references a `clientConn` type with a
  per-connection mutex that does not exist in the codebase.
- **Root cause:** Structural: the three goroutines were added incrementally
  without adding the required serialization.
- **Impact:** Concurrent writes corrupt the WebSocket frame stream → client
  receives garbled JSON or a protocol error and disconnects. Race detector
  reports data races under load.
- **Reproduction:** Start a simulation with high update rate; connect two browser
  tabs. The `go test -race` run will report concurrent map/slice writes through
  gorilla's internal write buffer under load.
- **Fix:** Wrap each `*websocket.Conn` in a `wsConn` struct that holds a
  `sync.Mutex`. All write calls go through mutex-guarded methods.

---

## ISSUE-027 — Critical — `Reset()` drains `stopChan` before the goroutine exits
- **Affected components:** `internal/simulator/simulator.go`
- **Description:** `Stop()` sends `true` to `stopChan` (non-blocking). `Reset()`
  calls `Stop()` then drains `stopChan` in a `select/default`. If the run
  goroutine has not yet read from `stopChan` when `Reset()` drains it, the
  goroutine never receives the stop signal and keeps running. The next `Start()`
  spawns a second goroutine that races with the first: two goroutines call
  `executeTimeUnit()` per tick, advancing the clock 2 units instead of 1.
- **Root cause:** `Stop()` does not wait for the goroutine to exit before
  returning; `Reset()` incorrectly assumes it does.
- **Impact:** Goroutine leak; double time-unit advancement after Reset+Start;
  data races on shared state.
- **Reproduction:** Call `Start(); time.Sleep(...); Reset(); Start()` in a tight
  loop with `-race`. Detectable by observing `currentTime` advancing faster than
  one unit per tick interval.
- **Fix:** Add a `sync.WaitGroup` to `Simulator`. `run()` calls `wg.Done()` on
  exit. `Stop()` calls `wg.Wait()` after signaling, guaranteeing the goroutine
  is gone before returning.

---

## ISSUE-028 — High — CFS `VRuntime` integer truncation for Weight > 1024
- **Affected components:** `internal/process/process.go:134`
- **Description:** `p.VRuntime += int64(duration * 1024 / p.Weight)` performs
  integer arithmetic before the cast. For `duration=1` and any `Weight > 1024`
  (e.g. nice=-1 → Weight=1280), `1*1024/1280 = 0` in integer arithmetic, so the
  cast yields 0. VRuntime never advances for negatively-niced processes; CFS
  always picks them, starving normal-priority processes.
- **Root cause:** Missing cast before the multiplication: the expression should
  be `int64(duration)*1024/int64(p.Weight)`.
- **Impact:** Processes with nice < 0 (weight > 1024) monopolize the CPU under
  CFS. The corresponding preemption threshold in `CFSScheduler.Preempt` has the
  same bug.
- **Reproduction:** `SetNice(-1)` on a process and run under CFS; observe it
  runs every tick while normal-priority processes starve.
- **Fix:** `p.VRuntime += int64(duration) * 1024 / int64(p.Weight)`. Same fix
  needed in the preempt threshold expression.

---

## ISSUE-029 — High — Dynamically-added process with past `ArrivalTime` never scheduled
- **Affected components:** `internal/simulator/simulator.go:368`
- **Description:** `checkArrivals()` uses strict equality `p.ArrivalTime ==
  s.currentTime`. A process added via the WebSocket `addProcess` message with
  `ArrivalTime < currentTime` (e.g. `ArrivalTime=0` when `currentTime=5`) never
  passes this check and is never enqueued. The simulation idles indefinitely
  waiting for processes that will never arrive.
- **Root cause:** The arrival check was designed for static process sets loaded
  at time 0; it does not handle the dynamic-add case.
- **Impact:** Simulation hangs; the dynamically-added process never runs.
- **Reproduction:** Initialize with no processes, start simulation, then send
  `addProcess` with `arrivalTime=0`. Observe `currentTime` incrementing with
  idle CPU forever.
- **Fix:** Change `p.ArrivalTime == s.currentTime` to `p.ArrivalTime <=
  s.currentTime` so a process added mid-simulation with a past arrival time
  enters the ready queue on the next tick.

---

## ISSUE-030 — High — `handleBroadcasts` goroutine leaks on `Shutdown()`
- **Affected components:** `web/server.go:84`
- **Description:** `handleBroadcasts()` blocks on `for update := range
  s.broadcast`. `Shutdown()` closes `s.closed` and closes client connections but
  never closes `s.broadcast`. The goroutine blocks forever on the receive from
  the open channel.
- **Root cause:** Missing `close(s.broadcast)` in `Shutdown()`.
- **Impact:** One goroutine leaks per server instance; in tests that create many
  servers the leak is a correctness issue and can mask shutdown races.
- **Reproduction:** Create a Server, call Shutdown(), then check for leaked
  goroutines (e.g. `runtime.Stack`).
- **Fix:** Add `close(s.broadcast)` in `Shutdown()`. Guard all send sites with a
  `case <-s.closed` arm to avoid panicking on send-to-closed-channel.

---

## ISSUE-031 — High — CORS middleware sets `Access-Control-Allow-Origin` to a comma-joined list
- **Affected components:** `internal/middleware/middleware.go:114-115`
- **Description:** `allow := strings.Join(allowedOrigins, ", ")` is set as
  `Access-Control-Allow-Origin` in the `else if allow != ""` branch (i.e. when
  the request has no Origin header, or the origin is not in the allowlist). The
  CORS spec requires ACAO to be either `*` or a single origin; a comma-separated
  list is invalid. Browsers reject it. Additionally, this branch leaks the full
  allowlist to requests from unrecognised origins.
- **Root cause:** Attempt to handle "no origin" case by echoing the config,
  which misunderstands the ACAO semantics.
- **Impact:** CORS never works for unrecognised-origin requests (by design) but
  the header is still set, leaking the allowlist. For same-origin requests the
  branch is not taken, so those work correctly.
- **Fix:** Remove the `else if` branch entirely. Only set ACAO when the request
  origin is in the allowlist.

---

## ISSUE-032 — Medium — `scheduleNextProcess` removes from readyQueue by PID, not pointer
- **Affected components:** `internal/simulator/simulator.go:392-395`
- **Description:** After `scheduler.Schedule()` returns the chosen `*Process`,
  the code removes it from `readyQueue` by searching for the first entry with a
  matching PID (`p.PID == next.PID`). When two processes share the same PID, the
  wrong process may be dequeued — either a different process is removed while the
  selected one stays, or the same process is removed twice.
- **Root cause:** PID is used as a unique key when it is only required to be an
  identifier; duplicates are not prevented.
- **Impact:** One process is silently lost from the ready queue; simulation may
  deadlock or complete with wrong results.
- **Fix:** Change the removal loop to compare by pointer: `if p == next`.

---

## ISSUE-033 — Medium — MLFQ/MLQ level map keyed by PID causes corruption with duplicate PIDs
- **Affected components:** `internal/scheduler/scheduler.go:341-349`
- **Description:** `MLFQScheduler.levels` and `MLQScheduler.levels` are
  `map[int]int` keyed by PID. When two processes have the same PID, the second
  process's level entry aliases the first's. `RemoveProcess(p1)` deletes
  `levels[pid]`, which also removes p2's level — the next `QuantumFor(p2)` and
  `Preempt(p2)` calls see level 0 instead of p2's earned demotion, corrupting
  the scheduling order.
- **Root cause:** PID used as a unique key for scheduler-internal state.
- **Impact:** MLFQ demotion history is silently reset for processes sharing a
  PID after any co-PID process completes. Starvation prevention breaks.
- **Fix:** Key the map by `*process.Process` pointer instead of PID.

---

## ISSUE-034 — Medium — CFS `Preempt` off-by-one: `len(readyQueue) <= 1` skips single-competitor case
- **Affected components:** `internal/scheduler/scheduler.go:276`
- **Description:** `CFSScheduler.Preempt` returns `false` immediately when
  `len(readyQueue) <= 1`. When exactly one competing process is in the queue
  (the most common steady-state for two-process workloads), preemption is never
  checked. The correct guard is `len(readyQueue) == 0` (no competitors at all).
- **Root cause:** Off-by-one: the current process has already been removed from
  the ready queue before `Preempt` is called, so `len(readyQueue) == 1` means
  one competitor exists.
- **Impact:** Under CFS with two processes, whichever process runs first is
  never preempted in favour of the other even when its VRuntime far exceeds the
  minimum granularity. Fairness is severely degraded.
- **Fix:** Change `<= 1` to `== 0`.

---

## ISSUE-035 — Medium — `Step()` concurrent callers both execute a time unit
- **Affected components:** `internal/simulator/simulator.go:142`
- **Description:** `Step()` reads the state under the mutex, releases the lock,
  then calls `executeTimeUnit()`. Two goroutines that both read `state ==
  SimStatePaused` will both release the lock and both call `executeTimeUnit()`,
  advancing `currentTime` by 2 instead of 1. The check-and-execute is not
  atomic.
- **Root cause:** The lock is released between the state check and the execution.
- **Impact:** `currentTime` can advance by N for N concurrent Step() calls.
  Metrics and Gantt chart entries are doubled.
- **Fix:** Add a `stepMu sync.Mutex` to `Simulator` and lock it for the full
  duration of `Step()`.

---

## ISSUE-036 — Medium — `LotteryScheduler.Reset()` hardcodes seed `0xC0FFEE` instead of original seed
- **Affected components:** `internal/scheduler/scheduler.go:487-491`
- **Description:** `LotteryScheduler.Reset()` resets the RNG state to the
  hardcoded constant `0xC0FFEE`, not the seed originally passed to `NewRNG()`.
  If the scheduler was constructed with a different seed (e.g. for
  reproducibility testing), `Reset()` produces a different sequence than the
  original run, breaking replay determinism.
- **Root cause:** The original seed is not stored in `deterministicRNG`.
- **Impact:** Lottery simulation replays do not reproduce the same scheduling
  sequence after Reset.
- **Fix:** Store the original seed in `deterministicRNG.seed` and restore it in
  `Reset()`.

---

## ISSUE-037 — Medium — `sendUpdate()` reads `updateCallback` without a lock
- **Affected components:** `internal/simulator/simulator.go:514`
- **Description:** `sendUpdate()` reads `s.updateCallback` without holding any
  lock. `SetUpdateCallback()` writes it under `s.mu.Lock()`. This is a data
  race: the writer and reader can execute concurrently.
- **Root cause:** Missing read lock in `sendUpdate()`.
- **Impact:** Race detector reports a data race; on weakly-ordered CPUs the read
  could observe a nil or partially-written pointer.
- **Fix:** Read `s.updateCallback` under `s.mu.RLock()` into a local variable,
  release the lock, then call the local copy.

---

## ISSUE-038 — Medium — `run()` reads `s.speed` without a lock on first ticker creation
- **Affected components:** `internal/simulator/simulator.go:249`
- **Description:** The first line of `run()` is `ticker :=
  time.NewTicker(time.Duration(s.speed) * time.Millisecond)`, which reads
  `s.speed` without holding any lock. `SetSpeed()` writes `s.speed` under
  `s.mu.Lock()`. The resumed path later in `run()` correctly reads `s.speed`
  under `s.mu.RLock()`.
- **Root cause:** Oversight: the initial ticker creation did not follow the same
  pattern as the resume path.
- **Impact:** Data race reported by the race detector; in theory a partially
  updated speed value could produce a wildly wrong initial tick interval.
- **Fix:** Read `s.speed` under `s.mu.RLock()` before creating the initial
  ticker.

---

## ISSUE-039 — Low — WebSocket `handleInit` silently falls back to FCFS for unknown algorithms
- **Affected components:** `web/server.go:331`
- **Description:** The algorithm switch in `handleInit` has `default: sched =
  scheduler.NewFCFSScheduler()`. An unknown algorithm name (e.g. `"edf"`,
  `"fcfs "` with a trailing space) silently substitutes FCFS with no error sent
  to the client. The REST API path correctly returns HTTP 400 for unknown
  algorithms.
- **Root cause:** Inconsistent error handling between the WebSocket and REST
  paths.
- **Impact:** Client thinks it requested algorithm X but gets FCFS; simulation
  results are silently wrong.
- **Fix:** In the `default` case, call `s.sendError(conn, ...)` with a list of
  valid algorithm names and return.

---

## ISSUE-040 — Low — `generateID` uses millisecond-precision timestamp; concurrent same-ms calls collide
- **Affected components:** `internal/api/api.go:245`
- **Description:** `generateID(algorithm, t)` returns
  `algorithm + "-" + t.Format("20060102-150405.000")`. Millisecond precision
  means two simulations started for the same algorithm within 1ms receive the
  same ID. The second result is unreachable via the GET endpoint (map/cache
  lookup by ID returns the first).
- **Root cause:** Time alone is insufficient for uniqueness under concurrency.
- **Impact:** Low probability in production; higher in benchmarks or burst
  tests. Result of the second simulation is silently discarded.
- **Fix:** Append an atomic counter suffix: `fmt.Sprintf("%s-%s-%d", algorithm,
  t.Format(...), idCounter.Add(1))`.

---

## ISSUE-041 — Critical — `Shutdown()` closes broadcast channel before `sim.Stop()`, risking panic
- **Affected components:** `web/server.go` (Shutdown)
- **Description:** `Shutdown()` called `close(s.closed)` then `close(s.broadcast)` then `sim.Stop()`. The run goroutine's update callback includes `case s.broadcast <- update:` in a select; sending to a closed channel panics in Go even inside a select. Since `s.closed` is also closed, Go randomly picks between the send case (panic) and the receive case (safe), giving ~50% crash probability per outstanding update.
- **Root cause:** Wrong ordering — `close(s.broadcast)` must happen after the run goroutine has fully exited via `sim.Stop()` → `wg.Wait()`.
- **Impact:** Random server crash during graceful shutdown whenever a simulation was running.
- **Fix:** Move `close(s.broadcast)` to after `sim.Stop()` in `Shutdown()`.

---

## ISSUE-042 — High — `Stop()` skips `wg.Wait()` when state is `SimStateComplete`, leaking the run goroutine
- **Affected components:** `internal/simulator/simulator.go` (Stop)
- **Description:** When `run()` sets `state=SimStateComplete` and enters `sendUpdate()`, a concurrent `Stop()` call sees state ≠ Running/Paused and returns immediately without calling `wg.Wait()`. The run goroutine is still alive. A subsequent `Start()` → `wg.Add(1)` → new goroutine means two goroutines race on simulation state.
- **Root cause:** Missing `case SimStateComplete:` branch in `Stop()`.
- **Fix:** Add a `case SimStateComplete:` that sets state to Idle and calls `wg.Wait()` to ensure the final `sendUpdate()` has returned.

---

## ISSUE-043 — High — `Reset()` does not drain `stopChan`; stale signal silently kills the next `Start()`
- **Affected components:** `internal/simulator/simulator.go` (Reset)
- **Description:** When `Stop()` sends to `stopChan` (buffered 1) and `run()` exits via the natural-completion path (line 313) without reading it, the signal remains buffered. `Reset()` drained `pauseChan` but not `stopChan` (and the comment "stopChan was already consumed by run()" was wrong for this race). The next `Start()` launches a goroutine that immediately reads the stale stop signal and exits.
- **Root cause:** The drain was removed in the ISSUE-027 fix on the (incorrect) assumption that `run()` always consumes `stopChan` before exiting.
- **Fix:** Restore `select { case <-s.stopChan: default: }` drain in `Reset()`.

---

## ISSUE-044 — High — `middleware.statusWriter` does not implement `http.Hijacker`; WebSocket upgrades always 500
- **Affected components:** `internal/middleware/middleware.go` (statusWriter)
- **Description:** The `LogAndMetrics` middleware wraps `http.ResponseWriter` in a `statusWriter` struct. gorilla/websocket calls `Hijack()` on the ResponseWriter during upgrade; since `statusWriter` does not implement `http.Hijacker`, the upgrade fails with "does not implement http.Hijacker" and the server returns 500 to every WebSocket client.
- **Root cause:** `statusWriter` only embeds `http.ResponseWriter` for `Write`/`WriteHeader`; optional interfaces (`Hijacker`, `Flusher`) must be forwarded explicitly.
- **Fix:** Add `Hijack()` and `Flush()` methods to `statusWriter` that delegate to the underlying `ResponseWriter`.

---

## ISSUE-045 — High — `handleInit` stops old simulator before validating process list; parse error leaves stale stopped sim
- **Affected components:** `web/server.go` (handleInit)
- **Description:** `old.Stop()` was called before iterating `processesData`. If any process failed validation, the function returned early without assigning `s.simulator = newSim`. `s.simulator` then pointed to the old (now-stopped) simulator. Subsequent `start`/`step` WS messages called `Start()` on the stopped sim, restarting its goroutine on stale state.
- **Root cause:** Validation and side-effects were interleaved without a rollback path.
- **Fix:** Parse and validate all processes into a local slice first; only call `old.Stop()` after successful validation.

---

## ISSUE-046 — Medium — `decodeJSON` passes `nil` ResponseWriter to `http.MaxBytesReader`
- **Affected components:** `internal/api/api.go` (decodeJSON)
- **Description:** `http.MaxBytesReader(nil, r.Body, 1<<20)` passes a nil `http.ResponseWriter`. In Go versions where `maxBytesReader` dereferences `w` to send a 413 status, this causes a nil pointer panic. Even in versions that return the error instead, `nil` is semantically wrong and may panic on future Go upgrades.
- **Root cause:** `decodeJSON` did not receive the `ResponseWriter` parameter, so `nil` was used as a placeholder.
- **Fix:** Add `w http.ResponseWriter` parameter to `decodeJSON` and pass the actual writer from `handleSimulate`.

---

## ISSUE-047 — Critical — Data race: `Shutdown()` closes `s.broadcast` while WebSocket handlers still sending to it
- **Affected components:** `web/server.go` (Shutdown, HandleWebSocket, handleAddProcess, handleReset, handleStep)
- **Description:** ISSUE-041 fixed the run() goroutine path, but WebSocket handler goroutines (handleAddProcess, handleReset, handleStep, handleSpeed) also send to `s.broadcast` via `select { case s.broadcast <- state: ... }`. The `case <-s.closed:` guard does not prevent the race: Go's select evaluates ALL cases, and concurrent `close(s.broadcast)` + `s.broadcast <- state` (from another goroutine) is a documented data race in Go even inside a select statement.
- **Root cause:** `Shutdown()` did not wait for all WebSocket handler goroutines to finish before closing the broadcast channel.
- **Impact:** Race detector flags the issue. Under the Go memory model, concurrent close and send on the same channel is undefined behavior.
- **Validation:** `go test -race -count=1 ./web/` previously failed with DATA RACE in `TestWS_AddProcess_Valid`; now passes clean.
- **Fix:** Added `clientWg sync.WaitGroup` to `Server`. `HandleWebSocket` calls `clientWg.Add(1)` / `defer clientWg.Done()`. `Shutdown()` now: closes connections → `clientWg.Wait()` → `close(s.broadcast)`. This guarantees no handler goroutine is mid-select on broadcast when it is closed.

---

## ENHANCEMENT-001 — `buildScheduler` double-instantiation fixed
- **Affected components:** `internal/api/api.go` (buildScheduler)
- **Description:** buildScheduler created each scheduler twice (`return NewFCFSScheduler(), NewFCFSScheduler().Name(), nil`), seeding the lottery RNG twice and wasting allocations.
- **Fix:** Store the scheduler in a local variable; call `.Name()` on the same instance.

---

## ENHANCEMENT-002 — `web` package unit tests (0% → 83.3% coverage)
- **Affected components:** `web/server_test.go` (new file)
- **Description:** The web package had no tests. Added 40+ test functions covering: DefaultPort, parseProcess, HandleHealth, all WebSocket message types (init, start, pause, resume, stop, reset, step, speed, addProcess, getState), broadcast delivery, and Shutdown behavior.

---

## ENHANCEMENT-003 — Priority aging / starvation prevention
- **Affected components:** `internal/scheduler/scheduler.go` (PriorityScheduler)
- **Description:** PriorityScheduler had no starvation prevention. A low-priority process could wait indefinitely if higher-priority processes kept arriving.
- **Fix:** Added stateless priority aging: for every `agingInterval` ticks a process spends waiting (derived from `p.LastExecuted` and `p.ArrivalTime` — no per-process map), effective priority is improved by 1 (clamped at 0). Default interval: 10 ticks. `NewPrioritySchedulerWithAging(preemptive, interval)` constructor added for custom intervals. Both `Schedule()` and `Preempt()` use effective priority. Added 13 aging-specific tests.

---

## ENHANCEMENT-004 — `scheduler` package test coverage (36% → 91.3%)
- **Affected components:** `internal/scheduler/scheduler_test.go` (new file)
- **Description:** Created 60+ test functions covering FCFS, SJF, SRTF, RoundRobin, Priority (with/without aging), CFS, MLFQ, MLQ, and Lottery schedulers — including all tiebreak paths, edge cases (empty queue, nil current, single process), and boundary conditions.

---

## ENHANCEMENT-005 — I/O burst support (previously dead code)
- **Affected components:** `internal/simulator/simulator.go`, `internal/process/process.go`, `internal/simulator/simulator_test.go`
- **Description:** `Process.IOBursts`, `Process.CurrentIOIndex`, and `process.StateWaiting` existed in the data model but `executeTimeUnit()` never checked them. Processes with I/O bursts ran as if they had no I/O.
- **Fix:** Added `ioQueue []*process.Process` to `Simulator`. Two new helpers: `tickIOQueue()` (runs at start of each tick; decrements IORemaining for each waiting process; moves done processes back to readyQueue) and `checkIOBurst()` (runs after each CPU execution; if a process has consumed enough CPU ticks to trigger its next I/O burst, it is moved to ioQueue with StateWaiting). Added `IORemaining int` to `Process` (with Clone/Reset support). Added `WaitingQueue []*process.Process` to `SimulationUpdate` for client visibility. `Reset()` now also resets IOBurst.Completed flags. Six integration tests added.
