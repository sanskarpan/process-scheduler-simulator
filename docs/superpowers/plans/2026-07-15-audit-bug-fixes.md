# Audit Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all 15 confirmed/plausible bugs surfaced by the deep codebase audit, with a regression test for each.

**Architecture:** Bugs are grouped by file/subsystem. Each task writes a failing test first, then the minimal fix, then verifies. Tests live in the existing `_test.go` files for each package. The ISSUES.md is written first as a stable bug registry.

**Tech Stack:** Go 1.23, gorilla/websocket, sync.RWMutex, net/http

---

## Task 0: Create ISSUES.md

**Files:**
- Create: `ISSUES.md` (replace existing with audit findings)

- [ ] Write `ISSUES.md` with all 15 new bug entries using the IDs ISSUE-026 through ISSUE-040
- [ ] Commit: `docs: add 15 new audit findings to ISSUES.md`

---

## Task 1: Fix concurrent WebSocket writes (ISSUE-026)

**Files:**
- Modify: `web/server.go` — add `connMu sync.Mutex` to a `wsConn` wrapper; all write calls go through it

**Root cause:** Three goroutines (handleBroadcasts, pinger, read-loop) write to the same `*websocket.Conn` simultaneously. gorilla requires serialized writes.

**Fix:** Introduce a `wsConn` struct that wraps `*websocket.Conn` and a `sync.Mutex`. Replace the bare `*websocket.Conn` in `clients` with `*wsConn`. All write paths call `wsConn.writeJSON` / `wsConn.writeMessage` which hold the mutex.

- [ ] **Step 1: Add wsConn wrapper to web/server.go**

Add before `type Server struct`:
```go
type wsConn struct {
    conn *websocket.Conn
    mu   sync.Mutex
}

func (c *wsConn) writeJSON(v interface{}) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.conn.WriteJSON(v)
}

func (c *wsConn) writeMessage(msgType int, data []byte) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.conn.WriteMessage(msgType, data)
}

func (c *wsConn) setWriteDeadline(t time.Time) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.conn.SetWriteDeadline(t)
}
```

- [ ] **Step 2: Update Server.clients to map[*wsConn]struct{}**

Change the `clients` field type and all its usages:
- `clients map[*wsConn]struct{}` in the struct definition
- `HandleWebSocket`: `wc := &wsConn{conn: conn}` after upgrade; add `wc` to clients; pass `wc` to pinger and message handler
- `handleBroadcasts`: iterate `[]*wsConn`; call `c.writeJSON(update)` and `c.writeMessage` via the mutex methods
- `unregisterClient`: accept `*wsConn`, call `wc.conn.Close()`
- `sendSuccess`, `sendError`, `handleGetState`, `handleInit`: accept `*wsConn`

- [ ] **Step 3: Remove all bare SetWriteDeadline + WriteJSON/WriteMessage calls outside wsConn**

Each write site must go through `wc.writeJSON(...)` instead of `conn.SetWriteDeadline(...); conn.WriteJSON(...)`. The deadline is set inside each `wsConn` method (callers no longer set it separately; move deadline logic into the methods).

- [ ] **Step 4: Run tests**

```bash
go test -race ./web/... ./internal/...
```
Expected: all pass, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add web/server.go
git commit -m "fix(web): serialize per-connection WebSocket writes via wsConn mutex (ISSUE-026)"
```

---

## Task 2: Fix Reset() goroutine race (ISSUE-027)

**Files:**
- Modify: `internal/simulator/simulator.go` — add `wg sync.WaitGroup` to track run goroutine

**Root cause:** `Reset()` drains `stopChan` before the goroutine reads it, so the goroutine keeps running. Next `Start()` spawns a second goroutine that races with the first.

**Fix:** Add a `sync.WaitGroup` to `Simulator`. `run()` calls `wg.Done()` on exit. `Stop()` signals via channel AND waits for the goroutine to exit. Drain happens after the goroutine is confirmed gone.

- [ ] **Step 1: Write failing test in internal/simulator/simulator_test.go**

```go
func TestResetNoGoroutineLeak(t *testing.T) {
    sched := scheduler.NewFCFSScheduler()
    sim := NewSimulator(sched)
    p := process.NewProcess(1, "P1", 0, 100, 0)
    sim.AddProcess(p)
    sim.Start()
    time.Sleep(20 * time.Millisecond)
    sim.Reset()
    // After Reset + Start, only ONE goroutine should execute.
    // We verify by counting executeTimeUnit calls via a test hook.
    // Simpler: check that time doesn't advance faster than 1/tick after Reset+Start.
    sim.SetSpeed(50)
    sim.Start()
    t.Cleanup(func() { sim.Stop() })
    time.Sleep(60 * time.Millisecond)
    state := sim.GetCurrentState()
    // At 50ms/tick, 60ms -> at most 2 ticks. With 2 goroutines it could be 4+.
    if state.CurrentTime > 3 {
        t.Errorf("time advanced too fast (%d ticks in 60ms), suggests goroutine leak",
            state.CurrentTime)
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test -race -run TestResetNoGoroutineLeak ./internal/simulator/
```
Expected: may pass or fail intermittently (race window).

- [ ] **Step 3: Add WaitGroup to Simulator struct**

In `NewSimulator` add `wg sync.WaitGroup` field. In `run()`, add `s.wg.Add(1)` before launching (called inside `Start()`) and `defer s.wg.Done()` at top of `run()`. In `Stop()`, after sending to stopChan, call `s.wg.Wait()` — but only if we actually sent (to avoid blocking when state was not Running). In `Reset()`, remove the `<-s.stopChan` drain since Stop() now guarantees the goroutine is gone before returning.

Full `Stop()` fix:
```go
func (s *Simulator) Stop() {
    s.mu.Lock()
    if s.state == SimStateRunning || s.state == SimStatePaused {
        s.state = SimStateIdle
        s.mu.Unlock()
        select {
        case s.stopChan <- true:
        default:
        }
        s.wg.Wait() // wait for run() goroutine to exit
        return
    }
    s.mu.Unlock()
}
```

Full `Reset()` fix (remove stopChan drain):
```go
func (s *Simulator) Reset() {
    s.Stop() // now guaranteed: goroutine is gone when Stop() returns

    // Only drain pauseChan (not stopChan — Stop already consumed it).
    select {
    case <-s.pauseChan:
    default:
    }

    s.mu.Lock()
    defer s.mu.Unlock()
    // ... rest of reset unchanged ...
}
```

`Start()` must call `s.wg.Add(1)` before launching the goroutine:
```go
func (s *Simulator) Start() {
    s.mu.Lock()
    if s.state == SimStateRunning {
        s.mu.Unlock()
        return
    }
    s.state = SimStateRunning
    s.wg.Add(1)
    s.mu.Unlock()
    go s.run()
}
```

And `run()` calls `defer s.wg.Done()` as its first defer.

- [ ] **Step 4: Run tests**

```bash
go test -race -count=3 -run TestResetNoGoroutineLeak ./internal/simulator/
go test -race ./internal/simulator/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/simulator/simulator.go internal/simulator/simulator_test.go
git commit -m "fix(simulator): use WaitGroup to guarantee goroutine exits before Reset (ISSUE-027)"
```

---

## Task 3: Fix CFS VRuntime integer truncation (ISSUE-028)

**Files:**
- Modify: `internal/process/process.go:134` — fix VRuntime arithmetic
- Modify: `internal/scheduler/scheduler.go:287` — fix Preempt threshold arithmetic

**Root cause:** `int64(duration * 1024 / p.Weight)` — both operands are `int`, so integer division truncates to 0 when `Weight > 1024`. `niceToWeight(-1)=1280 > 1024`, so VRuntime never advances for negatively-niced processes.

**Fix:** Cast before dividing: `int64(duration) * 1024 / int64(p.Weight)`.

- [ ] **Step 1: Write test in internal/process (new file)**

Create `internal/process/process_test.go`:
```go
package process

import "testing"

func TestVRuntimeAdvancesForHighWeight(t *testing.T) {
    p := NewProcess(1, "P1", 0, 10, 0)
    p.SetNice(-1) // weight = niceToWeight(-1) = 1280 > 1024
    before := p.VRuntime
    p.Execute(0, 1)
    if p.VRuntime == before {
        t.Errorf("VRuntime did not advance for Weight=%d (nice=-1): got %d",
            p.Weight, p.VRuntime)
    }
}

func TestVRuntimeAdvancesForMaxNice(t *testing.T) {
    p := NewProcess(1, "P1", 0, 100, 0)
    p.SetNice(-20) // maximum high-weight
    before := p.VRuntime
    p.Execute(0, 1)
    if p.VRuntime == before {
        t.Errorf("VRuntime did not advance for Weight=%d (nice=-20): got %d",
            p.Weight, p.VRuntime)
    }
}

func TestVRuntimeDefaultWeight(t *testing.T) {
    p := NewProcess(1, "P1", 0, 10, 0) // Weight=1024
    p.Execute(0, 1)
    if p.VRuntime != 1 {
        t.Errorf("VRuntime = %d, want 1 for Weight=1024", p.VRuntime)
    }
}
```

- [ ] **Step 2: Run failing tests**

```bash
go test -run TestVRuntime ./internal/process/
```
Expected: TestVRuntimeAdvancesForHighWeight and TestVRuntimeAdvancesForMaxNice FAIL.

- [ ] **Step 3: Fix process.go:134**

Change:
```go
p.VRuntime += int64(duration * 1024 / p.Weight)
```
To:
```go
p.VRuntime += int64(duration) * 1024 / int64(p.Weight)
```

Also fix `scheduler.go:287` (Preempt threshold):
```go
// Before:
return minVruntime < current.VRuntime-int64(s.minGranularity*1024/current.Weight)
// After:
return minVruntime < current.VRuntime-int64(s.minGranularity)*1024/int64(current.Weight)
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/process/ ./internal/scheduler/ ./internal/simulator/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/process/process.go internal/process/process_test.go internal/scheduler/scheduler.go
git commit -m "fix(cfs): prevent VRuntime integer truncation for high-weight processes (ISSUE-028)"
```

---

## Task 4: Fix addProcess with past arrivalTime (ISSUE-029)

**Files:**
- Modify: `internal/simulator/simulator.go:366` — checkArrivals uses strict equality; change to `<=`

**Root cause:** `p.ArrivalTime == s.currentTime` misses processes added dynamically with `ArrivalTime < currentTime`.

**Fix:** Change to `p.ArrivalTime <= s.currentTime` so a dynamically-added process with a past arrival time enters the ready queue on the next tick.

- [ ] **Step 1: Write test in internal/simulator/simulator_test.go**

```go
func TestAddProcessPastArrival(t *testing.T) {
    sched := scheduler.NewFCFSScheduler()
    sim := NewSimulator(sched)

    // Add first process that runs for 5 ticks
    sim.AddProcess(process.NewProcess(1, "P1", 0, 5, 0))

    done := make(chan struct{})
    sim.SetUpdateCallback(func(u *SimulationUpdate) {
        if u.State == SimStateComplete {
            select {
            case done <- struct{}{}:
            default:
            }
        }
    })
    sim.Start()
    time.Sleep(200 * time.Millisecond) // let sim run a few ticks

    // Add a second process with arrivalTime=0 (past)
    sim.AddProcess(process.NewProcess(2, "P2", 0, 3, 0))

    select {
    case <-done:
        // simulation completed - both processes ran
    case <-time.After(5 * time.Second):
        t.Fatal("simulation did not complete: process with past arrivalTime was never scheduled")
    }

    state := sim.GetCurrentState()
    if state.Metrics.CompletedProcesses != 2 {
        t.Errorf("expected 2 completed processes, got %d", state.Metrics.CompletedProcesses)
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test -race -run TestAddProcessPastArrival -timeout 10s ./internal/simulator/
```
Expected: FAIL (timeout after 5s — simulation never completes).

- [ ] **Step 3: Fix checkArrivals in simulator.go**

Change line ~368:
```go
// Before:
if p.ArrivalTime == s.currentTime && p.State == process.StateNew {
// After:
if p.ArrivalTime <= s.currentTime && p.State == process.StateNew {
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestAddProcessPastArrival ./internal/simulator/
go test -race ./internal/simulator/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/simulator/simulator.go internal/simulator/simulator_test.go
git commit -m "fix(simulator): schedule processes with past arrivalTime on dynamic add (ISSUE-029)"
```

---

## Task 5: Fix handleBroadcasts goroutine leak on Shutdown (ISSUE-030)

**Files:**
- Modify: `web/server.go` — close `s.broadcast` channel in `Shutdown()`

**Root cause:** `handleBroadcasts` ranges over `s.broadcast`. The channel is never closed, so the goroutine blocks forever after `Shutdown()`.

**Fix:** Close `s.broadcast` in `Shutdown()`. Add a nil-check in `handleBroadcasts` after Shutdown to avoid sending on a closed channel — use the `closed` channel as a guard in the update callback.

- [ ] **Step 1: Fix Shutdown() to close broadcast channel**

In `Shutdown()`, after `close(s.closed)`, add:
```go
close(s.broadcast)
```

But the broadcast goroutine must handle the channel being closed gracefully. The `for update := range s.broadcast` loop exits automatically when the channel is closed. ✓

The send site (update callback set in `handleInit`) uses a non-blocking select:
```go
select {
case s.broadcast <- update:
default:
}
```
This panics if the channel is closed. Guard it with the `s.closed` signal:
```go
select {
case s.broadcast <- update:
case <-s.closed:
default:
}
```

- [ ] **Step 2: Update all broadcast send sites**

In `handleInit` (the update callback) and `handleReset`/`handleAddProcess` direct sends, change every:
```go
select {
case s.broadcast <- state:
default:
}
```
to:
```go
select {
case s.broadcast <- state:
case <-s.closed:
default:
}
```

- [ ] **Step 3: Run tests**

```bash
go test -race ./web/... ./internal/...
```
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add web/server.go
git commit -m "fix(web): close broadcast channel on Shutdown to prevent goroutine leak (ISSUE-030)"
```

---

## Task 6: Fix CORS invalid multi-origin ACAO header (ISSUE-031)

**Files:**
- Modify: `internal/middleware/middleware.go:102-133`

**Root cause:** `else if allow != ""` sets `Access-Control-Allow-Origin` to `strings.Join(allowedOrigins, ", ")` — a comma-separated list that browsers reject as invalid.

**Fix:** Remove the else branch entirely. CORS headers should only be set when the request origin is in the allowlist. For requests without an Origin header, set no CORS headers.

- [ ] **Step 1: Write test in internal/middleware (new file)**

Create `internal/middleware/middleware_test.go`:
```go
package middleware

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func nopHandler(w http.ResponseWriter, r *http.Request) {}

func TestCORSAllowedOrigin(t *testing.T) {
    h := CORS([]string{"https://trusted.example.com"})(http.HandlerFunc(nopHandler))
    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Origin", "https://trusted.example.com")
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    got := rec.Header().Get("Access-Control-Allow-Origin")
    if got != "https://trusted.example.com" {
        t.Errorf("ACAO = %q, want exact origin", got)
    }
}

func TestCORSRejectedOriginNoHeader(t *testing.T) {
    h := CORS([]string{"https://trusted.example.com"})(http.HandlerFunc(nopHandler))
    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Origin", "https://attacker.com")
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    got := rec.Header().Get("Access-Control-Allow-Origin")
    if got != "" {
        t.Errorf("ACAO = %q for rejected origin, want empty", got)
    }
}

func TestCORSNoOriginHeaderSetsNoACAO(t *testing.T) {
    h := CORS([]string{"https://trusted.example.com"})(http.HandlerFunc(nopHandler))
    req := httptest.NewRequest("GET", "/", nil)
    // No Origin header
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    got := rec.Header().Get("Access-Control-Allow-Origin")
    if got != "" {
        t.Errorf("ACAO = %q for no-origin request, want empty", got)
    }
}

func TestCORSWildcard(t *testing.T) {
    h := CORS([]string{"*"})(http.HandlerFunc(nopHandler))
    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Origin", "https://anyone.com")
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    got := rec.Header().Get("Access-Control-Allow-Origin")
    if got != "https://anyone.com" {
        t.Errorf("ACAO = %q for wildcard config, want origin echoed", got)
    }
}
```

- [ ] **Step 2: Run failing tests**

```bash
go test -run TestCORS ./internal/middleware/
```
Expected: TestCORSRejectedOriginNoHeader FAIL (currently sets ACAO to joined list), TestCORSNoOriginHeaderSetsNoACAO FAIL.

- [ ] **Step 3: Fix middleware.go CORS function**

```go
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            origin := r.Header.Get("Origin")
            if origin != "" && originAllowed(origin, allowedOrigins) {
                w.Header().Set("Access-Control-Allow-Origin", origin)
                w.Header().Set("Vary", "Origin")
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
                w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
                w.Header().Set("Access-Control-Max-Age", "300")
            }
            // No else: unrecognised origins get no ACAO header → browser blocks them.
            if r.Method == http.MethodOptions {
                w.WriteHeader(http.StatusNoContent)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

Remove the `allow := strings.Join(...)` precomputation (now unused) and the `else if allow != ""` branch.

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/middleware/
go test -race ./...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/middleware/middleware.go internal/middleware/middleware_test.go
git commit -m "fix(middleware): CORS must not set ACAO for unrecognised or absent origins (ISSUE-031)"
```

---

## Task 7: Fix scheduleNextProcess removes by pointer, not PID (ISSUE-032)

**Files:**
- Modify: `internal/simulator/simulator.go:392-396`

**Root cause:** When two processes share a PID, `scheduleNextProcess` finds and removes the first PID match in `readyQueue`, not the pointer the scheduler chose. The wrong process is dequeued.

**Fix:** Remove by pointer comparison (`p == next`) instead of `p.PID == next.PID`.

- [ ] **Step 1: Write test in internal/simulator/simulator_test.go**

```go
func TestScheduleNextProcessRemovesByPointer(t *testing.T) {
    // Two processes with the same PID - scheduleNextProcess must dequeue
    // the exact pointer the scheduler selected, not the first PID match.
    sched := scheduler.NewSJFScheduler() // shortest first: P2 (burst=2) before P1 (burst=5)
    sim := NewSimulator(sched)

    p1 := process.NewProcess(1, "LongJob", 0, 5, 0)
    p2 := process.NewProcess(1, "ShortJob", 0, 2, 0) // same PID=1, shorter burst

    sim.AddProcess(p1)
    sim.AddProcess(p2)

    done := make(chan *SimulationUpdate, 1)
    sim.SetUpdateCallback(func(u *SimulationUpdate) {
        if u.State == SimStateComplete {
            select {
            case done <- u:
            default:
            }
        }
    })
    sim.Start()

    select {
    case u := <-done:
        if u.Metrics.CompletedProcesses != 2 {
            t.Errorf("expected 2 completed, got %d (one may have been lost)",
                u.Metrics.CompletedProcesses)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("simulation did not complete — a process was likely lost")
    }
}
```

- [ ] **Step 2: Run the test**

```bash
go test -race -run TestScheduleNextProcessRemovesByPointer ./internal/simulator/
```
(May pass or hang depending on implementation details — the test detects the "lost process" scenario.)

- [ ] **Step 3: Fix scheduleNextProcess in simulator.go**

Change the removal loop from PID comparison to pointer comparison:
```go
// Remove from ready queue — match by pointer, not PID, to handle duplicate PIDs.
for i, p := range s.readyQueue {
    if p == next {
        s.readyQueue = append(s.readyQueue[:i], s.readyQueue[i+1:]...)
        break
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/simulator/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/simulator/simulator.go internal/simulator/simulator_test.go
git commit -m "fix(simulator): remove selected process by pointer not PID in scheduleNextProcess (ISSUE-032)"
```

---

## Task 8: Fix MLFQ duplicate PID level corruption (ISSUE-033)

**Files:**
- Modify: `internal/scheduler/scheduler.go` — MLFQ/MLQ RemoveProcess and AddProcess to handle duplicate PIDs

**Root cause:** `MLFQScheduler` uses `map[int]int` keyed by PID. Two processes with PID=2 → first completion deletes `levels[2]` → second process loses its earned demotion (resets to 0 via Go map default).

**Fix:** Key the levels map by process pointer (memory address via `uintptr`) instead of PID. Since Go maps can't be keyed by pointer directly, use the process pointer as a `uintptr` key.

Actually, a cleaner fix: since this is a simulator, use `*process.Process` pointer as the map key directly — Go supports pointer keys in maps.

- [ ] **Step 1: Write test in internal/scheduler/lottery_mlq_test.go**

```go
func TestMLFQDuplicatePIDLevel(t *testing.T) {
    s := NewMLFQScheduler()
    p1 := process.NewProcess(1, "P1", 0, 10, 0)
    p2 := process.NewProcess(1, "P2", 0, 10, 0) // same PID

    s.AddProcess(p1)
    s.AddProcess(p2)

    // Demote p2 to level 1
    s.OnQuantumExpired(p2)
    if s.QuantumFor(p2) == s.QuantumFor(p1) {
        t.Fatal("p2 should have a larger quantum after demotion")
    }

    // p1 completes
    s.RemoveProcess(p1)

    // p2's level must still be 1, not reset to 0
    q := s.QuantumFor(p2)
    if q == s.timeQuantums[0] {
        t.Errorf("QuantumFor(p2) = %d (level 0 quantum), demotion was lost after p1 removal", q)
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test -race -run TestMLFQDuplicatePIDLevel ./internal/scheduler/
```
Expected: FAIL — `QuantumFor(p2)` returns the level-0 quantum.

- [ ] **Step 3: Change MLFQ levels map key from int to *process.Process**

In `MLFQScheduler`:
```go
type MLFQScheduler struct {
    name         string
    timeQuantums []int
    numLevels    int
    levels       map[*process.Process]int // keyed by pointer, not PID
}
```

Update all usages:
- `AddProcess(p)`: `if _, ok := s.levels[p]; !ok { s.levels[p] = 0 }`
- `RemoveProcess(p)`: `delete(s.levels, p)`
- `Schedule`: `lvl := s.levels[p]`
- `Preempt`: `currentLevel := s.levels[current]` and `s.levels[p] < currentLevel`
- `QuantumFor`: `lvl := s.levels[p]`
- `OnQuantumExpired`: `lvl := s.levels[p]; if lvl < s.numLevels-1 { s.levels[p] = lvl + 1 }`
- `Reset`: `s.levels = make(map[*process.Process]int)`
- `NewMLFQScheduler`: `levels: make(map[*process.Process]int)`

Do the same for `MLQScheduler.levels` field (same pattern, different type name).

- [ ] **Step 4: Update MLQScheduler similarly**

`MLQScheduler.levels` becomes `map[*process.Process]int`. Update `AddProcess`, `RemoveProcess`, `Preempt`, `levelFor` (used in Schedule). `levelFor` currently uses `p.Priority` directly, so the levels map for MLQ stores `levelFor(p)` at AddProcess time. Update similarly.

- [ ] **Step 5: Run tests**

```bash
go test -race ./internal/scheduler/ ./internal/simulator/
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/lottery_mlq_test.go
git commit -m "fix(scheduler): use process pointer as map key in MLFQ/MLQ to prevent duplicate PID level corruption (ISSUE-033)"
```

---

## Task 9: Fix CFS Preempt off-by-one (ISSUE-034)

**Files:**
- Modify: `internal/scheduler/scheduler.go:276`

**Root cause:** `len(readyQueue) <= 1` returns false early when there's exactly 1 competing process in the queue. Should be `== 0` (no competitors).

**Fix:** Change `<= 1` to `== 0`.

- [ ] **Step 1: Write test in internal/scheduler (add to existing scheduler_test.go or create)**

Add to `internal/simulator/scheduler_test.go` (if it exists) or new file `internal/scheduler/cfs_test.go`:
```go
func TestCFSPreemptWithOneCompetitor(t *testing.T) {
    s := NewCFSScheduler()
    // p1 has run for a while (high vruntime), p2 just arrived (vruntime=0)
    p1 := process.NewProcess(1, "P1", 0, 10, 0)
    p1.VRuntime = 1000 // simulated high vruntime
    p2 := process.NewProcess(2, "P2", 5, 10, 0)
    p2.VRuntime = 0

    // readyQueue has exactly p2 (current=p1 was removed from queue)
    readyQueue := []*process.Process{p2}
    if !s.Preempt(p1, readyQueue, 10) {
        t.Error("CFS should preempt p1 (vruntime=1000) in favour of p2 (vruntime=0) when p2 is the only competitor")
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test -race -run TestCFSPreemptWithOneCompetitor ./internal/scheduler/
```
Expected: FAIL.

- [ ] **Step 3: Fix scheduler.go:276**

```go
// Before:
if current == nil || len(readyQueue) <= 1 {
// After:
if current == nil || len(readyQueue) == 0 {
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/scheduler/ ./internal/simulator/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/cfs_test.go
git commit -m "fix(cfs): correct Preempt guard from <=1 to ==0 for single-competitor case (ISSUE-034)"
```

---

## Task 10: Fix Step() concurrent double-execute (ISSUE-035)

**Files:**
- Modify: `internal/simulator/simulator.go:142` — Step() must hold the state lock for the entire check-and-execute sequence

**Root cause:** Two concurrent goroutines both pass the state check (`!= Running && != Complete`) and both call `executeTimeUnit()`, advancing the clock 2 ticks instead of 1.

**Fix:** Add a `stepMu sync.Mutex` to serialize Step() calls. The state check and execute must be atomic.

- [ ] **Step 1: Write test in internal/simulator/simulator_test.go**

```go
func TestStepConcurrentSerializes(t *testing.T) {
    sched := scheduler.NewFCFSScheduler()
    sim := NewSimulator(sched)
    sim.AddProcess(process.NewProcess(1, "P1", 0, 100, 0))

    // pause the sim so Step() is the only way to advance
    sim.mu.Lock()
    sim.state = SimStatePaused
    sim.mu.Unlock()

    // Trigger checkArrivals so P1 is in the ready queue
    sim.executeTimeUnit()

    before := sim.currentTime

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            sim.Step()
        }()
    }
    wg.Wait()

    after := sim.currentTime
    // Each Step() call should execute exactly one time unit.
    // 10 concurrent Step() calls → exactly 10 ticks advanced.
    if after-before != 10 {
        t.Errorf("expected exactly 10 ticks from 10 concurrent Step() calls, got %d", after-before)
    }
}
```

- [ ] **Step 2: Run test (may pass or give wrong count)**

```bash
go test -race -run TestStepConcurrentSerializes ./internal/simulator/
```

- [ ] **Step 3: Add stepMu to Simulator and use it in Step()**

Add `stepMu sync.Mutex` to the `Simulator` struct. Wrap the body of `Step()` in `s.stepMu.Lock() / defer s.stepMu.Unlock()`:

```go
func (s *Simulator) Step() {
    s.stepMu.Lock()
    defer s.stepMu.Unlock()

    s.mu.Lock()
    if s.state == SimStateRunning || s.state == SimStateComplete {
        s.mu.Unlock()
        return
    }
    if s.state == SimStateIdle {
        s.state = SimStatePaused
    }
    s.mu.Unlock()

    s.executeTimeUnit()
    s.sendUpdate()

    if s.isComplete() {
        s.mu.Lock()
        s.state = SimStateComplete
        s.mu.Unlock()
        s.sendUpdate()
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/simulator/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/simulator/simulator.go internal/simulator/simulator_test.go
git commit -m "fix(simulator): serialize Step() calls with stepMu to prevent double time-unit execution (ISSUE-035)"
```

---

## Task 11: Fix LotteryScheduler.Reset() hardcoded seed (ISSUE-036)

**Files:**
- Modify: `internal/scheduler/scheduler.go:488`

**Root cause:** `Reset()` hard-codes the reseed to `0xC0FFEE` instead of the seed originally passed to `NewRNG()`.

**Fix:** Store the original seed in `deterministicRNG` and use it in `Reset()`.

- [ ] **Step 1: Write test in internal/scheduler/lottery_mlq_test.go**

```go
func TestLotteryResetRestoresSeed(t *testing.T) {
    seed := uint64(42)
    s := NewLotteryScheduler(1, NewRNG(seed))
    p := process.NewProcess(1, "P1", 0, 1, 0)
    q := process.NewProcess(2, "P2", 0, 1, 0)
    ready := []*process.Process{p, q}

    // Record first 20 picks
    first := make([]int, 20)
    for i := range first {
        first[i] = s.Schedule(ready, 0).PID
    }

    s.Reset()

    // After reset, picks should be identical
    for i := range first {
        got := s.Schedule(ready, 0).PID
        if got != first[i] {
            t.Errorf("pick[%d] after Reset: got PID=%d, want PID=%d (seed not restored)", i, got, first[i])
        }
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test -race -run TestLotteryResetRestoresSeed ./internal/scheduler/
```
Expected: FAIL — picks differ after reset (0xC0FFEE ≠ seed 42).

- [ ] **Step 3: Fix deterministicRNG to store original seed**

```go
type deterministicRNG struct {
    state uint64
    seed  uint64 // original seed for Reset
}

func NewRNG(seed uint64) RNG {
    if seed == 0 {
        seed = 0x9E3779B97F4A7C15
    }
    return &deterministicRNG{state: seed, seed: seed}
}
```

Fix `LotteryScheduler.Reset()`:
```go
func (s *LotteryScheduler) Reset() {
    if r, ok := s.rng.(*deterministicRNG); ok {
        r.state = r.seed
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/scheduler/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/lottery_mlq_test.go
git commit -m "fix(scheduler): LotteryScheduler.Reset restores original seed, not hardcoded 0xC0FFEE (ISSUE-036)"
```

---

## Task 12: Fix updateCallback data race (ISSUE-037)

**Files:**
- Modify: `internal/simulator/simulator.go:512-517`

**Root cause:** `sendUpdate()` reads `s.updateCallback` without holding any lock; `SetUpdateCallback()` writes it under `s.mu.Lock()`.

**Fix:** Read `s.updateCallback` under `s.mu.RLock()` into a local variable, release the lock, then call it.

- [ ] **Step 1: Fix sendUpdate() in simulator.go**

```go
func (s *Simulator) sendUpdate() {
    update := s.snapshotState()
    s.mu.RLock()
    cb := s.updateCallback
    s.mu.RUnlock()
    if cb != nil {
        cb(update)
    }
}
```

Note: `snapshotState()` already acquires `s.mu.RLock()` internally. The separate RLock here for `cb` is fine — RLocks can be held concurrently.

- [ ] **Step 2: Run tests with race detector**

```bash
go test -race -count=3 ./internal/simulator/
```
Expected: all pass, no race warnings.

- [ ] **Step 3: Commit**

```bash
git add internal/simulator/simulator.go
git commit -m "fix(simulator): read updateCallback under RLock to eliminate data race (ISSUE-037)"
```

---

## Task 13: Fix s.speed data race in run() (ISSUE-038)

**Files:**
- Modify: `internal/simulator/simulator.go:249`

**Root cause:** `run()` reads `s.speed` at line 249 to create the initial ticker, without holding any lock. `SetSpeed()` writes under `s.mu.Lock()`.

**Fix:** Read `s.speed` under `s.mu.RLock()` at the start of `run()`.

- [ ] **Step 1: Fix run() in simulator.go**

```go
func (s *Simulator) run() {
    defer s.wg.Done()

    s.mu.RLock()
    speed := s.speed
    s.mu.RUnlock()

    ticker := time.NewTicker(time.Duration(speed) * time.Millisecond)
    defer ticker.Stop()
    // ... rest of run unchanged ...
```

- [ ] **Step 2: Run tests with race detector**

```bash
go test -race -count=3 ./internal/simulator/
```
Expected: all pass, no race warnings.

- [ ] **Step 3: Commit**

```bash
git add internal/simulator/simulator.go
git commit -m "fix(simulator): read s.speed under RLock in run() to eliminate data race (ISSUE-038)"
```

---

## Task 14: Fix WebSocket unknown algorithm silently falls back to FCFS (ISSUE-039)

**Files:**
- Modify: `web/server.go:301-378` — `handleInit`

**Root cause:** The algorithm switch in `handleInit` has `default: sched = scheduler.NewFCFSScheduler()`, silently substituting FCFS for unrecognised names. The REST API correctly returns an error.

**Fix:** Share `buildScheduler` from the `api` package, or add a package-level helper in `scheduler`. For now: add an error return to the local switch and send an error to the client.

- [ ] **Step 1: Fix handleInit algorithm switch**

Replace the `default:` case:
```go
default:
    s.sendError(conn, fmt.Sprintf("unknown algorithm %q; valid values: fcfs, sjf, srtf, rr, priority, priority_np, cfs, mlfq, lottery, mlq", algorithm))
    return
```

Remove `sched = scheduler.NewFCFSScheduler()` from default.

- [ ] **Step 2: Run tests**

```bash
go test -race ./...
```
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add web/server.go
git commit -m "fix(web): return error for unknown algorithm in WebSocket handleInit (ISSUE-039)"
```

---

## Task 15: Fix generateID millisecond collision (ISSUE-040)

**Files:**
- Modify: `internal/api/api.go:245`

**Root cause:** `time.Format("...150405.000")` has millisecond precision. Concurrent same-algorithm simulations within 1ms get identical IDs.

**Fix:** Add an atomic counter suffix to guarantee uniqueness within a process.

- [ ] **Step 1: Write test in internal/api/api_test.go**

```go
func TestGenerateIDUnique(t *testing.T) {
    t0 := time.Now()
    ids := make(map[string]bool)
    for i := 0; i < 100; i++ {
        id := generateID("fcfs", t0) // same time
        if ids[id] {
            t.Fatalf("duplicate ID generated: %s", id)
        }
        ids[id] = true
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test -race -run TestGenerateIDUnique ./internal/api/
```
Expected: FAIL (all 100 calls return the same ID).

- [ ] **Step 3: Fix generateID in api.go**

Add an atomic counter at package level:
```go
var idCounter atomic.Uint64
```

Update `generateID`:
```go
func generateID(algorithm string, t time.Time) string {
    n := idCounter.Add(1)
    return fmt.Sprintf("%s-%s-%d", algorithm, t.Format("20060102-150405.000"), n)
}
```

Import `"sync/atomic"` and `"fmt"` (already imported).

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/api/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/api.go internal/api/api_test.go
git commit -m "fix(api): append atomic counter to generateID to prevent millisecond-precision collisions (ISSUE-040)"
```

---

## Final Verification

- [ ] Run full test suite with race detector:

```bash
go test -race -count=1 ./...
```
Expected: all packages pass, no races detected.

- [ ] Run linter:

```bash
golangci-lint run ./...
```
Expected: no new errors.

- [ ] Commit ISSUES.md update marking all issues as resolved.

---

## Self-Review

**Spec coverage:** All 15 audit findings (ISSUE-026 through ISSUE-040) have a corresponding task.

**Placeholder scan:** No TBD/TODO; all code blocks are complete.

**Type consistency:** `wsConn`, `wg sync.WaitGroup`, `stepMu sync.Mutex`, `map[*process.Process]int` — names consistent throughout each task.
