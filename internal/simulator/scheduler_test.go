package simulator

import (
	"sync"
	"testing"
	"time"

	"github.com/sanskar/scheduler-simulator/internal/process"
	"github.com/sanskar/scheduler-simulator/internal/scheduler"
)

// newSim builds a simulator with a fast tick speed for deterministic, quick
// tests.
func newSim(sched scheduler.Scheduler) *Simulator {
	sim := NewSimulator(sched)
	sim.SetSpeed(1)
	return sim
}

// runAndWait starts the simulator and blocks until the update callback signals
// completion or the timeout expires. The callback is installed before Start.
func runAndWait(t *testing.T, sim *Simulator, timeout time.Duration) *SimulationUpdate {
	t.Helper()
	completed := make(chan *SimulationUpdate, 1)
	sim.SetUpdateCallback(func(u *SimulationUpdate) {
		if u.State == SimStateComplete {
			select {
			case completed <- u:
			default:
			}
		}
	})
	sim.Start()
	select {
	case u := <-completed:
		return u
	case <-time.After(timeout):
		t.Fatalf("simulation did not complete within %v", timeout)
		return nil
	}
}

// maxConsecutiveRun returns the longest contiguous Gantt slice for pid.
func maxConsecutiveRun(gantt []process.GanttEntry, pid int) int {
	max := 0
	for _, e := range gantt {
		if e.PID == pid {
			run := e.EndTime - e.StartTime
			if run > max {
				max = run
			}
		}
	}
	return max
}

func TestSRTFScheduling(t *testing.T) {
	sim := newSim(scheduler.NewSRTFScheduler())
	sim.AddProcess(process.NewProcess(1, "P1", 0, 5, 0))
	sim.AddProcess(process.NewProcess(2, "P2", 1, 2, 0))

	runAndWait(t, sim, 5*time.Second)

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 2 {
		t.Fatalf("expected 2 completed, got %d", state.Metrics.CompletedProcesses)
	}
	if state.Metrics.TotalTime != 7 {
		t.Fatalf("expected total time 7, got %d", state.Metrics.TotalTime)
	}

	// Expected Gantt: P1(0-1), P2(1-3), P1(3-7).
	g := state.GanttChart
	if len(g) != 3 {
		t.Fatalf("expected 3 gantt entries, got %d: %+v", len(g), g)
	}
	want := []struct {
		pid, start, end int
	}{
		{1, 0, 1},
		{2, 1, 3},
		{1, 3, 7},
	}
	for i, w := range want {
		if g[i].PID != w.pid || g[i].StartTime != w.start || g[i].EndTime != w.end {
			t.Errorf("entry %d: want P%d(%d-%d), got P%d(%d-%d)",
				i, w.pid, w.start, w.end, g[i].PID, g[i].StartTime, g[i].EndTime)
		}
	}
}

func TestPriorityPreemptive(t *testing.T) {
	sim := newSim(scheduler.NewPriorityScheduler(true))
	sim.AddProcess(process.NewProcess(1, "P1", 0, 4, 2))
	sim.AddProcess(process.NewProcess(2, "P2", 1, 2, 0))

	runAndWait(t, sim, 5*time.Second)

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 2 {
		t.Fatalf("expected 2 completed, got %d", state.Metrics.CompletedProcesses)
	}
	if state.Metrics.TotalTime != 6 {
		t.Fatalf("expected total time 6, got %d", state.Metrics.TotalTime)
	}

	// P2 should have preempted P1: P1(0-1), P2(1-3), P1(3-6).
	g := state.GanttChart
	if len(g) != 3 {
		t.Fatalf("expected 3 gantt entries, got %d: %+v", len(g), g)
	}
	if g[1].PID != 2 || g[1].StartTime != 1 || g[1].EndTime != 3 {
		t.Errorf("expected P2 to run 1-3, got P%d(%d-%d)",
			g[1].PID, g[1].StartTime, g[1].EndTime)
	}
}

func TestMLFQDemotion(t *testing.T) {
	sim := newSim(scheduler.NewMLFQScheduler())
	sim.AddProcess(process.NewProcess(1, "P1", 0, 10, 0))
	sim.AddProcess(process.NewProcess(2, "P2", 0, 2, 0))

	runAndWait(t, sim, 5*time.Second)

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 2 {
		t.Fatalf("expected 2 completed, got %d", state.Metrics.CompletedProcesses)
	}
	if state.Metrics.TotalTime != 12 {
		t.Fatalf("expected total time 12, got %d", state.Metrics.TotalTime)
	}
	if state.Metrics.ContextSwitches < 2 {
		t.Errorf("expected at least 2 context switches, got %d", state.Metrics.ContextSwitches)
	}
}

func TestSingleProcess(t *testing.T) {
	sim := newSim(scheduler.NewFCFSScheduler())
	sim.AddProcess(process.NewProcess(1, "P1", 0, 3, 0))

	runAndWait(t, sim, 5*time.Second)

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 1 {
		t.Fatalf("expected 1 completed, got %d", state.Metrics.CompletedProcesses)
	}
	if state.Metrics.TotalTime != 3 {
		t.Fatalf("expected total time 3, got %d", state.Metrics.TotalTime)
	}
	// Turnaround == burst for a single process arriving at 0.
	if sim.processes[0].TurnaroundTime != sim.processes[0].BurstTime {
		t.Errorf("expected turnaround == burst (%d), got %d",
			sim.processes[0].BurstTime, sim.processes[0].TurnaroundTime)
	}
}

func TestEmptySimulation(t *testing.T) {
	sim := newSim(scheduler.NewFCFSScheduler())
	// Should not panic or hang.
	sim.Start()
	time.Sleep(200 * time.Millisecond)
	sim.Stop()
	_ = sim.GetCurrentState()
}

func TestConcurrentAccess(t *testing.T) {
	sim := newSim(scheduler.NewFCFSScheduler())
	sim.SetSpeed(10)
	sim.AddProcess(process.NewProcess(1, "P1", 0, 100, 0))
	sim.AddProcess(process.NewProcess(2, "P2", 0, 100, 0))
	sim.AddProcess(process.NewProcess(3, "P3", 0, 100, 0))

	sim.Start()

	var wg sync.WaitGroup
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = sim.GetCurrentState()
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(done)
	wg.Wait()
	sim.Stop()
}

func TestStepFromIdle(t *testing.T) {
	sim := newSim(scheduler.NewFCFSScheduler())
	sim.AddProcess(process.NewProcess(1, "P1", 0, 3, 0))

	for i := 0; i < 3; i++ {
		sim.Step()
	}

	state := sim.GetCurrentState()
	if state.CurrentTime != 3 {
		t.Fatalf("expected currentTime 3, got %d", state.CurrentTime)
	}
	if state.State != SimStateComplete {
		t.Errorf("expected state complete, got %v", state.State)
	}
	if state.Metrics.CompletedProcesses != 1 {
		t.Errorf("expected 1 completed, got %d", state.Metrics.CompletedProcesses)
	}

	// Stepping after completion must be a no-op.
	before := sim.GetCurrentState()
	sim.Step()
	after := sim.GetCurrentState()
	if after.CurrentTime != before.CurrentTime {
		t.Errorf("Step after complete should be no-op: %d vs %d",
			before.CurrentTime, after.CurrentTime)
	}
}

func TestResetRestoresProcesses(t *testing.T) {
	sim := newSim(scheduler.NewFCFSScheduler())
	sim.AddProcess(process.NewProcess(1, "P1", 0, 3, 0))

	runAndWait(t, sim, 5*time.Second)
	sim.Reset()

	state := sim.GetCurrentState()
	if state.CurrentTime != 0 {
		t.Errorf("expected currentTime 0 after reset, got %d", state.CurrentTime)
	}
	if state.State != SimStateIdle {
		t.Errorf("expected state idle after reset, got %v", state.State)
	}
	if sim.processes[0].RemainingTime != sim.processes[0].BurstTime {
		t.Errorf("expected remaining == burst after reset (%d), got %d",
			sim.processes[0].BurstTime, sim.processes[0].RemainingTime)
	}
}

func TestRoundRobinQuantum1(t *testing.T) {
	sim := newSim(scheduler.NewRoundRobinScheduler(1))
	sim.AddProcess(process.NewProcess(1, "P1", 0, 3, 0))
	sim.AddProcess(process.NewProcess(2, "P2", 0, 3, 0))

	runAndWait(t, sim, 5*time.Second)

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 2 {
		t.Fatalf("expected 2 completed, got %d", state.Metrics.CompletedProcesses)
	}
	if state.Metrics.TotalTime != 6 {
		t.Fatalf("expected total time 6, got %d", state.Metrics.TotalTime)
	}
	if state.Metrics.ContextSwitches < 4 {
		t.Errorf("expected at least 4 context switches, got %d", state.Metrics.ContextSwitches)
	}
}

func TestZeroBurstProcessHandled(t *testing.T) {
	sim := newSim(scheduler.NewFCFSScheduler())
	sim.AddProcess(process.NewProcess(1, "P1", 0, 0, 0))

	// A zero-burst process is complete from the moment it is created.
	if !sim.processes[0].IsComplete() {
		t.Fatal("expected zero-burst process to be complete at start")
	}

	completed := make(chan struct{}, 1)
	sim.SetUpdateCallback(func(u *SimulationUpdate) {
		if u.State == SimStateComplete {
			select {
			case completed <- struct{}{}:
			default:
			}
		}
	})
	sim.Start()

	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("zero-burst simulation did not complete within 2s")
	}
}

func TestCFSFairness(t *testing.T) {
	sim := newSim(scheduler.NewCFSScheduler())
	sim.AddProcess(process.NewProcess(1, "P1", 0, 4, 0))
	sim.AddProcess(process.NewProcess(2, "P2", 0, 4, 0))

	runAndWait(t, sim, 5*time.Second)

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 2 {
		t.Fatalf("expected 2 completed, got %d", state.Metrics.CompletedProcesses)
	}
	if state.Metrics.TotalTime != 8 {
		t.Fatalf("expected total time 8, got %d", state.Metrics.TotalTime)
	}

	// CFS should interleave the two equal-weight processes; neither may run
	// all 4 units consecutively.
	if maxConsecutiveRun(state.GanttChart, 1) >= 4 {
		t.Errorf("P1 ran consecutively too long; gantt: %+v", state.GanttChart)
	}
	if maxConsecutiveRun(state.GanttChart, 2) >= 4 {
		t.Errorf("P2 ran consecutively too long; gantt: %+v", state.GanttChart)
	}

	// Sanity: both PIDs appear in the Gantt chart.
	seen1, seen2 := false, false
	for _, e := range state.GanttChart {
		if e.PID == 1 {
			seen1 = true
		}
		if e.PID == 2 {
			seen2 = true
		}
	}
	if !seen1 || !seen2 {
		t.Errorf("expected both PIDs in gantt, seen1=%v seen2=%v: %+v", seen1, seen2, state.GanttChart)
	}
}
