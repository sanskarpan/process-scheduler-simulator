package scheduler

import (
	"fmt"

	"github.com/sanskar/scheduler-simulator/internal/process"
)

// Scheduler defines the interface for all scheduling algorithms
type Scheduler interface {
	// Schedule selects the next process to run
	Schedule(readyQueue []*process.Process, currentTime int) *process.Process

	// AddProcess adds a new process to the scheduler
	AddProcess(p *process.Process)

	// RemoveProcess removes a process when it completes
	RemoveProcess(p *process.Process)

	// Preempt checks if current process should be preempted
	Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool

	// Name returns the scheduler name
	Name() string

	// QuantumFor returns the time quantum that should apply to the given
	// process. Returning 0 means "run to completion / until preempted". This
	// lets multi-level schedulers (e.g. MLFQ) return a per-level quantum.
	QuantumFor(p *process.Process) int

	// OnQuantumExpired is called by the engine when a process exhausts its
	// quantum. Schedulers that maintain per-process level state (e.g. MLFQ)
	// can demote the process here. The engine still handles re-queueing.
	OnQuantumExpired(p *process.Process)

	// Reset resets the scheduler state
	Reset()
}

// FCFSScheduler implements First-Come-First-Served scheduling
type FCFSScheduler struct {
	name string
}

func NewFCFSScheduler() *FCFSScheduler {
	return &FCFSScheduler{name: "FCFS (First-Come-First-Served)"}
}

func (s *FCFSScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	// Select the first process (arrived earliest)
	earliest := readyQueue[0]
	for _, p := range readyQueue[1:] {
		if p.ArrivalTime < earliest.ArrivalTime {
			earliest = p
		}
	}
	return earliest
}

func (s *FCFSScheduler) AddProcess(p *process.Process)                          {}
func (s *FCFSScheduler) RemoveProcess(p *process.Process)                       {}
func (s *FCFSScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	return false // FCFS is non-preemptive
}
func (s *FCFSScheduler) Name() string                  { return s.name }
func (s *FCFSScheduler) QuantumFor(p *process.Process) int { return 0 }
func (s *FCFSScheduler) OnQuantumExpired(p *process.Process) {}
func (s *FCFSScheduler) Reset()                        {}

// SJFScheduler implements Shortest Job First (non-preemptive)
type SJFScheduler struct {
	name string
}

func NewSJFScheduler() *SJFScheduler {
	return &SJFScheduler{name: "SJF (Shortest Job First)"}
}

func (s *SJFScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	// Select process with shortest burst time
	shortest := readyQueue[0]
	for _, p := range readyQueue[1:] {
		if p.BurstTime < shortest.BurstTime {
			shortest = p
		} else if p.BurstTime == shortest.BurstTime && p.ArrivalTime < shortest.ArrivalTime {
			shortest = p // Tie-breaker: earlier arrival
		}
	}
	return shortest
}

func (s *SJFScheduler) AddProcess(p *process.Process)                          {}
func (s *SJFScheduler) RemoveProcess(p *process.Process)                       {}
func (s *SJFScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	return false // SJF is non-preemptive
}
func (s *SJFScheduler) Name() string                  { return s.name }
func (s *SJFScheduler) QuantumFor(p *process.Process) int { return 0 }
func (s *SJFScheduler) OnQuantumExpired(p *process.Process) {}
func (s *SJFScheduler) Reset()                        {}

// SRTFScheduler implements Shortest Remaining Time First (preemptive SJF)
type SRTFScheduler struct {
	name string
}

func NewSRTFScheduler() *SRTFScheduler {
	return &SRTFScheduler{name: "SRTF (Shortest Remaining Time First)"}
}

func (s *SRTFScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	// Select process with shortest remaining time
	shortest := readyQueue[0]
	for _, p := range readyQueue[1:] {
		if p.RemainingTime < shortest.RemainingTime {
			shortest = p
		} else if p.RemainingTime == shortest.RemainingTime && p.ArrivalTime < shortest.ArrivalTime {
			shortest = p
		}
	}
	return shortest
}

func (s *SRTFScheduler) AddProcess(p *process.Process)    {}
func (s *SRTFScheduler) RemoveProcess(p *process.Process) {}
func (s *SRTFScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	if current == nil {
		return false
	}
	// Check if any ready process has shorter remaining time
	for _, p := range readyQueue {
		if p.PID != current.PID && p.RemainingTime < current.RemainingTime {
			return true
		}
	}
	return false
}
func (s *SRTFScheduler) Name() string                  { return s.name }
func (s *SRTFScheduler) QuantumFor(p *process.Process) int { return 0 }
func (s *SRTFScheduler) OnQuantumExpired(p *process.Process) {}
func (s *SRTFScheduler) Reset()                        {}

// RoundRobinScheduler implements Round-Robin scheduling. The engine owns the
// ready queue and rotates processes through it on quantum expiry; this type
// only stores the configured quantum.
type RoundRobinScheduler struct {
	name        string
	timeQuantum int
}

func NewRoundRobinScheduler(timeQuantum int) *RoundRobinScheduler {
	if timeQuantum < 1 {
		timeQuantum = 1
	}
	return &RoundRobinScheduler{
		name:        fmt.Sprintf("Round-Robin (Quantum=%d)", timeQuantum),
		timeQuantum: timeQuantum,
	}
}

func (s *RoundRobinScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	// The engine maintains FIFO order in the ready queue; the head is next.
	return readyQueue[0]
}

func (s *RoundRobinScheduler) AddProcess(p *process.Process)    {}
func (s *RoundRobinScheduler) RemoveProcess(p *process.Process) {}
func (s *RoundRobinScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	// Round-robin preempts after time quantum expires; handled by the engine.
	return false
}

func (s *RoundRobinScheduler) Name() string                  { return s.name }
func (s *RoundRobinScheduler) QuantumFor(p *process.Process) int { return s.timeQuantum }
func (s *RoundRobinScheduler) OnQuantumExpired(p *process.Process) {}
func (s *RoundRobinScheduler) Reset()                        {}

// PriorityScheduler implements Priority scheduling (preemptive)
type PriorityScheduler struct {
	name        string
	preemptive  bool
}

func NewPriorityScheduler(preemptive bool) *PriorityScheduler {
	mode := "Non-Preemptive"
	if preemptive {
		mode = "Preemptive"
	}
	return &PriorityScheduler{
		name:       fmt.Sprintf("Priority (%s)", mode),
		preemptive: preemptive,
	}
}

func (s *PriorityScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	// Select process with highest priority (lower number = higher priority)
	highest := readyQueue[0]
	for _, p := range readyQueue[1:] {
		if p.Priority < highest.Priority {
			highest = p
		} else if p.Priority == highest.Priority && p.ArrivalTime < highest.ArrivalTime {
			highest = p
		}
	}
	return highest
}

func (s *PriorityScheduler) AddProcess(p *process.Process)    {}
func (s *PriorityScheduler) RemoveProcess(p *process.Process) {}
func (s *PriorityScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	if !s.preemptive || current == nil {
		return false
	}
	// Check if any ready process has higher priority
	for _, p := range readyQueue {
		if p.PID != current.PID && p.Priority < current.Priority {
			return true
		}
	}
	return false
}
func (s *PriorityScheduler) Name() string                  { return s.name }
func (s *PriorityScheduler) QuantumFor(p *process.Process) int { return 0 }
func (s *PriorityScheduler) OnQuantumExpired(p *process.Process) {}
func (s *PriorityScheduler) Reset()                        {}

// CFSScheduler implements a Completely Fair Scheduler (Linux-like). It picks
// the runnable process with the smallest virtual runtime. The engine owns the
// ready queue; this scheduler is stateless aside from configuration.
type CFSScheduler struct {
	name           string
	minGranularity int // Minimum time slice returned as the quantum
}

func NewCFSScheduler() *CFSScheduler {
	return &CFSScheduler{
		name:           "CFS (Completely Fair Scheduler)",
		minGranularity: 1, // Minimum 1 time unit
	}
}

func (s *CFSScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	// Select process with minimum vruntime. Tie-break by PID for determinism.
	minVruntime := readyQueue[0]
	for _, p := range readyQueue[1:] {
		if p.VRuntime < minVruntime.VRuntime ||
			(p.VRuntime == minVruntime.VRuntime && p.PID < minVruntime.PID) {
			minVruntime = p
		}
	}
	return minVruntime
}

func (s *CFSScheduler) AddProcess(p *process.Process)    {}
func (s *CFSScheduler) RemoveProcess(p *process.Process) {}

func (s *CFSScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	if current == nil || len(readyQueue) <= 1 {
		return false
	}
	// Find process with minimum vruntime among others
	minVruntime := int64(^uint64(0) >> 1) // Max int64
	for _, p := range readyQueue {
		if p.PID != current.PID && p.VRuntime < minVruntime {
			minVruntime = p.VRuntime
		}
	}
	// Preempt if another process has significantly lower vruntime
	return minVruntime < current.VRuntime-int64(s.minGranularity*1024/current.Weight)
}

func (s *CFSScheduler) Name() string                  { return s.name }
func (s *CFSScheduler) QuantumFor(p *process.Process) int { return s.minGranularity }
func (s *CFSScheduler) OnQuantumExpired(p *process.Process) {}
func (s *CFSScheduler) Reset()                        {}

// MLFQScheduler implements a Multi-Level Feedback Queue. Processes start at
// the highest-priority level (0) and are demoted one level each time they
// exhaust their quantum, down to the lowest level. Each level has its own
// (increasing) time quantum. Level membership is tracked internally by PID so
// the user-supplied Priority field is not mutated.
type MLFQScheduler struct {
	name         string
	timeQuantums []int // Time quantum for each level
	numLevels    int
	levels       map[int]int // PID -> level
}

func NewMLFQScheduler() *MLFQScheduler {
	numLevels := 3
	return &MLFQScheduler{
		name:         "MLFQ (Multi-Level Feedback Queue)",
		timeQuantums: []int{2, 4, 8}, // Increasing time quantums
		numLevels:    numLevels,
		levels:       make(map[int]int),
	}
}

func (s *MLFQScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	// Select from the highest-priority (lowest level number) queue present.
	// Tie-break by arrival time, then by PID for determinism.
	var best *process.Process
	bestLevel := s.numLevels
	for _, p := range readyQueue {
		lvl := s.levels[p.PID]
		if lvl < bestLevel ||
			(lvl == bestLevel && best != nil &&
				(p.ArrivalTime < best.ArrivalTime ||
					(p.ArrivalTime == best.ArrivalTime && p.PID < best.PID))) {
			best = p
			bestLevel = lvl
		}
	}
	if best == nil {
		best = readyQueue[0]
	}
	return best
}

func (s *MLFQScheduler) AddProcess(p *process.Process) {
	// New processes start at the highest priority level (0).
	if _, ok := s.levels[p.PID]; !ok {
		s.levels[p.PID] = 0
	}
}

func (s *MLFQScheduler) RemoveProcess(p *process.Process) {
	delete(s.levels, p.PID)
}

func (s *MLFQScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	if current == nil {
		return false
	}
	currentLevel := s.levels[current.PID]
	for _, p := range readyQueue {
		if p.PID != current.PID && s.levels[p.PID] < currentLevel {
			return true
		}
	}
	return false
}

func (s *MLFQScheduler) Name() string { return s.name }

func (s *MLFQScheduler) QuantumFor(p *process.Process) int {
	if p == nil {
		return s.timeQuantums[0]
	}
	lvl := s.levels[p.PID]
	if lvl < 0 || lvl >= s.numLevels {
		lvl = s.numLevels - 1
	}
	return s.timeQuantums[lvl]
}

// OnQuantumExpired demotes the process one level (if not already at the bottom).
func (s *MLFQScheduler) OnQuantumExpired(p *process.Process) {
	if p == nil {
		return
	}
	lvl := s.levels[p.PID]
	if lvl < s.numLevels-1 {
		s.levels[p.PID] = lvl + 1
	}
}

func (s *MLFQScheduler) Reset() {
	s.levels = make(map[int]int)
}

// LotteryScheduler implements proportional-share (lottery) scheduling.
// Each process holds a number of tickets proportional to its Weight; a
// uniform random draw picks the winner for the current tick. It is
// preemptive only via quantum expiry.
type LotteryScheduler struct {
	name   string
	rng    RNG
	quantum int
}

// RNG is a minimal random source used by LotteryScheduler. It is an interface
// so tests can inject a deterministic generator.
type RNG interface {
	Intn(n int) int
}

// deterministicRNG is a simple xorshift generator used as the default RNG so
// simulations are reproducible by default. Seeds from currentTime are not used
// to avoid global state; callers may inject their own RNG.
type deterministicRNG struct {
	state uint64
}

// NewRNG returns a deterministic RNG seeded with the given value.
func NewRNG(seed uint64) RNG {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15
	}
	return &deterministicRNG{state: seed}
}

func (r *deterministicRNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	// xorshift64
	x := r.state
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	r.state = x
	return int(x % uint64(n))
}

// NewLotteryScheduler creates a lottery scheduler with the given time quantum.
// If quantum <= 0, it defaults to 1.
func NewLotteryScheduler(quantum int, rng RNG) *LotteryScheduler {
	if quantum < 1 {
		quantum = 1
	}
	if rng == nil {
		rng = NewRNG(0xC0FFEE)
	}
	return &LotteryScheduler{name: "Lottery (Proportional Share)", rng: rng, quantum: quantum}
}

func (s *LotteryScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	totalTickets := 0
	for _, p := range readyQueue {
		w := p.Weight
		if w <= 0 {
			w = 1
		}
		totalTickets += w
	}
	if totalTickets <= 0 {
		return readyQueue[0]
	}
	winner := s.rng.Intn(totalTickets)
	running := 0
	for _, p := range readyQueue {
		w := p.Weight
		if w <= 0 {
			w = 1
		}
		running += w
		if winner < running {
			return p
		}
	}
	return readyQueue[len(readyQueue)-1]
}

func (s *LotteryScheduler) AddProcess(p *process.Process)    {}
func (s *LotteryScheduler) RemoveProcess(p *process.Process) {}
func (s *LotteryScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	return false
}
func (s *LotteryScheduler) Name() string                  { return s.name }
func (s *LotteryScheduler) QuantumFor(p *process.Process) int { return s.quantum }
func (s *LotteryScheduler) OnQuantumExpired(p *process.Process) {}
func (s *LotteryScheduler) Reset() {
	if r, ok := s.rng.(*deterministicRNG); ok {
		r.state = 0xC0FFEE
	}
}

// MLQScheduler implements fixed Multi-Level Queue scheduling. Unlike MLFQ,
// processes do not move between queues: each process's Priority selects its
// queue, and higher queues are served before lower ones (strict priority). The
// quantum is per-queue, defaulting to the scheduler's default quantum.
type MLQScheduler struct {
	name         string
	numLevels    int
	quantum      int
	levels       map[int]int // PID -> level
}

// NewMLQScheduler creates an MLQ scheduler with numLevels queues. If a
// process's Priority is >= numLevels, it is clamped to numLevels-1.
func NewMLQScheduler(numLevels, quantum int) *MLQScheduler {
	if numLevels < 1 {
		numLevels = 1
	}
	if quantum < 1 {
		quantum = 1
	}
	return &MLQScheduler{
		name:      fmt.Sprintf("MLQ (Multi-Level Queue, %d levels)", numLevels),
		numLevels: numLevels,
		quantum:   quantum,
		levels:    make(map[int]int),
	}
}

func (s *MLQScheduler) Schedule(readyQueue []*process.Process, currentTime int) *process.Process {
	if len(readyQueue) == 0 {
		return nil
	}
	var best *process.Process
	bestLevel := s.numLevels
	for _, p := range readyQueue {
		lvl := s.levelFor(p)
		if lvl < bestLevel ||
			(lvl == bestLevel && best != nil &&
				(p.ArrivalTime < best.ArrivalTime ||
					(p.ArrivalTime == best.ArrivalTime && p.PID < best.PID))) {
			best = p
			bestLevel = lvl
		}
	}
	if best == nil {
		best = readyQueue[0]
	}
	return best
}

func (s *MLQScheduler) AddProcess(p *process.Process) {
	s.levels[p.PID] = s.levelFor(p)
}

func (s *MLQScheduler) RemoveProcess(p *process.Process) {
	delete(s.levels, p.PID)
}

func (s *MLQScheduler) Preempt(current *process.Process, readyQueue []*process.Process, currentTime int) bool {
	if current == nil {
		return false
	}
	currentLevel := s.levels[current.PID]
	for _, p := range readyQueue {
		if p.PID != current.PID && s.levels[p.PID] < currentLevel {
			return true
		}
	}
	return false
}

func (s *MLQScheduler) Name() string { return s.name }

func (s *MLQScheduler) QuantumFor(p *process.Process) int { return s.quantum }

// OnQuantumExpired is a no-op for MLQ; processes stay in their fixed queue.
func (s *MLQScheduler) OnQuantumExpired(p *process.Process) {}

func (s *MLQScheduler) Reset() {
	s.levels = make(map[int]int)
}

// levelFor maps a process's Priority onto a queue level, clamped.
func (s *MLQScheduler) levelFor(p *process.Process) int {
	lvl := p.Priority
	if lvl < 0 {
		lvl = 0
	} else if lvl >= s.numLevels {
		lvl = s.numLevels - 1
	}
	return lvl
}
