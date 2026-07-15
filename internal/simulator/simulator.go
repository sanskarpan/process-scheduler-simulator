package simulator

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sanskar/scheduler-simulator/internal/process"
	"github.com/sanskar/scheduler-simulator/internal/scheduler"
)

// SimulationState represents the current state of the simulation
type SimulationState string

const (
	SimStateIdle     SimulationState = "idle"
	SimStateRunning  SimulationState = "running"
	SimStatePaused   SimulationState = "paused"
	SimStateComplete SimulationState = "complete"
)

// Simulator manages the scheduling simulation
type Simulator struct {
	scheduler       scheduler.Scheduler
	processes       []*process.Process
	readyQueue      []*process.Process
	currentProcess  *process.Process
	currentTime     int
	ganttChart      []process.GanttEntry
	events          []process.ProcessEvent
	contextSwitches int
	state           SimulationState
	speed           int // Milliseconds per time unit
	mu              sync.RWMutex
	wg              sync.WaitGroup // tracks the run() goroutine
	stepMu          sync.Mutex     // serializes concurrent Step() calls
	pauseChan       chan bool
	stopChan        chan bool
	updateCallback  func(*SimulationUpdate)
	lastGanttUpdate int
	totalIdleTime   int
	timeQuantumUsed int // Track time used in current quantum
}

// SimulationUpdate contains data sent to clients during simulation
type SimulationUpdate struct {
	CurrentTime     int                        `json:"currentTime"`
	CurrentProcess  *process.Process           `json:"currentProcess"`
	ReadyQueue      []*process.Process         `json:"readyQueue"`
	CompletedProces []*process.Process         `json:"completedProcesses"`
	GanttChart      []process.GanttEntry       `json:"ganttChart"`
	Events          []process.ProcessEvent     `json:"events"`
	Metrics         *process.SchedulingMetrics `json:"metrics"`
	State           SimulationState            `json:"state"`
	Algorithm       string                     `json:"algorithm"`
}

// NewSimulator creates a new simulator with the given scheduler
func NewSimulator(scheduler scheduler.Scheduler) *Simulator {
	return &Simulator{
		scheduler:       scheduler,
		processes:       make([]*process.Process, 0),
		readyQueue:      make([]*process.Process, 0),
		ganttChart:      make([]process.GanttEntry, 0),
		events:          make([]process.ProcessEvent, 0),
		state:           SimStateIdle,
		speed:           100, // 100ms per time unit by default
		pauseChan:       make(chan bool, 1),
		stopChan:        make(chan bool, 1),
		timeQuantumUsed: 0,
	}
}

// AddProcess adds a process to the simulation
func (s *Simulator) AddProcess(p *process.Process) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clone the process to avoid modifying the original
	clone := p.Clone()
	s.processes = append(s.processes, clone)

	// Sort processes by arrival time
	sort.Slice(s.processes, func(i, j int) bool {
		return s.processes[i].ArrivalTime < s.processes[j].ArrivalTime
	})
}

// AddProcesses adds multiple processes
func (s *Simulator) AddProcesses(processes []*process.Process) {
	for _, p := range processes {
		s.AddProcess(p)
	}
}

// SetUpdateCallback sets the callback function for simulation updates
func (s *Simulator) SetUpdateCallback(callback func(*SimulationUpdate)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateCallback = callback
}

// Start begins the simulation
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

// Pause pauses the simulation
func (s *Simulator) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == SimStateRunning {
		s.state = SimStatePaused
	}
}

// Resume resumes a paused simulation
func (s *Simulator) Resume() {
	s.mu.Lock()
	if s.state == SimStatePaused {
		s.state = SimStateRunning
		s.mu.Unlock()
		select {
		case s.pauseChan <- true:
		default:
		}
		return
	}
	s.mu.Unlock()
}

// Step executes one time unit of simulation synchronously. It is only allowed
// when the engine is paused or idle; stepping while running is ignored to
// avoid racing with the ticker and double-executing time units.
func (s *Simulator) Step() {
	// stepMu serializes concurrent Step() callers so two goroutines cannot
	// both pass the state check and both call executeTimeUnit().
	s.stepMu.Lock()
	defer s.stepMu.Unlock()

	s.mu.Lock()
	if s.state == SimStateRunning || s.state == SimStateComplete {
		s.mu.Unlock()
		return
	}
	// Transition idle -> paused so the UI reflects an in-progress simulation
	// and Resume/Start can pick it up.
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

// Stop stops the simulation and waits for the run goroutine to exit before
// returning. Subsequent calls are safe (no-ops if already stopped).
func (s *Simulator) Stop() {
	s.mu.Lock()
	if s.state == SimStateRunning || s.state == SimStatePaused {
		s.state = SimStateIdle
		s.mu.Unlock()
		select {
		case s.stopChan <- true:
		default:
		}
		s.wg.Wait() // wait until run() calls wg.Done()
		return
	}
	s.mu.Unlock()
}

// Reset resets the simulation to initial state. If a simulation is running it
// is stopped first so the run goroutine exits and does not race with the
// reset. Processes themselves are restored to their pre-simulation state.
func (s *Simulator) Reset() {
	// Stop() guarantees the run goroutine has exited before returning.
	s.Stop()

	// Drain the pause signal only; stopChan was already consumed by run().
	select {
	case <-s.pauseChan:
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.currentTime = 0
	s.currentProcess = nil
	s.readyQueue = make([]*process.Process, 0)
	s.ganttChart = make([]process.GanttEntry, 0)
	s.events = make([]process.ProcessEvent, 0)
	s.contextSwitches = 0
	s.state = SimStateIdle
	s.lastGanttUpdate = 0
	s.totalIdleTime = 0
	s.timeQuantumUsed = 0
	s.scheduler.Reset()

	// Reset all processes to their initial configuration.
	for i := range s.processes {
		s.processes[i].State = process.StateNew
		s.processes[i].RemainingTime = s.processes[i].BurstTime
		s.processes[i].StartTime = -1
		s.processes[i].CompletionTime = 0
		s.processes[i].WaitingTime = 0
		s.processes[i].TurnaroundTime = 0
		s.processes[i].ResponseTime = 0
		s.processes[i].VRuntime = 0
		s.processes[i].HasStarted = false
		s.processes[i].LastExecuted = 0
		s.processes[i].TimeQuantum = 0
		s.processes[i].CurrentIOIndex = 0
	}
}

// SetSpeed sets the simulation speed (milliseconds per time unit)
func (s *Simulator) SetSpeed(speed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if speed < 1 {
		speed = 1
	}
	if speed > 5000 {
		speed = 5000
	}
	s.speed = speed
}

// run is the main simulation loop. It is launched by Start(). Step() executes
// synchronously and does not go through this loop. The loop exits on stopChan
// or when the simulation completes.
func (s *Simulator) run() {
	defer s.wg.Done()

	s.mu.RLock()
	speed := s.speed
	s.mu.RUnlock()

	ticker := time.NewTicker(time.Duration(speed) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return

		case <-s.pauseChan:
			// Paused. Stop the periodic ticker and wait for resume/stop.
			ticker.Stop()
		pausedLoop:
			for {
				select {
				case <-s.pauseChan:
					// Resumed: rebuild ticker with the (possibly updated) speed.
					s.mu.RLock()
					speed := s.speed
					s.mu.RUnlock()
					ticker = time.NewTicker(time.Duration(speed) * time.Millisecond)
					break pausedLoop
				case <-s.stopChan:
					return
				}
			}

		case <-ticker.C:
			s.mu.RLock()
			state := s.state
			s.mu.RUnlock()

			if state == SimStatePaused {
				ticker.Stop()
				// Non-blocking: the run loop's pauseChan case will pick this up.
				select {
				case s.pauseChan <- true:
				default:
				}
				continue
			}

			if state != SimStateRunning {
				continue
			}

			s.executeTimeUnit()
			s.sendUpdate()

			// Check if simulation is complete
			if s.isComplete() {
				s.mu.Lock()
				s.state = SimStateComplete
				s.mu.Unlock()
				s.sendUpdate()
				return
			}
		}
	}
}

// executeTimeUnit executes one unit of simulation time
func (s *Simulator) executeTimeUnit() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for new arrivals
	s.checkArrivals()

	// Schedule next process if needed
	if s.currentProcess == nil || s.currentProcess.IsComplete() {
		s.scheduleNextProcess()
	} else if s.shouldPreempt() {
		// Check for preemption
		s.preemptCurrentProcess()
		s.scheduleNextProcess()
	}

	// Execute current process for one time unit
	if s.currentProcess != nil {
		s.executeProcess(1)
		s.timeQuantumUsed++

		// Check time quantum expiration (Round-Robin, CFS, MLFQ, ...).
		timeQuantum := s.scheduler.QuantumFor(s.currentProcess)
		if timeQuantum > 0 && s.timeQuantumUsed >= timeQuantum && s.currentProcess != nil {
			// Time quantum expired. Notify the scheduler so it can demote the
			// process (e.g. MLFQ) before re-queueing.
			if !s.currentProcess.IsComplete() {
				expired := s.currentProcess
				s.scheduler.OnQuantumExpired(expired)
				s.addEvent("quantum_expire", expired.PID,
					fmt.Sprintf("Process %d time quantum expired", expired.PID))
				s.contextSwitches++
				expired.State = process.StateReady
				s.readyQueue = append(s.readyQueue, expired)
				s.currentProcess = nil
				s.timeQuantumUsed = 0
				s.scheduleNextProcess()
			}
		}
	} else {
		// CPU is idle
		s.totalIdleTime++
		s.addGanttEntry(-1, "IDLE", s.currentTime, s.currentTime+1, "#2C3E50")
	}

	s.currentTime++

	// Update waiting times for processes in ready queue
	for _, p := range s.readyQueue {
		if p.State == process.StateReady && (s.currentProcess == nil || p.PID != s.currentProcess.PID) {
			p.WaitingTime++
		}
	}
}

// checkArrivals checks for processes arriving at or before current time.
// Using <= instead of == handles processes added dynamically with a past
// arrivalTime (e.g. arrivalTime=0 when currentTime=5).
func (s *Simulator) checkArrivals() {
	for _, p := range s.processes {
		if p.ArrivalTime <= s.currentTime && p.State == process.StateNew {
			p.State = process.StateReady
			s.readyQueue = append(s.readyQueue, p)
			s.scheduler.AddProcess(p)
			s.addEvent("arrival", p.PID, fmt.Sprintf("Process %d arrived", p.PID))
		}
	}
}

// scheduleNextProcess selects and schedules the next process to run
func (s *Simulator) scheduleNextProcess() {
	if len(s.readyQueue) == 0 {
		s.currentProcess = nil
		return
	}

	// Use scheduler to select next process
	next := s.scheduler.Schedule(s.readyQueue, s.currentTime)
	if next == nil {
		s.currentProcess = nil
		return
	}

	// Remove from ready queue by pointer, not PID, to correctly handle
	// processes that share the same PID.
	for i, p := range s.readyQueue {
		if p == next {
			s.readyQueue = append(s.readyQueue[:i], s.readyQueue[i+1:]...)
			break
		}
	}

	// Context switch
	if s.currentProcess != nil && s.currentProcess.PID != next.PID {
		s.contextSwitches++
	}

	wasStarted := next.HasStarted
	s.currentProcess = next
	s.currentProcess.State = process.StateRunning
	s.timeQuantumUsed = 0

	if !wasStarted {
		s.addEvent("start", next.PID, fmt.Sprintf("Process %d started execution", next.PID))
	} else {
		s.addEvent("resume", next.PID, fmt.Sprintf("Process %d resumed execution", next.PID))
	}
}

// shouldPreempt checks if current process should be preempted
func (s *Simulator) shouldPreempt() bool {
	return s.scheduler.Preempt(s.currentProcess, s.readyQueue, s.currentTime)
}

// preemptCurrentProcess preempts the currently running process
func (s *Simulator) preemptCurrentProcess() {
	if s.currentProcess == nil {
		return
	}
	s.addEvent("preempt", s.currentProcess.PID,
		fmt.Sprintf("Process %d preempted", s.currentProcess.PID))
	s.contextSwitches++
	s.currentProcess.State = process.StateReady
	s.readyQueue = append(s.readyQueue, s.currentProcess)
	s.currentProcess = nil
}

// executeProcess executes the current process for given duration
func (s *Simulator) executeProcess(duration int) {
	if s.currentProcess == nil {
		return
	}

	startTime := s.currentTime
	s.currentProcess.Execute(s.currentTime, duration)

	// Add to Gantt chart
	s.addGanttEntry(s.currentProcess.PID, s.currentProcess.Name,
		startTime, s.currentTime+duration, s.currentProcess.Color)

	// Check if process completed
	if s.currentProcess.IsComplete() {
		s.addEvent("complete", s.currentProcess.PID,
			fmt.Sprintf("Process %d completed", s.currentProcess.PID))
		s.scheduler.RemoveProcess(s.currentProcess)
		s.currentProcess = nil
	}
}

// addGanttEntry adds an entry to the Gantt chart
func (s *Simulator) addGanttEntry(pid int, name string, start, end int, color string) {
	// Merge with previous entry if same process
	if len(s.ganttChart) > 0 {
		last := &s.ganttChart[len(s.ganttChart)-1]
		if last.PID == pid && last.EndTime == start {
			last.EndTime = end
			return
		}
	}

	s.ganttChart = append(s.ganttChart, process.GanttEntry{
		PID:       pid,
		Name:      name,
		StartTime: start,
		EndTime:   end,
		Color:     color,
	})
}

// addEvent adds an event to the event log
func (s *Simulator) addEvent(eventType string, pid int, description string) {
	var state process.ProcessState
	for _, p := range s.processes {
		if p.PID == pid {
			state = p.State
			break
		}
	}

	s.events = append(s.events, process.ProcessEvent{
		Time:        s.currentTime,
		PID:         pid,
		EventType:   eventType,
		Description: description,
		State:       state,
	})
}

// isComplete checks if simulation is complete
func (s *Simulator) isComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.processes {
		if !p.IsComplete() {
			return false
		}
	}
	return true
}

// sendUpdate sends current simulation state to callback. The callback is
// invoked WITHOUT the lock held so a slow/blocked consumer does not stall the
// engine, and we avoid spawning a goroutine per update (which previously could
// leak if the broadcast channel blocked).
func (s *Simulator) sendUpdate() {
	update := s.snapshotState()
	s.mu.RLock()
	cb := s.updateCallback
	s.mu.RUnlock()
	if cb != nil {
		cb(update)
	}
}

// snapshotState builds an immutable SimulationUpdate under the read lock.
func (s *Simulator) snapshotState() *SimulationUpdate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Clone processes for completed list
	completed := make([]*process.Process, 0)
	for _, p := range s.processes {
		if p.IsComplete() {
			completed = append(completed, p.Clone())
		}
	}

	// Clone ready queue
	readyClone := make([]*process.Process, len(s.readyQueue))
	for i, p := range s.readyQueue {
		readyClone[i] = p.Clone()
	}

	// Clone current process
	var currentClone *process.Process
	if s.currentProcess != nil {
		currentClone = s.currentProcess.Clone()
	}

	// Clone gantt + events so the consumer can read without races.
	ganttClone := make([]process.GanttEntry, len(s.ganttChart))
	copy(ganttClone, s.ganttChart)
	eventsClone := make([]process.ProcessEvent, len(s.events))
	copy(eventsClone, s.events)

	return &SimulationUpdate{
		CurrentTime:     s.currentTime,
		CurrentProcess:  currentClone,
		ReadyQueue:      readyClone,
		CompletedProces: completed,
		GanttChart:      ganttClone,
		Events:          eventsClone,
		Metrics:         s.calculateMetrics(),
		State:           s.state,
		Algorithm:       s.scheduler.Name(),
	}
}

// calculateMetrics computes overall scheduling metrics. It must NOT mutate
// process state (it is called from sendUpdate under a read lock); per-process
// WaitingTime is already maintained incrementally by executeTimeUnit.
func (s *Simulator) calculateMetrics() *process.SchedulingMetrics {
	totalProcesses := len(s.processes)
	completed := 0
	totalTurnaround := 0
	totalWaiting := 0
	totalResponse := 0

	for _, p := range s.processes {
		if p.IsComplete() {
			completed++
			totalTurnaround += p.TurnaroundTime
			totalWaiting += p.WaitingTime
			totalResponse += p.ResponseTime
		} else {
			// Read-only: use the waiting time maintained by the engine.
			totalWaiting += p.WaitingTime
		}
	}

	avgTurnaround := 0.0
	avgWaiting := 0.0
	avgResponse := 0.0
	cpuUtil := 0.0
	throughput := 0.0

	if totalProcesses > 0 {
		avgWaiting = float64(totalWaiting) / float64(totalProcesses)
	}

	if completed > 0 {
		avgTurnaround = float64(totalTurnaround) / float64(completed)
		avgResponse = float64(totalResponse) / float64(completed)
	}

	if s.currentTime > 0 {
		cpuUtil = float64(s.currentTime-s.totalIdleTime) / float64(s.currentTime) * 100
		throughput = float64(completed) / float64(s.currentTime)
	}

	return &process.SchedulingMetrics{
		TotalProcesses:        totalProcesses,
		CompletedProcesses:    completed,
		AverageTurnaroundTime: avgTurnaround,
		AverageWaitingTime:    avgWaiting,
		AverageResponseTime:   avgResponse,
		CPUUtilization:        cpuUtil,
		Throughput:            throughput,
		ContextSwitches:       s.contextSwitches,
		TotalTime:             s.currentTime,
		Algorithm:             s.scheduler.Name(),
		Timestamp:             time.Now(),
	}
}

// GetCurrentState returns current simulation state (thread-safe)
func (s *Simulator) GetCurrentState() *SimulationUpdate {
	return s.snapshotState()
}
