# TEST RESULTS

Environment: Go 1.26.1, darwin/arm64 (Apple M3 Pro). All commands run from
the repo root.

## Baseline (before fixes)
```
$ go build ./...           # OK
$ go vet ./...             # clean
$ go test ./...            # ok  internal/simulator  7.956s
```
Baseline had no race-detector run. Running `go test -race` on the original
code would flag ISSUE-001/002/011/012 (data races).

## Final (after fixes)
```
$ go build ./...           # OK
$ go vet ./...             # clean
$ go test -race -count=1 ./...
?   github.com/sanskar/scheduler-simulator/cmd/server      [no test files]
?   github.com/sanskar/scheduler-simulator/internal/process [no test files]
?   github.com/sanskar/scheduler-simulator/internal/scheduler [no test files]
ok  github.com/sanskar/scheduler-simulator/internal/simulator  9.506s
?   github.com/sanskar/scheduler-simulator/web              [no test files]
```
Race detector: **clean**.

## Test inventory

### Existing tests (internal/simulator/simulator_test.go) — all still pass
| Test | Covers |
|---|---|
| TestNewSimulator | Construction + initial state |
| TestAddProcess | Add + sort by arrival |
| TestFCFSScheduling | FCFS order + turnaround |
| TestSJFScheduling | SJF shortest-burst order |
| TestRoundRobinScheduling | RR quantum + context switches |
| TestPriorityScheduling | Non-preemptive priority order |
| TestCFSScheduling | CFS completion + fairness |
| TestSimulationPauseResume | Pause halts time; resume continues |
| TestSimulationReset | Reset clears time/state |
| TestMetricsCalculation | CPU util/throughput/completion |
| TestProcessStates | State transitions on Execute |
| TestGanttChartGeneration | Gantt covers full time span |

### New regression tests (internal/simulator/scheduler_test.go)
| Test | Covers | Issue |
|---|---|---|
| TestSRTFScheduling | SRTF preemption + total time | — |
| TestPriorityPreemptive | Preemptive priority preempts | — |
| TestMLFQDemotion | MLFQ completes + demotion + ctx switches | ISSUE-007 |
| TestSingleProcess | Single-process turnaround == burst | — |
| TestEmptySimulation | No panic / no hang with zero processes | — |
| TestConcurrentAccess | Concurrent GetCurrentState under -race | ISSUE-001/002/011 |
| TestStepFromIdle | Step from idle is synchronous, completes | ISSUE-005 |
| TestResetRestoresProcesses | Reset restores RemainingTime etc. | ISSUE-003 |
| TestRoundRobinQuantum1 | RR q=1 strict alternation | ISSUE-008 |
| TestZeroBurstProcessHandled | Zero-burst completes without hang | ISSUE-024 |
| TestCFSFairness | CFS alternates between equal-weight procs | ISSUE-006 |

### Benchmark tests (internal/simulator/bench_test.go)
See BENCHMARKS.md.

## E2E smoke test (manual harness)
A Python `websockets` client exercised the running server over WebSocket:

1. **Health endpoint** — `GET /health` returns `{"status":"healthy",...}`.
2. **Static files** — `GET /` returns 200.
3. **Malformed input (panic regression)** — sent `{'type':'init','processes':[{'pid':1}]}` (missing arrivalTime) and `{'type':'addProcess','process':{}}`. Before FIX-016 these would panic the server. After: graceful JSON `{"type":"error","message":"Invalid process data: missing field \"arrivalTime\""}`. Server stayed alive.
4. **XSS payload** — process name `<img src=x onerror=alert(1)>`. After FIX-020: completes 1/1, no script execution on render.
5. **Zero-burst process** — burst 0. After FIX-024 documentation: completes 1/1, no hang.
6. **All 8 algorithms** with identical 4-process workload:

| Algorithm | Completed | TotalTime | AvgWait | ContextSwitches |
|---|---|---|---|---|
| fcfs | 4/4 | 26 | 8.75 | 0 |
| sjf | 4/4 | 26 | 7.75 | 0 |
| srtf | 4/4 | 26 | 6.50 | 1 |
| rr (q=2) | 4/4 | 26 | 12.25 | 10 |
| priority (preemptive) | 4/4 | 26 | 7.25 | 2 |
| priority_np | 4/4 | 26 | 8.00 | 0 |
| cfs | 4/4 | 26 | 12.75 | 22 |
| mlfq | 4/4 | 26 | 13.00 | 6 |

All complete the full workload. (SRTF's 1 context switch and CFS's 22 reflect
their preemption characteristics; totals match the expected makespan of 8+4+9+5
= 26 CPU units with last arrival at t=3.)

7. **Graceful shutdown** — SIGTERM → "Shutdown signal received, draining...
   Server stopped" with all WebSocket connections closed and simulator engine
   stopped.

## Coverage of critical flows
- Happy path: all 8 algorithms ✓
- Failure path: malformed input ✓, missing fields ✓, unknown message ✓
- Concurrency: `-race` on full suite ✓, dedicated concurrent-access test ✓
- Edge cases: zero processes ✓, single process ✓, zero-burst ✓, equal-burst
  ties ✓, simultaneous arrivals ✓
- Lifecycle: pause/resume ✓, reset ✓, stop ✓, re-init (goroutine leak) ✓,
  shutdown ✓
- Security: XSS ✓, CSWSH (CheckOrigin) ✓, input validation ✓
