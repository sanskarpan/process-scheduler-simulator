// Package api provides the REST API for the simulator: run a simulation
// synchronously, list algorithms, and list/retrieve past simulations.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/sanskar/scheduler-simulator/internal/logging"
	"github.com/sanskar/scheduler-simulator/internal/metrics"
	"github.com/sanskar/scheduler-simulator/internal/process"
	"github.com/sanskar/scheduler-simulator/internal/scheduler"
	"github.com/sanskar/scheduler-simulator/internal/simulator"
	"github.com/sanskar/scheduler-simulator/internal/store"
	"github.com/sanskar/scheduler-simulator/internal/version"
)

var idCounter atomic.Uint64

// Handler holds dependencies for the REST API handlers.
type Handler struct {
	store          *store.Store
	defaultQuantum int
	defaultSpeed   int
}

// NewHandler returns a REST API handler.
func NewHandler(s *store.Store, defaultQuantum, defaultSpeed int) *Handler {
	return &Handler{store: s, defaultQuantum: defaultQuantum, defaultSpeed: defaultSpeed}
}

// Register routes on the given mux. Routes use Go 1.22+ method+pattern routing.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/version", h.handleVersion)
	mux.HandleFunc("GET /api/algorithms", h.handleAlgorithms)
	mux.HandleFunc("POST /api/simulate", h.handleSimulate)
	mux.HandleFunc("GET /api/simulations", h.handleList)
	mux.HandleFunc("GET /api/simulations/{id}", h.handleGet)
}

// Algorithm describes a supported scheduling algorithm.
type Algorithm struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Preemptive   bool   `json:"preemptive"`
	NeedsQuantum bool   `json:"needsQuantum"`
	Description  string `json:"description"`
}

// simulateRequest is the JSON body for POST /api/simulate.
type simulateRequest struct {
	Algorithm   string               `json:"algorithm"`
	TimeQuantum int                  `json:"timeQuantum,omitempty"`
	Speed       int                  `json:"speed,omitempty"`
	Processes   []store.ProcessInput `json:"processes"`
}

// simulateResponse is the JSON body for a successful simulation.
type simulateResponse struct {
	ID         string                      `json:"id"`
	Algorithm  string                      `json:"algorithm"`
	DurationMs int64                       `json:"durationMs"`
	State      *simulator.SimulationUpdate `json:"state"`
}

func (h *Handler) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version.Version})
}

func (h *Handler) handleAlgorithms(w http.ResponseWriter, r *http.Request) {
	algorithms := []Algorithm{
		{ID: "fcfs", Name: "First-Come-First-Served", Preemptive: false, NeedsQuantum: false, Description: "Non-preemptive; runs processes in arrival order."},
		{ID: "sjf", Name: "Shortest Job First", Preemptive: false, NeedsQuantum: false, Description: "Non-preemptive; selects the shortest burst time."},
		{ID: "srtf", Name: "Shortest Remaining Time First", Preemptive: true, NeedsQuantum: false, Description: "Preemptive SJF."},
		{ID: "rr", Name: "Round-Robin", Preemptive: true, NeedsQuantum: true, Description: "Time-sliced; each process runs for `timeQuantum`."},
		{ID: "priority", Name: "Priority (Preemptive)", Preemptive: true, NeedsQuantum: false, Description: "Preempts on higher-priority arrivals."},
		{ID: "priority_np", Name: "Priority (Non-Preemptive)", Preemptive: false, NeedsQuantum: false, Description: "Runs to completion once started."},
		{ID: "cfs", Name: "Completely Fair Scheduler", Preemptive: true, NeedsQuantum: false, Description: "Proportional-share via vruntime; Linux-like."},
		{ID: "mlfq", Name: "Multi-Level Feedback Queue", Preemptive: true, NeedsQuantum: false, Description: "Demotes processes on quantum expiry."},
		{ID: "lottery", Name: "Lottery (Proportional Share)", Preemptive: true, NeedsQuantum: true, Description: "Random proportional-share by ticket weight."},
		{ID: "mlq", Name: "Multi-Level Queue (Fixed)", Preemptive: true, NeedsQuantum: true, Description: "Fixed priority queues; strict priority between queues."},
	}
	writeJSON(w, http.StatusOK, algorithms)
}

func (h *Handler) handleSimulate(w http.ResponseWriter, r *http.Request) {
	var req simulateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Processes) == 0 {
		writeError(w, http.StatusBadRequest, "at least one process is required")
		return
	}
	if req.TimeQuantum <= 0 {
		req.TimeQuantum = h.defaultQuantum
	}
	if req.Speed <= 0 {
		req.Speed = h.defaultSpeed
	}

	sched, name, err := buildScheduler(req.Algorithm, req.TimeQuantum)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sim := simulator.NewSimulator(sched)
	for _, p := range req.Processes {
		if vErr := validateProcess(p); vErr != nil {
			writeError(w, http.StatusBadRequest, vErr.Error())
			return
		}
		sim.AddProcess(process.NewProcess(p.PID, p.Name, p.Arrival, p.Burst, p.Priority))
	}

	metrics.SimStarted(name)
	start := time.Now()

	done := make(chan *simulator.SimulationUpdate, 1)
	sim.SetUpdateCallback(func(u *simulator.SimulationUpdate) {
		if u.State == simulator.SimStateComplete {
			select {
			case done <- u:
			default:
			}
		}
	})
	sim.SetSpeed(1) // run as fast as possible for synchronous API
	sim.Start()

	select {
	case u := <-done:
		dur := time.Since(start)
		metrics.SimCompleted(name, dur)
		metrics.SimSteps(name, u.CurrentTime)
		rec := store.Record{
			ID:        generateID(name, start),
			Algorithm: name,
			CreatedAt: start,
			Duration:  dur,
			Config: store.SimulationConfig{
				Algorithm:   req.Algorithm,
				TimeQuantum: req.TimeQuantum,
				Speed:       req.Speed,
				Processes:   req.Processes,
			},
			FinalState: u,
		}
		h.store.Save(rec)
		writeJSON(w, http.StatusOK, simulateResponse{
			ID:         rec.ID,
			Algorithm:  name,
			DurationMs: dur.Milliseconds(),
			State:      u,
		})
	case <-time.After(30 * time.Second):
		sim.Stop()
		metrics.SimCompleted(name, time.Since(start))
		writeError(w, http.StatusGatewayTimeout, "simulation timed out (30s)")
	case <-r.Context().Done():
		sim.Stop()
		writeError(w, 499, "client closed request")
	}
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	all := h.store.List()
	if limit > len(all) {
		limit = len(all)
	}
	writeJSON(w, http.StatusOK, all[:limit])
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	rec, ok := h.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "simulation not found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// buildScheduler constructs a scheduler by algorithm ID.
func buildScheduler(algorithm string, timeQuantum int) (scheduler.Scheduler, string, error) {
	switch algorithm {
	case "fcfs":
		return scheduler.NewFCFSScheduler(), scheduler.NewFCFSScheduler().Name(), nil
	case "sjf":
		return scheduler.NewSJFScheduler(), scheduler.NewSJFScheduler().Name(), nil
	case "srtf":
		return scheduler.NewSRTFScheduler(), scheduler.NewSRTFScheduler().Name(), nil
	case "rr":
		s := scheduler.NewRoundRobinScheduler(timeQuantum)
		return s, s.Name(), nil
	case "priority":
		s := scheduler.NewPriorityScheduler(true)
		return s, s.Name(), nil
	case "priority_np":
		s := scheduler.NewPriorityScheduler(false)
		return s, s.Name(), nil
	case "cfs":
		s := scheduler.NewCFSScheduler()
		return s, s.Name(), nil
	case "mlfq":
		s := scheduler.NewMLFQScheduler()
		return s, s.Name(), nil
	case "lottery":
		s := scheduler.NewLotteryScheduler(timeQuantum, nil)
		return s, s.Name(), nil
	case "mlq":
		s := scheduler.NewMLQScheduler(3, timeQuantum)
		return s, s.Name(), nil
	default:
		return nil, "", errors.New("unknown algorithm: " + algorithm)
	}
}

func validateProcess(p store.ProcessInput) error {
	if p.PID < 0 {
		return errors.New("pid must be >= 0")
	}
	if p.Arrival < 0 {
		return errors.New("arrivalTime must be >= 0")
	}
	if p.Burst < 0 {
		return errors.New("burstTime must be >= 0")
	}
	return nil
}

func generateID(algorithm string, t time.Time) string {
	n := idCounter.Add(1)
	return fmt.Sprintf("%s-%s-%d", algorithm, t.Format("20060102-150405.000"), n)
}

// decodeJSON decodes the request body into dst with a size limit.
// w is required so MaxBytesReader can send a 413 when the limit is exceeded.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = logging.Logger // keep import for context use in extensions
