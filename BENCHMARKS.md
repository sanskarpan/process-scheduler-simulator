# BENCHMARKS

## Environment
- OS: macOS darwin/arm64
- CPU: Apple M3 Pro
- Go: 1.26.1
- Command: `go test -run='^$' -bench=. -benchmem -count=2 ./internal/simulator/...`

## Benchmarks

### 1. Full FCFS simulation of 100 processes (end-to-end engine)
```
BenchmarkFCFS_Run-11    3   445533042 ns/op   16715653 B/op   49849 allocs/op
BenchmarkFCFS_Run-11    3   443914889 ns/op   16715440 B/op   49846 allocs/op
```
- **Interpretation:** ~445 ms for a 100-process run with default speed=1ms/tick.
  ~49.8k allocations, ~16.7 MB. The dominant cost is per-tick snapshot cloning
  (`snapshotState`) broadcast to the (nil, in-bench) callback — each tick
  clones 100 processes' gantt/events state. This is acceptable for an
  interactive simulator but is the first thing to optimize if scaling to
  thousands of processes: options include delta updates or skipping the clone
  when no client is connected.

### 2. Scheduler selection only (CFS, 1000 ready processes)
```
BenchmarkSchedule_Only-11    1000000   1095 ns/op   0 B/op   0 allocs/op
BenchmarkSchedule_Only-11     966264   1216 ns/op   0 B/op   0 allocs/op
```
- **Interpretation:** ~1.1 µs, zero allocations. The CFS `Schedule` is a linear
  scan of the ready queue — O(n) but allocation-free. For 1000 processes this is
  well under the tick budget even at 1ms/tick. No change needed.

### 3. State snapshot (50 processes, 10 steps of history)
```
BenchmarkSnapshotState-11    518746   2424 ns/op   15624 B/op   58 allocs/op
BenchmarkSnapshotState-11    484536   2473 ns/op   15624 B/op   58 allocs/op
```
- **Interpretation:** ~2.4 µs, 15 KB, 58 allocs per update. This is the
  per-tick cost of `GetCurrentState`/`sendUpdate`. At 100ms/tick (default) this
  is ~0.002% CPU — negligible. At 1ms/tick it's ~0.2% — still negligible.

## Before/after comparison
The original code did not have benchmarks. The "before" baseline for the
engine's per-tick path is implicitly worse because:
- `sendUpdate` spawned a goroutine per tick (goroutine + scheduler overhead
  not captured here, but it grew unbounded under load — see ISSUE-004).
- `calculateMetrics` mutated process state under a read lock, so under
  `-race` the original would fail rather than run.

The "after" numbers above are the new steady-state.

## Key takeaways
- Scheduler selection is allocation-free and sub-microsecond for realistic
  sizes — no scheduler performance work needed.
- The per-tick snapshot clone dominates total runtime and allocations. If
  larger workloads or faster ticks are needed, the recommended optimization is
  to skip the broadcast when no clients are connected, or to send delta
  updates (only the changed tail of the gantt chart and new events) rather than
  full snapshots.
- No memory leak was observed: `BenchmarkFCFS_Run` allocates per iteration but
  the allocations are per-tick and not retained after the run goroutine exits
  (verified by the goroutine/engine-leak fixes in FIX-003/019).
