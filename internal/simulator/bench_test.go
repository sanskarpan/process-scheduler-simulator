package simulator

import (
	"fmt"
	"testing"

	"github.com/sanskar/scheduler-simulator/internal/process"
	"github.com/sanskar/scheduler-simulator/internal/scheduler"
)

// BenchmarkFCFS_Run benchmarks a full FCFS simulation of 100 processes.
func BenchmarkFCFS_Run(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sched := scheduler.NewFCFSScheduler()
		sim := NewSimulator(sched)
		for j := 0; j < 100; j++ {
			sim.AddProcess(process.NewProcess(j+1, fmt.Sprintf("P%d", j+1), j%10, (j%8)+1, 0))
		}
		done := make(chan struct{})
		sim.SetUpdateCallback(func(u *SimulationUpdate) {
			if u.State == SimStateComplete {
				close(done)
			}
		})
		sim.SetSpeed(1)
		sim.Start()
		<-done
	}
}

// BenchmarkSchedule_Only benchmarks just the Schedule() selection logic for
// 1000 ready processes (no engine overhead).
func BenchmarkSchedule_Only(b *testing.B) {
	sched := scheduler.NewCFSScheduler()
	q := make([]*process.Process, 1000)
	for i := 0; i < 1000; i++ {
		q[i] = process.NewProcess(i+1, "P", 0, 10, 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Schedule(q, 0)
	}
}

// BenchmarkSnapshotState benchmarks the immutable state cloning done for every
// broadcast update.
func BenchmarkSnapshotState(b *testing.B) {
	sched := scheduler.NewRoundRobinScheduler(2)
	sim := NewSimulator(sched)
	for j := 0; j < 50; j++ {
		sim.AddProcess(process.NewProcess(j+1, "P", j%5, (j%10)+1, 0))
	}
	// Run a few steps to populate gantt/events.
	for i := 0; i < 10; i++ {
		sim.Step()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sim.GetCurrentState()
	}
}
