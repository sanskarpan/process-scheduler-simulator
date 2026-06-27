package scheduler

import (
	"testing"

	"github.com/sanskar/scheduler-simulator/internal/process"
)

func TestLotteryDeterministic(t *testing.T) {
	s := NewLotteryScheduler(1, NewRNG(42))
	p1 := process.NewProcess(1, "P1", 0, 10, 0)
	p2 := process.NewProcess(2, "P2", 0, 10, 0)
	ready := []*process.Process{p1, p2}

	count1, count2 := 0, 0
	for i := 0; i < 100; i++ {
		pick := s.Schedule(ready, 0)
		switch pick.PID {
		case 1:
			count1++
		case 2:
			count2++
		}
	}
	if count1 == 0 {
		t.Errorf("P1 was never selected (fairness failure)")
	}
	if count2 == 0 {
		t.Errorf("P2 was never selected (fairness failure)")
	}
	if count1+count2 != 100 {
		t.Errorf("count1+count2 = %d, want 100", count1+count2)
	}
}

func TestLotteryWeightBias(t *testing.T) {
	s := NewLotteryScheduler(1, NewRNG(7))
	pA := process.NewProcess(1, "A", 0, 100, 0)
	pA.Weight = 10
	pB := process.NewProcess(2, "B", 0, 100, 0)
	pB.Weight = 1
	ready := []*process.Process{pA, pB}

	countA := 0
	const draws = 1000
	for i := 0; i < draws; i++ {
		pick := s.Schedule(ready, 0)
		if pick.PID == 1 {
			countA++
		}
	}
	ratio := float64(countA) / float64(draws)
	if ratio <= 0.80 {
		t.Errorf("A picked %d/%d (%.2f), want > 0.80", countA, draws, ratio)
	}
}

func TestLotteryQuantumFor(t *testing.T) {
	s := NewLotteryScheduler(5, nil)
	p := process.NewProcess(1, "P1", 0, 1, 0)
	if q := s.QuantumFor(p); q != 5 {
		t.Errorf("QuantumFor = %d, want 5", q)
	}
}

func TestMLQStrictPriority(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	p1 := process.NewProcess(1, "P1", 0, 2, 0) // level 0 (high)
	p2 := process.NewProcess(2, "P2", 0, 2, 2) // level 2 (low)
	s.AddProcess(p1)
	s.AddProcess(p2)

	ready := []*process.Process{p1, p2}
	pick := s.Schedule(ready, 0)
	if pick.PID != 1 {
		t.Fatalf("Schedule = P%d, want P1 (higher priority)", pick.PID)
	}

	// P1 completes; only P2 remains.
	s.RemoveProcess(p1)
	pick = s.Schedule([]*process.Process{p2}, 0)
	if pick.PID != 2 {
		t.Fatalf("Schedule after P1 completes = P%d, want P2", pick.PID)
	}
}

func TestMLQQuantum(t *testing.T) {
	s := NewMLQScheduler(3, 7)
	p := process.NewProcess(1, "P1", 0, 1, 1)
	if q := s.QuantumFor(p); q != 7 {
		t.Errorf("QuantumFor = %d, want 7", q)
	}
}

func TestMLQPreempt(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	p1 := process.NewProcess(1, "P1", 0, 2, 0) // level 0
	p2 := process.NewProcess(2, "P2", 0, 2, 2) // level 2
	s.AddProcess(p1)
	s.AddProcess(p2)

	if !s.Preempt(p2, []*process.Process{p1}, 0) {
		t.Error("Preempt = false, want true (P1 is higher priority than P2)")
	}
}

func TestMLQClampsPriority(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	pHigh := process.NewProcess(1, "P1", 0, 2, 0)  // level 0
	pLow := process.NewProcess(2, "P2", 0, 2, 99) // clamped to level 2
	s.AddProcess(pHigh)
	s.AddProcess(pLow)

	ready := []*process.Process{pLow, pHigh}
	pick := s.Schedule(ready, 0)
	if pick.PID != 1 {
		t.Fatalf("Schedule = P%d, want P1 (priority 99 should clamp to lowest)", pick.PID)
	}

	// After P1 completes, P2 (clamped to last level) is selected.
	pick = s.Schedule([]*process.Process{pLow}, 0)
	if pick.PID != 2 {
		t.Fatalf("Schedule = P%d, want P2", pick.PID)
	}
}

func TestMLQReset(t *testing.T) {
	s := NewMLQScheduler(3, 4)
	p1 := process.NewProcess(1, "P1", 0, 1, 0)
	p2 := process.NewProcess(2, "P2", 0, 1, 1)
	s.AddProcess(p1)
	s.AddProcess(p2)

	s.Reset()
	// After Reset, levels map is empty. AddProcess re-initializes level for
	// each PID. Verify ordering still works: P1 (level 0) selected before P2.
	s.AddProcess(p1)
	s.AddProcess(p2)
	pick := s.Schedule([]*process.Process{p1, p2}, 0)
	if pick.PID != 1 {
		t.Errorf("after Reset + AddProcess, Schedule = P%d, want P1", pick.PID)
	}
}
