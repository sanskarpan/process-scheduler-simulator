// Package store provides an in-memory simulation history store. It keeps the
// last N completed simulations so clients can list and replay them via the
// REST API. It is safe for concurrent use.
package store

import (
	"sync"
	"time"

	"github.com/sanskar/scheduler-simulator/internal/simulator"
)

// Record is a completed simulation snapshot.
type Record struct {
	ID         string                      `json:"id"`
	Algorithm  string                      `json:"algorithm"`
	CreatedAt  time.Time                   `json:"createdAt"`
	Duration   time.Duration               `json:"duration"`
	Config     SimulationConfig            `json:"config"`
	FinalState *simulator.SimulationUpdate `json:"finalState"`
}

// SimulationConfig is the input configuration that produced a simulation.
type SimulationConfig struct {
	Algorithm   string         `json:"algorithm"`
	TimeQuantum int            `json:"timeQuantum,omitempty"`
	Speed       int            `json:"speed"`
	Processes   []ProcessInput `json:"processes"`
}

// IOBurstInput is the JSON shape for a single I/O burst in a process.
// Duration=0 entries are skipped by the simulator.
type IOBurstInput struct {
	AfterCPUTime int `json:"afterCPUTime"`
	Duration     int `json:"duration"`
}

// ProcessInput is the JSON shape for a process in a simulation request.
type ProcessInput struct {
	PID      int            `json:"pid"`
	Name     string         `json:"name"`
	Arrival  int            `json:"arrivalTime"`
	Burst    int            `json:"burstTime"`
	Priority int            `json:"priority"`
	IOBursts []IOBurstInput `json:"ioBursts,omitempty"`
}

// Store keeps the last `capacity` completed simulation records in memory.
type Store struct {
	mu       sync.RWMutex
	records  []Record
	capacity int
}

// New returns a Store that retains the last `capacity` completed simulations.
func New(capacity int) *Store {
	if capacity < 1 {
		capacity = 100
	}
	return &Store{capacity: capacity, records: make([]Record, 0, capacity)}
}

// Save stores a completed simulation record. If the store is full, the oldest
// record is evicted.
func (s *Store) Save(r Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) >= s.capacity {
		s.records = s.records[1:]
	}
	s.records = append(s.records, r)
}

// Get returns a record by ID, or false if not found.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.records {
		if r.ID == id {
			return r, true
		}
	}
	return Record{}, false
}

// List returns all stored records, newest first.
func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, len(s.records))
	// Newest first.
	for i, r := range s.records {
		out[len(s.records)-1-i] = r
	}
	return out
}

// Clear removes all stored records.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = s.records[:0]
}
