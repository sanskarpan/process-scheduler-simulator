package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanskar/scheduler-simulator/internal/store"
)

func newTestHandler(t *testing.T) (*Handler, *http.ServeMux) {
	t.Helper()
	h := NewHandler(store.New(10), 4, 1, 0)
	mux := http.NewServeMux()
	h.Register(mux)
	return h, mux
}

func doJSON(t *testing.T, mux *http.ServeMux, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandleAlgorithms(t *testing.T) {
	_, mux := newTestHandler(t)
	rec := doJSON(t, mux, http.MethodGet, "/api/algorithms", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var algos []Algorithm
	if err := json.NewDecoder(rec.Body).Decode(&algos); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]bool{"fcfs": false, "rr": false, "lottery": false, "mlq": false}
	for _, a := range algos {
		if _, ok := want[a.ID]; ok {
			want[a.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("algorithm %q not found in response", id)
		}
	}
}

func TestHandleVersion(t *testing.T) {
	_, mux := newTestHandler(t)
	rec := doJSON(t, mux, http.MethodGet, "/api/version", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var v map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := v["version"]; !ok {
		t.Errorf("response missing 'version' field: %+v", v)
	}
}

func TestHandleSimulateFCFS(t *testing.T) {
	_, mux := newTestHandler(t)
	body := map[string]interface{}{
		"algorithm": "fcfs",
		"processes": []map[string]interface{}{
			{"pid": 1, "name": "P1", "arrivalTime": 0, "burstTime": 3, "priority": 0},
		},
	}
	rec := doJSON(t, mux, http.MethodPost, "/api/simulate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp simulateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State == nil {
		t.Fatal("State is nil")
	}
	if resp.State.State != "complete" {
		t.Errorf("State.State = %q, want complete", resp.State.State)
	}
	if resp.State.Metrics == nil {
		t.Fatal("State.Metrics is nil")
	}
	if resp.State.Metrics.CompletedProcesses != 1 {
		t.Errorf("CompletedProcesses = %d, want 1", resp.State.Metrics.CompletedProcesses)
	}
}

func TestHandleSimulateInvalidAlgorithm(t *testing.T) {
	_, mux := newTestHandler(t)
	body := map[string]interface{}{
		"algorithm": "bogus",
		"processes": []map[string]interface{}{
			{"pid": 1, "name": "P1", "arrivalTime": 0, "burstTime": 1, "priority": 0},
		},
	}
	rec := doJSON(t, mux, http.MethodPost, "/api/simulate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleSimulateNoProcesses(t *testing.T) {
	_, mux := newTestHandler(t)
	body := map[string]interface{}{
		"algorithm": "fcfs",
		"processes": []map[string]interface{}{},
	}
	rec := doJSON(t, mux, http.MethodPost, "/api/simulate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleList(t *testing.T) {
	_, mux := newTestHandler(t)
	// Seed the store with a simulation first.
	body := map[string]interface{}{
		"algorithm": "fcfs",
		"processes": []map[string]interface{}{
			{"pid": 1, "name": "P1", "arrivalTime": 0, "burstTime": 1, "priority": 0},
		},
	}
	rec := doJSON(t, mux, http.MethodPost, "/api/simulate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed simulate status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, mux, http.MethodGet, "/api/simulations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var list []store.Record
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) == 0 {
		t.Errorf("expected non-empty list")
	}
}

func TestHandleGetNotFound(t *testing.T) {
	_, mux := newTestHandler(t)
	rec := doJSON(t, mux, http.MethodGet, "/api/simulations/nonexistent", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleSimulateWithIOBursts(t *testing.T) {
	_, mux := newTestHandler(t)
	body := map[string]interface{}{
		"algorithm": "fcfs",
		"processes": []map[string]interface{}{
			{
				"pid":         1,
				"name":        "P1",
				"arrivalTime": 0,
				"burstTime":   6,
				"priority":    0,
				"ioBursts": []map[string]interface{}{
					{"afterCPUTime": 3, "duration": 2},
				},
			},
		},
	}
	rec := doJSON(t, mux, http.MethodPost, "/api/simulate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp simulateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State == nil {
		t.Fatal("State is nil")
	}
	if resp.State.State != "complete" {
		t.Errorf("State.State = %q, want complete", resp.State.State)
	}
	// 3 CPU ticks → I/O wait 2 ticks → 3 more CPU ticks = 7 elapsed ticks (currentTime=7).
	if resp.State.CurrentTime < 7 {
		t.Errorf("CurrentTime = %d, want >= 7 (3 cpu + 2 io + 3 cpu - 1)", resp.State.CurrentTime)
	}
	if resp.State.Metrics == nil {
		t.Fatal("Metrics is nil")
	}
	if resp.State.Metrics.CompletedProcesses != 1 {
		t.Errorf("CompletedProcesses = %d, want 1", resp.State.Metrics.CompletedProcesses)
	}
	// Verify I/O event was recorded.
	foundIO := false
	for _, e := range resp.State.Events {
		if e.EventType == "io_start" || e.EventType == "io_complete" {
			foundIO = true
			break
		}
	}
	if !foundIO {
		t.Errorf("expected I/O events in simulation, got none: %+v", resp.State.Events)
	}
}

func TestHandleSimulateConcurrencyLimit(t *testing.T) {
	h := NewHandler(store.New(10), 4, 1, 1) // concurrencyLimit=1
	mux := http.NewServeMux()
	h.Register(mux)

	// Acquire the semaphore slot by pre-filling it.
	h.simSem <- struct{}{}
	defer func() { <-h.simSem }()

	body := map[string]interface{}{
		"algorithm": "fcfs",
		"processes": []map[string]interface{}{
			{"pid": 1, "name": "P1", "arrivalTime": 0, "burstTime": 1, "priority": 0},
		},
	}
	rec := doJSON(t, mux, http.MethodPost, "/api/simulate", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when semaphore is full", rec.Code)
	}
}
