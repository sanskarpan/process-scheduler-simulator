# Scheduling Algorithms

The simulator implements 10 CPU scheduling algorithms. Each section describes
the selection logic, time complexity, whether preemption occurs, and known
behavioral characteristics.

---

## Algorithm IDs

| ID | Name | Preemptive |
|----|------|-----------|
| `fcfs` | First-Come, First-Served | No |
| `sjf` | Shortest Job First | No |
| `srtf` | Shortest Remaining Time First | Yes |
| `rr` | Round Robin | Yes (quantum-based) |
| `priority` | Priority (preemptive) | Yes |
| `priority_np` | Priority (non-preemptive) | No |
| `cfs` | Completely Fair Scheduler | Yes |
| `mlfq` | Multi-Level Feedback Queue | Yes |
| `lottery` | Lottery Scheduling | Yes (quantum-based) |
| `mlq` | Multi-Level Queue | Yes (between queues) |

---

## FCFS — First-Come, First-Served

**Algorithm:** Processes execute in arrival order. No sorting, no priority.
The process that arrived earliest is always first in the ready queue.

**Selection:** `O(n)` scan — returns the process with the lowest `ArrivalTime`
among ready processes.

**Preemption:** None. Once a process starts, it runs to completion.

**Starvation risk:** None. All processes eventually run.

**Characteristics:**
- Convoy effect: a CPU-bound process delays all shorter processes behind it.
- Best for batch workloads with similar burst lengths.
- Average waiting time is sensitive to arrival order.

---

## SJF — Shortest Job First

**Algorithm:** Among all ready processes, the one with the shortest `BurstTime`
(original burst, not remaining) is selected.

**Selection:** `O(n)` scan.

**Preemption:** None. The shortest *next* job is chosen at each scheduling
decision point (when the CPU becomes free), but the current job is not
interrupted.

**Starvation risk:** High. Long processes can starve if short processes keep
arriving.

**Characteristics:**
- Optimal average waiting time for a given set of non-preemptive processes.
- Requires knowledge of burst times in advance (unrealistic in practice; useful
  as a theoretical benchmark).

---

## SRTF — Shortest Remaining Time First

**Algorithm:** Preemptive version of SJF. At every tick, the ready process with
the smallest `RemainingTime` is selected. If a new arrival has less remaining
time than the current process, preemption occurs immediately.

**Selection:** `O(n)` scan per tick.

**Preemption:** Yes. Preempts the current process when a shorter-remaining
process arrives.

**Starvation risk:** High. Long jobs can be indefinitely displaced by shorter
arrivals.

**Characteristics:**
- Provably optimal average waiting time among preemptive algorithms (for known
  burst lengths).
- Context-switch overhead can be high in practice.

---

## RR — Round Robin

**Algorithm:** Processes are served in a circular queue. Each gets a fixed time
quantum; when the quantum expires, the current process is moved to the back of
the queue and the next process runs.

**Selection:** `O(1)` — head of the queue.

**Preemption:** Yes, on quantum expiry.

**Parameters:** `timeQuantum` (default: 4 ticks; passed via `init.timeQuantum`).

**Starvation risk:** None. Every process gets equal CPU slices in turn.

**Characteristics:**
- Fairness by construction.
- Larger quantum → approaches FCFS behavior.
- Smaller quantum → lower response time but higher context-switch overhead.
- Response time is bounded by `(n-1) × quantum` for n processes.

---

## Priority (Preemptive)

**ID:** `priority`

**Algorithm:** The ready process with the lowest `Priority` number (highest
importance) runs. A newly arrived process with a higher priority than the
current one immediately preempts it.

**Selection:** `O(n)` scan using `effectivePriority()` (see Aging below).

**Preemption:** Yes. Any arrival or completion that changes the highest-priority
ready process triggers preemption.

**Starvation risk:** Yes, for low-priority processes. Mitigated by aging.

### Priority Aging

Both priority schedulers support optional aging via
`NewPrioritySchedulerWithAging(preemptive, agingInterval)`.

Effective priority is computed statlessly at schedule time:

```
boostFactor   = (currentTime - max(lastExecuted, arrivalTime)) / agingInterval
effectivePri  = max(0, basePriority - boostFactor)
```

A process that has been waiting longer receives a lower effective priority
number, i.e., a higher effective urgency. This prevents indefinite starvation.
No mutable state is needed; the computation is purely derived from timestamps.

---

## Priority (Non-Preemptive)

**ID:** `priority_np`

Same selection logic as the preemptive variant, but the current process is
never interrupted mid-burst. The highest-priority process is chosen only when
the CPU becomes free.

---

## CFS — Completely Fair Scheduler

**Algorithm:** Each process tracks a virtual runtime (`VRuntime`). At every
scheduling point, the process with the smallest `VRuntime` is selected. When a
process runs for one tick, its `VRuntime` increases by `1 / nReady` (inverse
of the number of ready processes), which simulates proportional-share fairness.

**Selection:** `O(n)` scan (production Linux uses a red-black tree for `O(log n)`).

**Preemption:** Yes. The process with the minimum `VRuntime` is compared
against the current at each tick; the current is preempted if it is no longer
the minimum. A minimum granularity prevents excessive context switches.

**Parameters:** `minGranularity` (internal constant, currently 1 tick).

**Starvation risk:** None. `VRuntime` eventually catches up for any runnable
process.

**Characteristics:**
- Approximates ideal processor sharing.
- Priority-unaware in this implementation (all processes treated equally).
- `VRuntime` is reset when a process becomes ready again after I/O so that
  I/O-bound processes aren't penalized.

---

## MLFQ — Multi-Level Feedback Queue

**Algorithm:** Three queues with quanta of 1, 2, and 4 ticks (highest to
lowest priority). A new process enters queue 0. When it exhausts its quantum,
it is demoted to the next queue. Processes in lower queues only run when all
higher queues are empty.

**Selection:** `O(n)` — scan queues top-down, return first process in the
highest non-empty queue.

**Preemption:** Yes. A process running from queue 2 is immediately preempted
when a process arrives in queue 0 or 1.

**Starvation risk:** Low. Periodic priority boost (every 100 ticks) resets all
processes back to queue 0, preventing indefinite starvation of long-running processes.

**Characteristics:**
- Interactive (short) processes naturally stay in high-priority queues.
- CPU-bound processes migrate to lower queues over time.
- Approximates SJF without knowing burst times in advance.

---

## Lottery Scheduling

**Algorithm:** Each process holds a number of tickets proportional to its
`Priority` (or 1 ticket if priority is 0). At each scheduling point, a random
ticket number is drawn; the process holding that ticket wins the CPU.

**Selection:** `O(n)` — sum all tickets, draw a random number, scan processes
until the cumulative ticket count exceeds the draw.

**Preemption:** Yes, on quantum expiry (same as Round Robin).

**Parameters:** `timeQuantum` (same as RR), `RNG` interface (injectable for
deterministic tests).

**Ticket allocation:**
- If all priorities are 0, each process gets 1 ticket (uniform distribution).
- Higher `Priority` number → more tickets → higher probability of selection.

**Starvation risk:** Probabilistic near-zero. A process with at least 1 ticket
will eventually be scheduled.

**Characteristics:**
- Probabilistic fairness: over many rounds, each process gets CPU proportional
  to its ticket count.
- No deterministic ordering — useful for studying probabilistic behavior.
- The deterministic `RNG` (XorShift64, injectable seed) makes tests reproducible.

---

## MLQ — Multi-Level Queue

**Algorithm:** A fixed number of queues (default: 3) with strict priority
ordering. Queue 0 is highest priority. Processes are assigned to a queue based
on their `Priority` value:

```
queueIndex = min(priority, numLevels-1)
```

Within each queue, Round Robin scheduling is used (shared quantum across all
queues).

**Selection:** `O(n)` — find the highest non-empty queue, select the Round
Robin head within that queue.

**Preemption:** Yes. A higher-priority queue becoming non-empty immediately
preempts the current process.

**Starvation risk:** High for low-priority queues if high-priority queues are
never empty.

**Characteristics:**
- Statically partitioned — a process cannot change queues.
- Suitable for workloads with clearly distinct priority classes (e.g.,
  system vs. interactive vs. batch processes).
- Unlike MLFQ, there is no automatic demotion or promotion.

---

## Choosing an Algorithm

| Goal | Recommended |
|------|-------------|
| Minimize average waiting time (known bursts) | `sjf` / `srtf` |
| Equal fairness with bounded response time | `rr` |
| Priority-aware with starvation protection | `priority` (with aging) |
| Approximate SJF without knowing bursts | `mlfq` |
| Linux-style proportional sharing | `cfs` |
| Proportional share by weight | `lottery` |
| Fixed-class workloads | `mlq` |
| Simplest, arrival-order processing | `fcfs` |
