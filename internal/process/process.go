// Package process defines the Process type, I/O burst records, Gantt chart
// entries, event log entries, and aggregate scheduling metrics used throughout
// the simulator.
package process

import (
	"fmt"
	"time"
)

// VRuntimeScale is the fixed-point scaling factor for CFS virtual-runtime
// arithmetic. Using 1<<20 instead of 1024 ensures that even processes with
// weight > 1024 (nice < 0) produce a non-zero VRuntime advance per tick.
// The scheduler uses the same constant for the preemption threshold.
const VRuntimeScale = 1 << 20

// ProcessState represents the current state of a process
type ProcessState int

const (
	StateNew ProcessState = iota
	StateReady
	StateRunning
	StateWaiting
	StateTerminated
)

func (s ProcessState) String() string {
	switch s {
	case StateNew:
		return "New"
	case StateReady:
		return "Ready"
	case StateRunning:
		return "Running"
	case StateWaiting:
		return "Waiting"
	case StateTerminated:
		return "Terminated"
	default:
		return "Unknown"
	}
}

// Process represents a simulated process in the scheduler
type Process struct {
	PID            int          // Process ID
	Name           string       // Process name
	ArrivalTime    int          // Time when process arrives
	BurstTime      int          // Total CPU time required
	RemainingTime  int          // Remaining CPU time
	Priority       int          // Priority level (lower number = higher priority)
	State          ProcessState // Current state
	StartTime      int          // Time when first executed (for response time)
	CompletionTime int          // Time when process completes
	WaitingTime    int          // Total time spent waiting
	TurnaroundTime int          // Completion - Arrival time
	ResponseTime   int          // Start - Arrival time
	LastExecuted   int          // Last time this process was executed
	TimeQuantum    int          // For round-robin (time slice used)
	VRuntime       int64        // Virtual runtime for CFS
	Nice           int          // Nice value for CFS (-20 to 19)
	Weight         int          // Weight for CFS scheduling
	IOBursts       []IOBurst    // I/O operations
	CurrentIOIndex int          // Current I/O operation index
	IORemaining    int          // Ticks remaining in current I/O operation (0 = not in I/O)
	Color          string       // Color for visualization
	HasStarted     bool         // Internal flag to track first execution
}

// IOBurst represents an I/O operation
type IOBurst struct {
	AfterCPUTime int // CPU time after which I/O occurs
	Duration     int // I/O duration
	Completed    bool
}

// NewProcess creates a new process
func NewProcess(pid int, name string, arrivalTime, burstTime, priority int) *Process {
	return &Process{
		PID:           pid,
		Name:          name,
		ArrivalTime:   arrivalTime,
		BurstTime:     burstTime,
		RemainingTime: burstTime,
		Priority:      priority,
		State:         StateNew,
		StartTime:     -1,
		Nice:          0,
		Weight:        1024, // Default weight
		Color:         generateColor(pid),
	}
}

// Clone creates a deep copy of the process
func (p *Process) Clone() *Process {
	ioBursts := make([]IOBurst, len(p.IOBursts))
	copy(ioBursts, p.IOBursts)

	return &Process{
		PID:            p.PID,
		Name:           p.Name,
		ArrivalTime:    p.ArrivalTime,
		BurstTime:      p.BurstTime,
		RemainingTime:  p.RemainingTime,
		Priority:       p.Priority,
		State:          p.State,
		StartTime:      p.StartTime,
		CompletionTime: p.CompletionTime,
		WaitingTime:    p.WaitingTime,
		TurnaroundTime: p.TurnaroundTime,
		ResponseTime:   p.ResponseTime,
		LastExecuted:   p.LastExecuted,
		TimeQuantum:    p.TimeQuantum,
		VRuntime:       p.VRuntime,
		Nice:           p.Nice,
		Weight:         p.Weight,
		IOBursts:       ioBursts,
		CurrentIOIndex: p.CurrentIOIndex,
		IORemaining:    p.IORemaining,
		Color:          p.Color,
		HasStarted:     p.HasStarted,
	}
}

// Execute simulates executing the process for a given duration
func (p *Process) Execute(currentTime, duration int) {
	if p.State != StateRunning {
		p.State = StateRunning
	}

	// Track first execution for response time
	if !p.HasStarted {
		p.HasStarted = true
		p.StartTime = currentTime
		p.ResponseTime = currentTime - p.ArrivalTime
	}

	p.RemainingTime -= duration
	p.LastExecuted = currentTime + duration

	// Update virtual runtime for CFS. All access to VRuntime is serialized by
	// the simulator's mutex, so a plain int64 is correct and avoids the prior
	// mixed atomic/non-atomic access data race.
	p.VRuntime += int64(duration) * VRuntimeScale / int64(p.Weight)

	if p.RemainingTime <= 0 {
		p.State = StateTerminated
		p.CompletionTime = currentTime + duration
		p.TurnaroundTime = p.CompletionTime - p.ArrivalTime
		p.WaitingTime = p.TurnaroundTime - p.BurstTime
		p.RemainingTime = 0
	}
}

// IsComplete checks if process has finished execution
func (p *Process) IsComplete() bool {
	return p.State == StateTerminated || p.RemainingTime <= 0
}

// SetNice sets the nice value and updates weight (CFS)
func (p *Process) SetNice(nice int) {
	if nice < -20 {
		nice = -20
	} else if nice > 19 {
		nice = 19
	}
	p.Nice = nice
	// Weight calculation similar to Linux CFS
	// Each nice level represents ~10% change in weight
	p.Weight = niceToWeight(nice)
}

// niceToWeight converts nice value to weight (approximation of Linux kernel)
func niceToWeight(nice int) int {
	// Linux kernel uses a table, we'll use a simplified formula
	// nice -20 -> weight ~88761, nice 0 -> weight 1024, nice 19 -> weight ~15
	baseWeight := 1024
	if nice == 0 {
		return baseWeight
	}

	// Each nice level is approximately 1.25x weight change
	// nice -1 -> 1.25x weight, nice +1 -> 0.80x weight
	weight := float64(baseWeight)
	for i := 0; i < abs(nice); i++ {
		if nice < 0 {
			weight *= 1.25
		} else {
			weight *= 0.80
		}
	}
	return int(weight)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// String returns a string representation of the process
func (p *Process) String() string {
	return fmt.Sprintf("P%d[%s] Arr:%d Burst:%d Rem:%d Pri:%d State:%s",
		p.PID, p.Name, p.ArrivalTime, p.BurstTime, p.RemainingTime, p.Priority, p.State)
}

// ProcessEvent represents a scheduling event
type ProcessEvent struct {
	Time        int          // When the event occurred
	PID         int          // Process ID
	EventType   string       // "arrival", "start", "complete", "preempt", "context_switch"
	Description string       // Human-readable description
	State       ProcessState // Process state after event
}

// GanttEntry represents a time slice in the Gantt chart
type GanttEntry struct {
	PID       int    // Process ID (-1 for idle)
	Name      string // Process name
	StartTime int    // Start time of this slice
	EndTime   int    // End time of this slice
	Color     string // Color for visualization
}

// SchedulingMetrics holds overall scheduling performance metrics
type SchedulingMetrics struct {
	TotalProcesses        int
	CompletedProcesses    int
	AverageTurnaroundTime float64
	AverageWaitingTime    float64
	AverageResponseTime   float64
	CPUUtilization        float64
	Throughput            float64
	ContextSwitches       int
	TotalTime             int
	Algorithm             string
	Timestamp             time.Time
}

// generateColor generates a stable color for a process PID. The offset by
// (pid-1) matches the client-side palette so server and browser agree.
func generateColor(pid int) string {
	colors := []string{
		"#4A90E2", "#50C878", "#E74C3C", "#F39C12", "#9B59B6",
		"#1ABC9C", "#E67E22", "#3498DB", "#2ECC71", "#E91E63",
		"#00BCD4", "#FF5722", "#795548", "#607D8B", "#CDDC39",
	}
	idx := pid - 1
	if idx < 0 {
		idx = 0
	}
	return colors[idx%len(colors)]
}
