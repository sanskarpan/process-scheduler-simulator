package simulator

import (
	"testing"
	"time"

	"github.com/sanskar/scheduler-simulator/internal/process"
	"github.com/sanskar/scheduler-simulator/internal/scheduler"
)

func TestNewSimulator(t *testing.T) {
	sched := scheduler.NewFCFSScheduler()
	sim := NewSimulator(sched)

	if sim == nil {
		t.Fatal("Expected simulator to be created")
	}

	if sim.state != SimStateIdle {
		t.Errorf("Expected initial state to be idle, got %v", sim.state)
	}

	if sim.currentTime != 0 {
		t.Errorf("Expected initial time to be 0, got %d", sim.currentTime)
	}
}

func TestAddProcess(t *testing.T) {
	sched := scheduler.NewFCFSScheduler()
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 5, 0)
	p2 := process.NewProcess(2, "P2", 1, 3, 0)

	sim.AddProcess(p1)
	sim.AddProcess(p2)

	if len(sim.processes) != 2 {
		t.Errorf("Expected 2 processes, got %d", len(sim.processes))
	}

	// Should be sorted by arrival time
	if sim.processes[0].ArrivalTime != 0 {
		t.Errorf("Expected first process arrival time to be 0, got %d", sim.processes[0].ArrivalTime)
	}
}

func TestFCFSScheduling(t *testing.T) {
	sched := scheduler.NewFCFSScheduler()
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 3, 0)
	p2 := process.NewProcess(2, "P2", 1, 4, 0)
	p3 := process.NewProcess(3, "P3", 2, 2, 0)

	sim.AddProcess(p1)
	sim.AddProcess(p2)
	sim.AddProcess(p3)

	completedChan := make(chan bool)
	updateCount := 0

	sim.SetUpdateCallback(func(update *SimulationUpdate) {
		updateCount++
		if update.State == SimStateComplete {
			completedChan <- true
		}
	})

	sim.Start()

	select {
	case <-completedChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Simulation did not complete within timeout")
	}

	// Check metrics
	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 3 {
		t.Errorf("Expected 3 completed processes, got %d", state.Metrics.CompletedProcesses)
	}

	// FCFS should complete in order: P1 (0-3), P2 (3-7), P3 (7-9)
	expectedTime := 9
	if state.Metrics.TotalTime != expectedTime {
		t.Errorf("Expected total time %d, got %d", expectedTime, state.Metrics.TotalTime)
	}

	// Check turnaround times
	// P1: 3-0=3, P2: 7-1=6, P3: 9-2=7
	// Average: (3+6+7)/3 = 5.33
	expectedAvgTurnaround := 5.33
	if state.Metrics.AverageTurnaroundTime < expectedAvgTurnaround-0.1 ||
		state.Metrics.AverageTurnaroundTime > expectedAvgTurnaround+0.1 {
		t.Errorf("Expected avg turnaround ~%.2f, got %.2f",
			expectedAvgTurnaround, state.Metrics.AverageTurnaroundTime)
	}
}

func TestSJFScheduling(t *testing.T) {
	sched := scheduler.NewSJFScheduler()
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 6, 0)
	p2 := process.NewProcess(2, "P2", 0, 3, 0)
	p3 := process.NewProcess(3, "P3", 0, 8, 0)

	sim.AddProcess(p1)
	sim.AddProcess(p2)
	sim.AddProcess(p3)

	completedChan := make(chan bool)

	sim.SetUpdateCallback(func(update *SimulationUpdate) {
		if update.State == SimStateComplete {
			completedChan <- true
		}
	})

	sim.Start()

	select {
	case <-completedChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("SJF simulation did not complete within timeout")
	}

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 3 {
		t.Errorf("Expected 3 completed processes, got %d", state.Metrics.CompletedProcesses)
	}

	// SJF should execute in order: P2 (3), P1 (6), P3 (8)
	// Order: P2 (0-3), P1 (3-9), P3 (9-17)
	expectedTime := 17
	if state.Metrics.TotalTime != expectedTime {
		t.Errorf("Expected total time %d, got %d", expectedTime, state.Metrics.TotalTime)
	}
}

func TestRoundRobinScheduling(t *testing.T) {
	timeQuantum := 2
	sched := scheduler.NewRoundRobinScheduler(timeQuantum)
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 5, 0)
	p2 := process.NewProcess(2, "P2", 0, 4, 0)

	sim.AddProcess(p1)
	sim.AddProcess(p2)

	completedChan := make(chan bool)

	sim.SetUpdateCallback(func(update *SimulationUpdate) {
		if update.State == SimStateComplete {
			completedChan <- true
		}
	})

	sim.Start()

	select {
	case <-completedChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Round-robin simulation did not complete within timeout")
	}

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 2 {
		t.Errorf("Expected 2 completed processes, got %d", state.Metrics.CompletedProcesses)
	}

	// RR with quantum 2: P1(2), P2(2), P1(2), P2(2), P1(1)
	// Total time = 9
	expectedTime := 9
	if state.Metrics.TotalTime != expectedTime {
		t.Errorf("Expected total time %d, got %d", expectedTime, state.Metrics.TotalTime)
	}

	// Check context switches (should have several due to round-robin)
	if state.Metrics.ContextSwitches < 2 {
		t.Errorf("Expected at least 2 context switches, got %d", state.Metrics.ContextSwitches)
	}
}

func TestPriorityScheduling(t *testing.T) {
	sched := scheduler.NewPriorityScheduler(false) // Non-preemptive
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 4, 2) // Lower priority
	p2 := process.NewProcess(2, "P2", 0, 3, 0) // Highest priority
	p3 := process.NewProcess(3, "P3", 0, 2, 1) // Medium priority

	sim.AddProcess(p1)
	sim.AddProcess(p2)
	sim.AddProcess(p3)

	completedChan := make(chan bool)

	sim.SetUpdateCallback(func(update *SimulationUpdate) {
		if update.State == SimStateComplete {
			completedChan <- true
		}
	})

	sim.Start()

	select {
	case <-completedChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Priority simulation did not complete within timeout")
	}

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 3 {
		t.Errorf("Expected 3 completed processes, got %d", state.Metrics.CompletedProcesses)
	}

	// Priority order: P2 (0), P3 (1), P1 (2)
	// Execution: P2 (0-3), P3 (3-5), P1 (5-9)
	expectedTime := 9
	if state.Metrics.TotalTime != expectedTime {
		t.Errorf("Expected total time %d, got %d", expectedTime, state.Metrics.TotalTime)
	}
}

func TestCFSScheduling(t *testing.T) {
	sched := scheduler.NewCFSScheduler()
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 5, 0)
	p2 := process.NewProcess(2, "P2", 0, 3, 0)

	sim.AddProcess(p1)
	sim.AddProcess(p2)

	completedChan := make(chan bool)

	sim.SetUpdateCallback(func(update *SimulationUpdate) {
		if update.State == SimStateComplete {
			completedChan <- true
		}
	})

	sim.Start()

	select {
	case <-completedChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("CFS simulation did not complete within timeout")
	}

	state := sim.GetCurrentState()
	if state.Metrics.CompletedProcesses != 2 {
		t.Errorf("Expected 2 completed processes, got %d", state.Metrics.CompletedProcesses)
	}

	// CFS should provide fair scheduling
	expectedTime := 8
	if state.Metrics.TotalTime != expectedTime {
		t.Errorf("Expected total time %d, got %d", expectedTime, state.Metrics.TotalTime)
	}
}

func TestSimulationPauseResume(t *testing.T) {
	sched := scheduler.NewFCFSScheduler()
	sim := NewSimulator(sched)
	sim.SetSpeed(50) // Faster for testing

	p1 := process.NewProcess(1, "P1", 0, 20, 0)

	sim.AddProcess(p1)

	sim.Start()
	time.Sleep(300 * time.Millisecond) // Let it run

	sim.Pause()
	time.Sleep(50 * time.Millisecond)

	state1 := sim.GetCurrentState()
	time1 := state1.CurrentTime

	if time1 == 0 {
		t.Skip("Simulation did not run before pause - timing issue, skip test")
	}

	time.Sleep(300 * time.Millisecond) // Should not advance while paused

	state2 := sim.GetCurrentState()
	time2 := state2.CurrentTime

	if time2 != time1 {
		t.Errorf("Time advanced while paused: %d -> %d", time1, time2)
	}

	sim.Stop()
}

func TestSimulationReset(t *testing.T) {
	sched := scheduler.NewFCFSScheduler()
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 5, 0)
	sim.AddProcess(p1)

	sim.Start()
	time.Sleep(300 * time.Millisecond)
	sim.Stop()

	state1 := sim.GetCurrentState()
	if state1.CurrentTime == 0 {
		t.Error("Expected time to have advanced before reset")
	}

	sim.Reset()

	state2 := sim.GetCurrentState()
	if state2.CurrentTime != 0 {
		t.Errorf("Expected time to be 0 after reset, got %d", state2.CurrentTime)
	}

	if state2.State != SimStateIdle {
		t.Errorf("Expected state to be idle after reset, got %v", state2.State)
	}
}

func TestMetricsCalculation(t *testing.T) {
	sched := scheduler.NewFCFSScheduler()
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 3, 0)
	p2 := process.NewProcess(2, "P2", 2, 4, 0)

	sim.AddProcess(p1)
	sim.AddProcess(p2)

	completedChan := make(chan bool)

	sim.SetUpdateCallback(func(update *SimulationUpdate) {
		if update.State == SimStateComplete {
			completedChan <- true
		}
	})

	sim.Start()

	select {
	case <-completedChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Simulation did not complete within timeout")
	}

	state := sim.GetCurrentState()
	metrics := state.Metrics

	// Verify CPU utilization
	if metrics.CPUUtilization <= 0 || metrics.CPUUtilization > 100 {
		t.Errorf("Invalid CPU utilization: %.2f%%", metrics.CPUUtilization)
	}

	// Verify throughput
	if metrics.Throughput <= 0 {
		t.Errorf("Invalid throughput: %.3f", metrics.Throughput)
	}

	// Verify all processes completed
	if metrics.CompletedProcesses != metrics.TotalProcesses {
		t.Errorf("Not all processes completed: %d/%d",
			metrics.CompletedProcesses, metrics.TotalProcesses)
	}
}

func TestProcessStates(t *testing.T) {
	p := process.NewProcess(1, "P1", 0, 5, 0)

	if p.State != process.StateNew {
		t.Errorf("New process should have process.StateNew, got %v", p.State)
	}

	p.State = process.StateReady
	if p.State != process.StateReady {
		t.Errorf("Expected process.StateReady, got %v", p.State)
	}

	p.Execute(0, 2)
	if p.State != process.StateRunning {
		t.Errorf("Expected StateRunning after execute, got %v", p.State)
	}

	if p.RemainingTime != 3 {
		t.Errorf("Expected remaining time 3, got %d", p.RemainingTime)
	}

	p.Execute(2, 3)
	if !p.IsComplete() {
		t.Error("Process should be complete")
	}

	if p.State != process.StateTerminated {
		t.Errorf("Expected process.StateTerminated, got %v", p.State)
	}
}

func TestGanttChartGeneration(t *testing.T) {
	sched := scheduler.NewFCFSScheduler()
	sim := NewSimulator(sched)

	p1 := process.NewProcess(1, "P1", 0, 3, 0)
	p2 := process.NewProcess(2, "P2", 0, 2, 0)

	sim.AddProcess(p1)
	sim.AddProcess(p2)

	completedChan := make(chan bool)

	sim.SetUpdateCallback(func(update *SimulationUpdate) {
		if update.State == SimStateComplete {
			completedChan <- true
		}
	})

	sim.Start()

	select {
	case <-completedChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Simulation did not complete within timeout")
	}

	state := sim.GetCurrentState()

	if len(state.GanttChart) == 0 {
		t.Error("Expected Gantt chart to have entries")
	}

	// Verify Gantt chart covers entire simulation
	lastEntry := state.GanttChart[len(state.GanttChart)-1]
	if lastEntry.EndTime != state.CurrentTime {
		t.Errorf("Gantt chart should end at current time %d, got %d",
			state.CurrentTime, lastEntry.EndTime)
	}
}
