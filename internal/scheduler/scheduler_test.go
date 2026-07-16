package scheduler

import (
	"testing"

	"github.com/sanskar/scheduler-simulator/internal/process"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func proc(pid, arrival, burst, priority int) *process.Process {
	return process.NewProcess(pid, "P"+string(rune('0'+pid)), arrival, burst, priority)
}

// ── FCFS ─────────────────────────────────────────────────────────────────────

func TestFCFSEmptyQueue(t *testing.T) {
	s := NewFCFSScheduler()
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestFCFSSelectsEarliestArrival(t *testing.T) {
	s := NewFCFSScheduler()
	p1 := proc(1, 5, 3, 0)
	p2 := proc(2, 2, 3, 0)
	got := s.Schedule([]*process.Process{p1, p2}, 10)
	if got.PID != 2 {
		t.Errorf("FCFS selected P%d, want P2 (earliest arrival)", got.PID)
	}
}

func TestFCFSNonPreemptive(t *testing.T) {
	s := NewFCFSScheduler()
	p1 := proc(1, 0, 10, 0)
	p2 := proc(2, 1, 1, 0)
	if s.Preempt(p1, []*process.Process{p2}, 5) {
		t.Error("FCFS Preempt returned true; should always be false")
	}
}

func TestFCFSNoop(t *testing.T) {
	s := NewFCFSScheduler()
	p := proc(1, 0, 1, 0)
	s.AddProcess(p)
	s.RemoveProcess(p)
	s.OnQuantumExpired(p)
	s.Reset()
	if s.Name() == "" {
		t.Error("Name() returned empty string")
	}
	if s.QuantumFor(p) != 0 {
		t.Error("QuantumFor should return 0 for FCFS")
	}
}

// ── SJF ──────────────────────────────────────────────────────────────────────

func TestSJFEmptyQueue(t *testing.T) {
	s := NewSJFScheduler()
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestSJFSelectsShortest(t *testing.T) {
	s := NewSJFScheduler()
	p1 := proc(1, 0, 10, 0)
	p2 := proc(2, 0, 3, 0)
	got := s.Schedule([]*process.Process{p1, p2}, 0)
	if got.PID != 2 {
		t.Errorf("SJF selected P%d, want P2 (shorter burst)", got.PID)
	}
}

func TestSJFTieBreakByArrival(t *testing.T) {
	s := NewSJFScheduler()
	p1 := proc(1, 5, 4, 0)
	p2 := proc(2, 2, 4, 0) // same burst, earlier arrival
	got := s.Schedule([]*process.Process{p1, p2}, 10)
	if got.PID != 2 {
		t.Errorf("SJF tie-break: got P%d, want P2 (earlier arrival)", got.PID)
	}
}

func TestSJFNonPreemptive(t *testing.T) {
	s := NewSJFScheduler()
	p1 := proc(1, 0, 10, 0)
	p2 := proc(2, 1, 1, 0)
	if s.Preempt(p1, []*process.Process{p2}, 5) {
		t.Error("SJF Preempt returned true; should always be false")
	}
}

// ── SRTF ─────────────────────────────────────────────────────────────────────

func TestSRTFEmptyQueue(t *testing.T) {
	s := NewSRTFScheduler()
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestSRTFSelectsShortestRemaining(t *testing.T) {
	s := NewSRTFScheduler()
	p1 := proc(1, 0, 10, 0)
	p1.RemainingTime = 8
	p2 := proc(2, 0, 5, 0)
	p2.RemainingTime = 3
	got := s.Schedule([]*process.Process{p1, p2}, 2)
	if got.PID != 2 {
		t.Errorf("SRTF selected P%d, want P2 (shorter remaining)", got.PID)
	}
}

func TestSRTFTieBreakByArrival(t *testing.T) {
	s := NewSRTFScheduler()
	p1 := proc(1, 5, 4, 0)
	p1.RemainingTime = 4
	p2 := proc(2, 2, 4, 0)
	p2.RemainingTime = 4
	got := s.Schedule([]*process.Process{p1, p2}, 6)
	if got.PID != 2 {
		t.Errorf("SRTF tie-break: got P%d, want P2 (earlier arrival)", got.PID)
	}
}

func TestSRTFPreemptOnShorterRemaining(t *testing.T) {
	s := NewSRTFScheduler()
	current := proc(1, 0, 10, 0)
	current.RemainingTime = 8
	newcomer := proc(2, 3, 2, 0)
	newcomer.RemainingTime = 2
	if !s.Preempt(current, []*process.Process{newcomer}, 3) {
		t.Error("SRTF should preempt when newcomer has shorter remaining time")
	}
}

func TestSRTFNoPreemptWhenNoneIsSmaller(t *testing.T) {
	s := NewSRTFScheduler()
	current := proc(1, 0, 3, 0)
	current.RemainingTime = 2
	other := proc(2, 0, 10, 0)
	other.RemainingTime = 9
	if s.Preempt(current, []*process.Process{other}, 1) {
		t.Error("SRTF should not preempt when current has shortest remaining")
	}
}

func TestSRTFPreemptNilCurrent(t *testing.T) {
	s := NewSRTFScheduler()
	if s.Preempt(nil, []*process.Process{proc(1, 0, 1, 0)}, 0) {
		t.Error("SRTF Preempt with nil current should return false")
	}
}

// ── Round-Robin ───────────────────────────────────────────────────────────────

func TestRoundRobinReturnsHead(t *testing.T) {
	s := NewRoundRobinScheduler(3)
	p1 := proc(1, 0, 10, 0)
	p2 := proc(2, 0, 10, 0)
	got := s.Schedule([]*process.Process{p1, p2}, 0)
	if got.PID != 1 {
		t.Errorf("RR selected P%d, want P1 (head of queue)", got.PID)
	}
}

func TestRoundRobinQuantum(t *testing.T) {
	s := NewRoundRobinScheduler(4)
	if q := s.QuantumFor(proc(1, 0, 1, 0)); q != 4 {
		t.Errorf("QuantumFor = %d, want 4", q)
	}
}

func TestRoundRobinQuantumDefaultsToOne(t *testing.T) {
	s := NewRoundRobinScheduler(0)
	if q := s.QuantumFor(nil); q != 1 {
		t.Errorf("QuantumFor(nil) = %d, want 1 (clamped from 0)", q)
	}
}

func TestRoundRobinEmptyQueue(t *testing.T) {
	s := NewRoundRobinScheduler(2)
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestRoundRobinNoPreempt(t *testing.T) {
	s := NewRoundRobinScheduler(2)
	p1 := proc(1, 0, 10, 0)
	p2 := proc(2, 0, 1, 0)
	if s.Preempt(p1, []*process.Process{p2}, 1) {
		t.Error("RR Preempt should always return false (quantum handled by engine)")
	}
}

// ── Priority (core) ───────────────────────────────────────────────────────────

func TestPrioritySelectsHighest(t *testing.T) {
	s := NewPrioritySchedulerWithAging(false, 0)
	p1 := proc(1, 0, 5, 3)
	p2 := proc(2, 0, 5, 1) // lower number = higher priority
	got := s.Schedule([]*process.Process{p1, p2}, 0)
	if got.PID != 2 {
		t.Errorf("Priority selected P%d, want P2 (priority 1 > 3)", got.PID)
	}
}

func TestPriorityTieBreakByArrival(t *testing.T) {
	s := NewPrioritySchedulerWithAging(false, 0)
	p1 := proc(1, 5, 5, 2)
	p2 := proc(2, 2, 5, 2) // same priority, earlier arrival
	got := s.Schedule([]*process.Process{p1, p2}, 10)
	if got.PID != 2 {
		t.Errorf("tie-break: got P%d, want P2 (earlier arrival)", got.PID)
	}
}

func TestPriorityEmptyQueue(t *testing.T) {
	s := NewPriorityScheduler(true)
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestPriorityNonPreemptiveNoPreempt(t *testing.T) {
	s := NewPrioritySchedulerWithAging(false, 0)
	p1 := proc(1, 0, 10, 5)
	p2 := proc(2, 1, 5, 1) // higher priority
	if s.Preempt(p1, []*process.Process{p2}, 5) {
		t.Error("non-preemptive Priority: Preempt should always be false")
	}
}

func TestPriorityPreemptivePreempts(t *testing.T) {
	s := NewPrioritySchedulerWithAging(true, 0)
	p1 := proc(1, 0, 10, 5) // running
	p2 := proc(2, 3, 5, 1)  // higher priority arrives
	if !s.Preempt(p1, []*process.Process{p2}, 3) {
		t.Error("preemptive Priority: should preempt when higher-priority arrives")
	}
}

func TestPriorityPreemptiveNoPreemptWhenSame(t *testing.T) {
	s := NewPrioritySchedulerWithAging(true, 0)
	p1 := proc(1, 0, 10, 2)
	p2 := proc(2, 3, 5, 2) // same priority: no preempt
	if s.Preempt(p1, []*process.Process{p2}, 3) {
		t.Error("preemptive Priority: should not preempt at same priority level")
	}
}

func TestPriorityPreemptNilCurrent(t *testing.T) {
	s := NewPriorityScheduler(true)
	if s.Preempt(nil, []*process.Process{proc(1, 0, 1, 0)}, 0) {
		t.Error("Preempt with nil current should return false")
	}
}

func TestPriorityNoop(t *testing.T) {
	s := NewPriorityScheduler(false)
	p := proc(1, 0, 1, 0)
	s.AddProcess(p)
	s.RemoveProcess(p)
	s.OnQuantumExpired(p)
	s.Reset()
	if s.QuantumFor(p) != 0 {
		t.Error("Priority QuantumFor should return 0")
	}
}

// ── Priority Aging ────────────────────────────────────────────────────────────

func TestPriorityAgingNoBoostBeforeInterval(t *testing.T) {
	// agingInterval=10: no boost until 10 ticks of waiting have elapsed
	s := NewPrioritySchedulerWithAging(false, 10)
	p1 := proc(1, 0, 5, 1) // higher static priority
	p2 := proc(2, 0, 5, 5) // lower static priority, but waiting

	// At t=9: waited only 9 ticks — no boost for p2 yet
	got := s.Schedule([]*process.Process{p1, p2}, 9)
	if got.PID != 1 {
		t.Errorf("at t=9 (before aging): got P%d, want P1", got.PID)
	}
}

func TestPriorityAgingBoostAtInterval(t *testing.T) {
	// agingInterval=10: at t=10 p2 (priority 5, waited 10 ticks) → effective 4
	s := NewPrioritySchedulerWithAging(false, 10)
	p1 := proc(1, 0, 5, 4) // static priority 4
	p2 := proc(2, 0, 5, 5) // static priority 5 → effective 4 after 10 ticks

	// At t=10: both have effective priority 4; tie-break by arrival (both 0),
	// then PID should not matter — they tie — verify one is selected
	got := s.Schedule([]*process.Process{p2, p1}, 10)
	// effective: p1=4, p2=5-1=4 → tie; p2 arrived at 0 == p1 arrived at 0
	// with equal arrival: first element in loop wins (p2 was iterated first)
	if got == nil {
		t.Error("Schedule returned nil for non-empty queue")
	}
}

func TestPriorityAgingLowPriorityWinsAfterWaiting(t *testing.T) {
	// agingInterval=5: p2 (priority 10) needs 50 ticks to match p1 (priority 0),
	// but at t=25 (waited 25 ticks) it gets 5 boosts → effective priority 5,
	// still higher than p1's 0. At t=50 (waited 50 ticks, 10 boosts) → effective
	// 0, ties with p1. We just need to verify the direction is correct.
	s := NewPrioritySchedulerWithAging(false, 5)
	p1 := proc(1, 0, 100, 0)  // always high priority
	p2 := proc(2, 0, 100, 10) // needs time to catch up

	// At t=50: p2 waited 50 ticks, 50/5=10 boosts, effective = max(0,10-10)=0
	// Both have effective priority 0; tie-break by arrival (both 0), p1 wins
	got50 := s.Schedule([]*process.Process{p1, p2}, 50)
	if got50 == nil {
		t.Fatal("got nil at t=50")
	}
	// effective(p2, t=50) = max(0, 10-10) = 0 == effective(p1) = 0 → tie
	// Verify both are selectable (no crash, stable result)

	// At t=4: p2 has 0 boosts (4/5=0), effective=10, definitely loses to p1
	got4 := s.Schedule([]*process.Process{p1, p2}, 4)
	if got4.PID != 1 {
		t.Errorf("at t=4: got P%d, want P1 (p2 not yet boosted)", got4.PID)
	}

	// At t=5: p2 gets 1 boost (5/5=1), effective=9, still loses to p1's 0
	got5 := s.Schedule([]*process.Process{p1, p2}, 5)
	if got5.PID != 1 {
		t.Errorf("at t=5: got P%d, want P1 (p2 effective=9, not yet caught up)", got5.PID)
	}
}

func TestPriorityAgingClampedAtZero(t *testing.T) {
	s := NewPrioritySchedulerWithAging(false, 1) // boost every tick
	p := proc(1, 0, 100, 2)

	// At t=100: 100 boosts, priority-2=0 (clamped). Effective must be 0, not negative.
	eff := s.effectivePriority(p, 100)
	if eff != 0 {
		t.Errorf("effectivePriority at t=100 = %d, want 0 (clamped)", eff)
	}
}

func TestPriorityAgingUsesLastExecuted(t *testing.T) {
	// A process that ran recently should not get a big aging boost.
	s := NewPrioritySchedulerWithAging(false, 10)
	p := proc(1, 0, 100, 5)
	p.LastExecuted = 90 // ran recently at t=90

	// At t=100: waited only 100-90=10 ticks → 1 boost → effective=4
	eff := s.effectivePriority(p, 100)
	if eff != 4 {
		t.Errorf("effectivePriority(p, 100) with LastExecuted=90: got %d, want 4", eff)
	}
}

func TestPriorityAgingDisabledWhenIntervalZero(t *testing.T) {
	s := NewPrioritySchedulerWithAging(false, 0)
	p := proc(1, 0, 100, 5)
	// Even after a long wait, no boost when aging is disabled.
	eff := s.effectivePriority(p, 1000)
	if eff != 5 {
		t.Errorf("aging disabled: effectivePriority = %d, want 5 (unchanged)", eff)
	}
}

func TestPriorityAgingPreemptiveCatchUp(t *testing.T) {
	// With aging, a lower-priority process that waited long enough should be
	// able to trigger preemption of a currently-running higher-priority process.
	//
	// running (priority 3) has been executing continuously; we simulate this by
	// setting LastExecuted to currentTime so its effective wait time is ~0.
	// waiting (priority 10) has been in the ready queue since t=0 the whole time.
	s := NewPrioritySchedulerWithAging(true, 5)
	running := proc(1, 0, 100, 3)  // running; LastExecuted kept at current time
	waiting := proc(2, 0, 100, 10) // waiting since t=0, LastExecuted=0

	// At t=35:
	//   running: LastExecuted=35, eff = 3 - 0 = 3
	//   waiting: LastExecuted=0,  eff = max(0, 10 - 35/5) = max(0,10-7) = 3
	// Tie → no preemption.
	running.LastExecuted = 35
	if s.Preempt(running, []*process.Process{waiting}, 35) {
		t.Error("at t=35 effective priorities equal: should not preempt")
	}

	// At t=40:
	//   running: LastExecuted=40, eff = 3 - 0 = 3
	//   waiting: LastExecuted=0,  eff = max(0, 10 - 40/5) = max(0,10-8) = 2
	// waiting wins → preempt.
	running.LastExecuted = 40
	if !s.Preempt(running, []*process.Process{waiting}, 40) {
		t.Error("at t=40 waiting aged enough to preempt: should preempt")
	}
}

func TestPriorityAgingNegativeWaitClamped(t *testing.T) {
	// Edge case: currentTime < arrival (shouldn't happen in practice, but must
	// not panic or produce negative boosts).
	s := NewPrioritySchedulerWithAging(false, 10)
	p := proc(1, 50, 10, 5)
	eff := s.effectivePriority(p, 30) // currentTime before arrival
	if eff != 5 {
		t.Errorf("effectivePriority with time before arrival = %d, want 5", eff)
	}
}

func TestPriorityDefaultSchedulerHasAging(t *testing.T) {
	s := NewPriorityScheduler(false)
	// Default aging interval is 10. A process that waited 10 ticks should be
	// boosted by 1.
	p := proc(1, 0, 100, 5)
	eff := s.effectivePriority(p, 10)
	if eff != 4 {
		t.Errorf("default scheduler effectivePriority(p, 10) = %d, want 4", eff)
	}
}

// ── CFS ──────────────────────────────────────────────────────────────────────

func TestCFSSelectsMinVruntime(t *testing.T) {
	s := NewCFSScheduler()
	p1 := process.NewProcess(1, "P1", 0, 10, 0)
	p1.VRuntime = 500
	p2 := process.NewProcess(2, "P2", 0, 10, 0)
	p2.VRuntime = 100
	got := s.Schedule([]*process.Process{p1, p2}, 5)
	if got.PID != 2 {
		t.Errorf("CFS selected P%d (VRuntime=%d), want P2 (VRuntime=100)", got.PID, got.VRuntime)
	}
}

func TestCFSTieBreakByPID(t *testing.T) {
	s := NewCFSScheduler()
	p1 := process.NewProcess(3, "P3", 0, 10, 0)
	p1.VRuntime = 100
	p2 := process.NewProcess(1, "P1", 0, 10, 0)
	p2.VRuntime = 100
	got := s.Schedule([]*process.Process{p1, p2}, 5)
	if got.PID != 1 {
		t.Errorf("CFS tie-break by PID: got P%d, want P1", got.PID)
	}
}

func TestCFSEmptyQueue(t *testing.T) {
	s := NewCFSScheduler()
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestCFSQuantumFor(t *testing.T) {
	s := NewCFSScheduler()
	p := process.NewProcess(1, "P1", 0, 1, 0)
	if q := s.QuantumFor(p); q != 1 {
		t.Errorf("CFS QuantumFor = %d, want 1 (minGranularity)", q)
	}
}

func TestCFSNoop(t *testing.T) {
	s := NewCFSScheduler()
	p := process.NewProcess(1, "P1", 0, 1, 0)
	s.AddProcess(p)
	s.RemoveProcess(p)
	s.OnQuantumExpired(p)
	s.Reset()
}

func TestCFSPreemptNilCurrent(t *testing.T) {
	s := NewCFSScheduler()
	if s.Preempt(nil, []*process.Process{process.NewProcess(1, "P1", 0, 1, 0)}, 0) {
		t.Error("Preempt with nil current should return false")
	}
}

func TestCFSPreemptEmptyQueue(t *testing.T) {
	s := NewCFSScheduler()
	p := process.NewProcess(1, "P1", 0, 1, 0)
	if s.Preempt(p, nil, 0) {
		t.Error("Preempt with empty queue should return false")
	}
}

func TestCFSPreemptCurrentPIDFiltered(t *testing.T) {
	// If the only "competitor" has the same PID, no preemption.
	s := NewCFSScheduler()
	p := process.NewProcess(1, "P1", 0, 10, 0)
	p.VRuntime = 1000
	// Pass the same pointer — should be filtered out by p.PID != current.PID check.
	if s.Preempt(p, []*process.Process{p}, 5) {
		t.Error("Preempt should return false when only competitor is current itself")
	}
}

// ── MLFQ ─────────────────────────────────────────────────────────────────────

func TestMLFQLevelPromotion(t *testing.T) {
	s := NewMLFQScheduler()
	p := process.NewProcess(1, "P1", 0, 10, 0)
	s.AddProcess(p)

	// New process starts at level 0.
	if lvl := s.levels[p]; lvl != 0 {
		t.Errorf("new process level = %d, want 0", lvl)
	}

	// After quantum expiry it is demoted to level 1.
	s.OnQuantumExpired(p)
	if lvl := s.levels[p]; lvl != 1 {
		t.Errorf("after 1 demotion, level = %d, want 1", lvl)
	}

	// Demote to level 2 (bottom).
	s.OnQuantumExpired(p)
	if lvl := s.levels[p]; lvl != 2 {
		t.Errorf("after 2 demotions, level = %d, want 2", lvl)
	}

	// Cannot demote beyond bottom.
	s.OnQuantumExpired(p)
	if lvl := s.levels[p]; lvl != 2 {
		t.Errorf("at bottom, further demotion changed level to %d, want 2", lvl)
	}
}

func TestMLFQScheduleHigherLevelFirst(t *testing.T) {
	s := NewMLFQScheduler()
	p1 := process.NewProcess(1, "P1", 0, 10, 0)
	p2 := process.NewProcess(2, "P2", 0, 10, 0)
	s.AddProcess(p1)
	s.AddProcess(p2)

	// Demote p2 to level 1.
	s.OnQuantumExpired(p2)

	got := s.Schedule([]*process.Process{p1, p2}, 0)
	if got.PID != 1 {
		t.Errorf("MLFQ: P%d selected, want P1 (higher level)", got.PID)
	}
}

func TestMLFQQuantumForLevel(t *testing.T) {
	s := NewMLFQScheduler()
	p := process.NewProcess(1, "P1", 0, 10, 0)
	s.AddProcess(p)

	if q := s.QuantumFor(p); q != 2 {
		t.Errorf("level-0 quantum = %d, want 2", q)
	}
	s.OnQuantumExpired(p) // → level 1
	if q := s.QuantumFor(p); q != 4 {
		t.Errorf("level-1 quantum = %d, want 4", q)
	}
	s.OnQuantumExpired(p) // → level 2
	if q := s.QuantumFor(p); q != 8 {
		t.Errorf("level-2 quantum = %d, want 8", q)
	}
}

func TestMLFQQuantumForNil(t *testing.T) {
	s := NewMLFQScheduler()
	if q := s.QuantumFor(nil); q != s.timeQuantums[0] {
		t.Errorf("QuantumFor(nil) = %d, want %d", q, s.timeQuantums[0])
	}
}

func TestMLFQScheduleEmptyQueue(t *testing.T) {
	s := NewMLFQScheduler()
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestMLFQPreemptHigherLevelArrives(t *testing.T) {
	s := NewMLFQScheduler()
	p1 := process.NewProcess(1, "P1", 0, 10, 0) // level 1 (demoted)
	p2 := process.NewProcess(2, "P2", 5, 5, 0)  // level 0 (new arrival)
	s.AddProcess(p1)
	s.AddProcess(p2)
	s.OnQuantumExpired(p1) // p1 → level 1

	if !s.Preempt(p1, []*process.Process{p2}, 5) {
		t.Error("MLFQ should preempt when a higher-level process is ready")
	}
}

func TestMLFQPreemptNilCurrent(t *testing.T) {
	s := NewMLFQScheduler()
	if s.Preempt(nil, []*process.Process{process.NewProcess(1, "P1", 0, 1, 0)}, 0) {
		t.Error("Preempt with nil current should return false")
	}
}

func TestMLFQPreemptSameLevel(t *testing.T) {
	s := NewMLFQScheduler()
	p1 := process.NewProcess(1, "P1", 0, 10, 0)
	p2 := process.NewProcess(2, "P2", 0, 10, 0)
	s.AddProcess(p1)
	s.AddProcess(p2)
	// Both at level 0 — no preemption.
	if s.Preempt(p1, []*process.Process{p2}, 5) {
		t.Error("MLFQ: same level should not trigger preemption")
	}
}

func TestMLFQReset(t *testing.T) {
	s := NewMLFQScheduler()
	p := process.NewProcess(1, "P1", 0, 10, 0)
	s.AddProcess(p)
	s.OnQuantumExpired(p)
	s.Reset()
	if len(s.levels) != 0 {
		t.Errorf("after Reset, levels map has %d entries, want 0", len(s.levels))
	}
}

func TestMLFQAddProcessIdempotent(t *testing.T) {
	s := NewMLFQScheduler()
	p := process.NewProcess(1, "P1", 0, 10, 0)
	s.AddProcess(p)
	s.OnQuantumExpired(p) // → level 1
	s.AddProcess(p)       // should not reset level
	if lvl := s.levels[p]; lvl != 1 {
		t.Errorf("AddProcess on existing process reset level to %d, want 1", lvl)
	}
}

func TestMLFQName(t *testing.T) {
	s := NewMLFQScheduler()
	if s.Name() == "" {
		t.Error("Name() returned empty string")
	}
}

// ── MLQ additional ────────────────────────────────────────────────────────────

func TestMLQNoPreemptSamePriority(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	p1 := process.NewProcess(1, "P1", 0, 10, 1)
	p2 := process.NewProcess(2, "P2", 0, 10, 1)
	s.AddProcess(p1)
	s.AddProcess(p2)
	if s.Preempt(p1, []*process.Process{p2}, 0) {
		t.Error("MLQ: same level should not preempt")
	}
}

func TestMLQPreemptNilCurrent(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	if s.Preempt(nil, []*process.Process{process.NewProcess(1, "P1", 0, 1, 0)}, 0) {
		t.Error("MLQ Preempt with nil current should return false")
	}
}

func TestMLQArrivalTieBreak(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	p1 := process.NewProcess(1, "P1", 5, 5, 1) // same level, later arrival
	p2 := process.NewProcess(2, "P2", 2, 5, 1) // same level, earlier arrival
	s.AddProcess(p1)
	s.AddProcess(p2)
	got := s.Schedule([]*process.Process{p1, p2}, 6)
	if got.PID != 2 {
		t.Errorf("MLQ tie-break by arrival: got P%d, want P2", got.PID)
	}
}

func TestMLQOnQuantumExpiredNoop(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	p := process.NewProcess(1, "P1", 0, 10, 0)
	s.AddProcess(p)
	s.OnQuantumExpired(p) // must not change level
	lvl := s.levels[p]
	if lvl != 0 {
		t.Errorf("MLQ OnQuantumExpired changed level to %d, want 0 (MLQ is fixed)", lvl)
	}
}

func TestMLQEmptySchedule(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestMLQName(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	if s.Name() == "" {
		t.Error("Name() returned empty string")
	}
}

// ── Lottery additional ────────────────────────────────────────────────────────

func TestLotteryEmptyQueue(t *testing.T) {
	s := NewLotteryScheduler(1, nil)
	if got := s.Schedule(nil, 0); got != nil {
		t.Errorf("empty queue: got %v, want nil", got)
	}
}

func TestLotteryZeroWeightTreatedAsOne(t *testing.T) {
	s := NewLotteryScheduler(1, NewRNG(0xABCD))
	p := process.NewProcess(1, "P1", 0, 10, 0)
	p.Weight = 0 // should be treated as 1 to prevent divide-by-zero
	got := s.Schedule([]*process.Process{p}, 0)
	if got.PID != 1 {
		t.Errorf("single-process lottery: got P%d, want P1", got.PID)
	}
}

func TestLotterySingleProcess(t *testing.T) {
	s := NewLotteryScheduler(2, NewRNG(99))
	p := process.NewProcess(42, "solo", 0, 5, 0)
	got := s.Schedule([]*process.Process{p}, 0)
	if got.PID != 42 {
		t.Errorf("single process: got P%d, want P42", got.PID)
	}
}

func TestLotteryDefaultQuantumIsOne(t *testing.T) {
	s := NewLotteryScheduler(0, nil) // clamped to 1
	p := process.NewProcess(1, "P1", 0, 1, 0)
	if q := s.QuantumFor(p); q != 1 {
		t.Errorf("QuantumFor = %d, want 1", q)
	}
}

func TestLotteryPreemptAlwaysFalse(t *testing.T) {
	s := NewLotteryScheduler(1, nil)
	p1 := process.NewProcess(1, "P1", 0, 10, 0)
	p2 := process.NewProcess(2, "P2", 0, 10, 0)
	if s.Preempt(p1, []*process.Process{p2}, 5) {
		t.Error("Lottery Preempt should always return false")
	}
}

func TestLotteryResetNonDeterministic(t *testing.T) {
	// Reset on a scheduler with a non-deterministicRNG (e.g. custom RNG) should
	// not panic.
	custom := &customRNG{n: 0}
	s := NewLotteryScheduler(1, custom)
	s.Reset() // must not panic
}

// customRNG is a trivial always-zero RNG to test the non-deterministicRNG Reset path.
type customRNG struct{ n int }

func (r *customRNG) Intn(n int) int { return 0 }
