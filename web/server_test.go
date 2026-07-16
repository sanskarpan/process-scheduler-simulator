package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sanskar/scheduler-simulator/internal/config"
	"github.com/sanskar/scheduler-simulator/internal/simulator"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func testCfg() config.Config {
	return config.Config{
		BroadcastBufferSize: 32,
		MaxClients:          0,
		WSReadLimit:         4096,
		WSWriteWait:         2 * time.Second,
		WSPongWait:          5 * time.Second,
		WSPingPeriod:        2 * time.Second,
	}
}

// newTestWS creates a Server, wraps HandleWebSocket in an httptest.Server, and
// dials a WebSocket connection. httptest sets no Origin header so CheckOrigin
// passes (localhost short-circuit). Cleanup is registered via t.Cleanup.
func newTestWS(t *testing.T) (*Server, *websocket.Conn) {
	t.Helper()
	s := NewServer(testCfg())
	ts := httptest.NewServer(http.HandlerFunc(s.HandleWebSocket))
	t.Cleanup(func() {
		s.Shutdown()
		ts.Close()
	})
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return s, conn
}

// recv reads one JSON message from a WebSocket connection with a deadline.
func recv(t *testing.T, conn *websocket.Conn) map[string]interface{} {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// sendJSON sends a JSON-encoded map over the WebSocket connection.
func sendJSON(t *testing.T, conn *websocket.Conn, v interface{}) {
	t.Helper()
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetWriteDeadline: %v", err)
	}
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
}

// drain reads messages until one has the wanted type field, dropping others.
func drain(t *testing.T, conn *websocket.Conn, wantType string) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m := recv(t, conn)
		if m["type"] == wantType {
			return m
		}
	}
	t.Fatalf("timed out waiting for type=%q", wantType)
	return nil
}

// initSim sends an init message and drains until success.
func initSim(t *testing.T, conn *websocket.Conn, algorithm string) {
	t.Helper()
	sendJSON(t, conn, map[string]interface{}{
		"type":      "init",
		"algorithm": algorithm,
		"processes": []map[string]interface{}{
			{"pid": 1, "name": "P1", "arrivalTime": 0, "burstTime": 4, "priority": 0},
			{"pid": 2, "name": "P2", "arrivalTime": 1, "burstTime": 3, "priority": 1},
		},
	})
	r := drain(t, conn, "success")
	if !strings.Contains(r["message"].(string), "initialized") {
		t.Fatalf("init: unexpected message: %v", r)
	}
}

// ── DefaultPort ──────────────────────────────────────────────────────────────

func TestDefaultPort_EnvSet(t *testing.T) {
	t.Setenv("PORT", "9090")
	if got := DefaultPort(":8080"); got != ":9090" {
		t.Errorf("DefaultPort = %q, want \":9090\"", got)
	}
}

func TestDefaultPort_EnvInvalid(t *testing.T) {
	t.Setenv("PORT", "notanumber")
	if got := DefaultPort(":8080"); got != ":8080" {
		t.Errorf("DefaultPort = %q, want fallback \":8080\"", got)
	}
}

func TestDefaultPort_EnvUnset(t *testing.T) {
	os.Unsetenv("PORT")
	if got := DefaultPort(":1234"); got != ":1234" {
		t.Errorf("DefaultPort = %q, want fallback \":1234\"", got)
	}
}

// ── parseProcess ─────────────────────────────────────────────────────────────

func TestParseProcess_Valid(t *testing.T) {
	p, err := parseProcess(map[string]interface{}{
		"pid": float64(3), "name": "Worker", "arrivalTime": float64(2),
		"burstTime": float64(5), "priority": float64(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PID != 3 || p.Name != "Worker" || p.ArrivalTime != 2 || p.BurstTime != 5 || p.Priority != 1 {
		t.Errorf("parsed process mismatch: %+v", p)
	}
}

func TestParseProcess_DefaultName(t *testing.T) {
	p, err := parseProcess(map[string]interface{}{
		"pid": float64(7), "arrivalTime": float64(0), "burstTime": float64(3),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "P7" {
		t.Errorf("Name = %q, want \"P7\"", p.Name)
	}
}

func TestParseProcess_DefaultPriority(t *testing.T) {
	p, err := parseProcess(map[string]interface{}{
		"pid": float64(1), "arrivalTime": float64(0), "burstTime": float64(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Priority != 0 {
		t.Errorf("Priority = %d, want 0", p.Priority)
	}
}

func TestParseProcess_MissingPID(t *testing.T) {
	_, err := parseProcess(map[string]interface{}{
		"arrivalTime": float64(0), "burstTime": float64(3),
	})
	if err == nil {
		t.Fatal("expected error for missing pid")
	}
}

func TestParseProcess_MissingArrivalTime(t *testing.T) {
	_, err := parseProcess(map[string]interface{}{
		"pid": float64(1), "burstTime": float64(3),
	})
	if err == nil {
		t.Fatal("expected error for missing arrivalTime")
	}
}

func TestParseProcess_MissingBurstTime(t *testing.T) {
	_, err := parseProcess(map[string]interface{}{
		"pid": float64(1), "arrivalTime": float64(0),
	})
	if err == nil {
		t.Fatal("expected error for missing burstTime")
	}
}

func TestParseProcess_NegativeArrivalTime(t *testing.T) {
	_, err := parseProcess(map[string]interface{}{
		"pid": float64(1), "arrivalTime": float64(-1), "burstTime": float64(3),
	})
	if err == nil {
		t.Fatal("expected error for negative arrivalTime")
	}
}

func TestParseProcess_NegativeBurstTime(t *testing.T) {
	_, err := parseProcess(map[string]interface{}{
		"pid": float64(1), "arrivalTime": float64(0), "burstTime": float64(-1),
	})
	if err == nil {
		t.Fatal("expected error for negative burstTime")
	}
}

func TestParseProcess_NegativePID(t *testing.T) {
	_, err := parseProcess(map[string]interface{}{
		"pid": float64(-1), "arrivalTime": float64(0), "burstTime": float64(3),
	})
	if err == nil {
		t.Fatal("expected error for negative pid")
	}
}

// ── HandleHealth ─────────────────────────────────────────────────────────────

func TestHandleHealth_NoSimulator(t *testing.T) {
	s := NewServer(testCfg())
	t.Cleanup(s.Shutdown)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.HandleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "healthy" {
		t.Errorf("status = %v, want \"healthy\"", body["status"])
	}
	if body["simulatorReady"] != false {
		t.Errorf("simulatorReady = %v, want false", body["simulatorReady"])
	}
}

func TestHandleHealth_Headers(t *testing.T) {
	s := NewServer(testCfg())
	t.Cleanup(s.Shutdown)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.HandleHealth(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// ── WebSocket: connect ────────────────────────────────────────────────────────

func TestHandleWebSocket_Connect(t *testing.T) {
	_, conn := newTestWS(t)
	// A fresh server has no simulator → no initial message is sent.
	// We just verify the connection was established (no error in newTestWS).
	_ = conn
}

func TestHandleWebSocket_MaxClients(t *testing.T) {
	cfg := testCfg()
	cfg.MaxClients = 1
	s := NewServer(cfg)
	ts := httptest.NewServer(http.HandlerFunc(s.HandleWebSocket))
	t.Cleanup(func() { s.Shutdown(); ts.Close() })

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	c1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer c1.Close()

	// Second connection should be closed immediately by the server.
	c2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		// Dial itself may fail, which is also acceptable.
		return
	}
	defer c2.Close()
	// Or the server closes it after a brief write.
	_ = c2.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = c2.ReadMessage()
	if err == nil {
		t.Error("expected second connection to be closed by MaxClients limit")
	}
}

// ── WebSocket: unknown/missing type ──────────────────────────────────────────

func TestHandleWebSocket_MissingType(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"foo": "bar"})
	r := drain(t, conn, "error")
	if !strings.Contains(r["message"].(string), "type") {
		t.Errorf("error message = %q, want mention of 'type'", r["message"])
	}
}

func TestHandleWebSocket_UnknownMessageType(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "doesNotExist"})
	r := drain(t, conn, "error")
	if msg, _ := r["message"].(string); !strings.Contains(msg, "doesNotExist") && !strings.Contains(strings.ToLower(msg), "unknown") {
		t.Errorf("error message = %q, want mention of unknown type", msg)
	}
}

// ── WebSocket: init ───────────────────────────────────────────────────────────

func TestWS_Init_FCFS(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
}

func TestWS_Init_AllAlgorithms(t *testing.T) {
	algos := []string{"fcfs", "sjf", "srtf", "rr", "priority", "priority_np", "cfs", "mlfq", "lottery", "mlq"}
	for _, algo := range algos {
		algo := algo
		t.Run(algo, func(t *testing.T) {
			_, conn := newTestWS(t)
			initSim(t, conn, algo)
		})
	}
}

func TestWS_Init_UnknownAlgorithm(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{
		"type":      "init",
		"algorithm": "bogus_algo",
		"processes": []map[string]interface{}{
			{"pid": 1, "arrivalTime": 0, "burstTime": 3},
		},
	})
	r := drain(t, conn, "error")
	if msg, _ := r["message"].(string); !strings.Contains(msg, "bogus_algo") {
		t.Errorf("error = %q, want mention of unknown algorithm", msg)
	}
}

func TestWS_Init_InvalidProcess_NegativePID(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{
		"type":      "init",
		"algorithm": "fcfs",
		"processes": []map[string]interface{}{
			{"pid": -1, "arrivalTime": 0, "burstTime": 3},
		},
	})
	r := drain(t, conn, "error")
	if msg, _ := r["message"].(string); !strings.Contains(strings.ToLower(msg), "pid") {
		t.Errorf("error = %q, want mention of pid", msg)
	}
}

func TestWS_Init_InvalidProcess_SimulatorUnchanged(t *testing.T) {
	_, conn := newTestWS(t)
	// Valid init first.
	initSim(t, conn, "fcfs")

	// Now send an invalid init (bad process). The old sim should survive.
	sendJSON(t, conn, map[string]interface{}{
		"type":      "init",
		"algorithm": "sjf",
		"processes": []map[string]interface{}{
			{"pid": -99, "arrivalTime": 0, "burstTime": 3},
		},
	})
	drain(t, conn, "error")

	// getState should still work and return the old (fcfs) state.
	sendJSON(t, conn, map[string]interface{}{"type": "getState"})
	state := recv(t, conn)
	if state["algorithm"] == nil {
		t.Error("getState returned nil algorithm after failed re-init; expected old simulator to still be active")
	}
}

func TestWS_Init_SendsStateAfterSuccess(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{
		"type": "init", "algorithm": "fcfs",
		"processes": []map[string]interface{}{
			{"pid": 1, "arrivalTime": 0, "burstTime": 3},
		},
	})
	// First message: success
	r1 := drain(t, conn, "success")
	if !strings.Contains(r1["message"].(string), "initialized") {
		t.Fatalf("expected 'initialized' in message, got: %v", r1)
	}
	// Second message: state snapshot (no 'type' key — it's a SimulationUpdate)
	// We drain one more message and check it looks like a state.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected state snapshot after init: %v", err)
	}
	var state simulator.SimulationUpdate
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if state.State == "" {
		t.Error("state snapshot has empty State field")
	}
}

// ── WebSocket: getState ───────────────────────────────────────────────────────

func TestWS_GetState_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "getState"})
	r := drain(t, conn, "error")
	if msg, _ := r["message"].(string); !strings.Contains(strings.ToLower(msg), "not initialized") {
		t.Errorf("error = %q, want 'not initialized'", msg)
	}
}

func TestWS_GetState_AfterInit(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "getState"})
	// getState writes directly, no broadcast race — first unread message is the state.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var state map[string]interface{}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if state["state"] == nil {
		t.Error("getState response missing 'state' field")
	}
}

// ── WebSocket: start/pause/resume/stop ───────────────────────────────────────

func TestWS_Start_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "start"})
	r := drain(t, conn, "error")
	if msg, _ := r["message"].(string); !strings.Contains(strings.ToLower(msg), "not initialized") {
		t.Errorf("error = %q, want 'not initialized'", msg)
	}
}

func TestWS_Start_AfterInit(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "start"})
	r := drain(t, conn, "success")
	if msg, _ := r["message"].(string); !strings.Contains(strings.ToLower(msg), "start") {
		t.Errorf("start response = %q", msg)
	}
}

func TestWS_Pause_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "pause"})
	drain(t, conn, "error")
}

func TestWS_Pause_AfterStart(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "start"})
	drain(t, conn, "success")
	time.Sleep(60 * time.Millisecond)
	sendJSON(t, conn, map[string]interface{}{"type": "pause"})
	drain(t, conn, "success")
}

func TestWS_Resume_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "resume"})
	drain(t, conn, "error")
}

func TestWS_Resume_AfterPause(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "rr")
	sendJSON(t, conn, map[string]interface{}{"type": "start"})
	drain(t, conn, "success")
	time.Sleep(60 * time.Millisecond)
	sendJSON(t, conn, map[string]interface{}{"type": "pause"})
	drain(t, conn, "success")
	sendJSON(t, conn, map[string]interface{}{"type": "resume"})
	drain(t, conn, "success")
}

func TestWS_Stop_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "stop"})
	drain(t, conn, "error")
}

func TestWS_Stop_AfterStart(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "start"})
	drain(t, conn, "success")
	time.Sleep(60 * time.Millisecond)
	sendJSON(t, conn, map[string]interface{}{"type": "stop"})
	drain(t, conn, "success")
}

// ── WebSocket: step ───────────────────────────────────────────────────────────

func TestWS_Step_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "step"})
	drain(t, conn, "error")
}

func TestWS_Step_AfterInit(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	for i := 0; i < 3; i++ {
		sendJSON(t, conn, map[string]interface{}{"type": "step"})
		drain(t, conn, "success")
	}
	sendJSON(t, conn, map[string]interface{}{"type": "getState"})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, _ := conn.ReadMessage()
	var state map[string]interface{}
	_ = json.Unmarshal(data, &state)
	if ct, _ := state["currentTime"].(float64); ct != 3 {
		t.Errorf("currentTime after 3 steps = %v, want 3", state["currentTime"])
	}
}

// ── WebSocket: reset ──────────────────────────────────────────────────────────

func TestWS_Reset_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "reset"})
	drain(t, conn, "error")
}

func TestWS_Reset_AfterSteps(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	for i := 0; i < 2; i++ {
		sendJSON(t, conn, map[string]interface{}{"type": "step"})
		drain(t, conn, "success")
	}
	sendJSON(t, conn, map[string]interface{}{"type": "reset"})
	drain(t, conn, "success")

	sendJSON(t, conn, map[string]interface{}{"type": "getState"})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, _ := conn.ReadMessage()
	var state map[string]interface{}
	_ = json.Unmarshal(data, &state)
	if ct, _ := state["currentTime"].(float64); ct != 0 {
		t.Errorf("currentTime after reset = %v, want 0", state["currentTime"])
	}
}

// ── WebSocket: speed ──────────────────────────────────────────────────────────

func TestWS_Speed_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{"type": "speed", "speed": float64(100)})
	drain(t, conn, "error")
}

func TestWS_Speed_Valid(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "speed", "speed": float64(200)})
	r := drain(t, conn, "success")
	if msg, _ := r["message"].(string); !strings.Contains(msg, "200") {
		t.Errorf("speed message = %q, want '200'", msg)
	}
}

func TestWS_Speed_BelowMin(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "speed", "speed": float64(0)})
	drain(t, conn, "error")
}

func TestWS_Speed_InvalidType(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "speed", "speed": "fast"})
	drain(t, conn, "error")
}

// ── WebSocket: addProcess ─────────────────────────────────────────────────────

func TestWS_AddProcess_NotInitialized(t *testing.T) {
	_, conn := newTestWS(t)
	sendJSON(t, conn, map[string]interface{}{
		"type":    "addProcess",
		"process": map[string]interface{}{"pid": 1, "arrivalTime": 0, "burstTime": 3},
	})
	drain(t, conn, "error")
}

func TestWS_AddProcess_Valid(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{
		"type":    "addProcess",
		"process": map[string]interface{}{"pid": 5, "name": "P5", "arrivalTime": 0, "burstTime": 3, "priority": 0},
	})
	r := drain(t, conn, "success")
	if msg, _ := r["message"].(string); !strings.Contains(msg, "P5") {
		t.Errorf("addProcess message = %q, want mention of P5", msg)
	}
}

func TestWS_AddProcess_InvalidData(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{
		"type":    "addProcess",
		"process": map[string]interface{}{"pid": -5, "arrivalTime": 0, "burstTime": 3},
	})
	drain(t, conn, "error")
}

func TestWS_AddProcess_MissingProcessField(t *testing.T) {
	_, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "addProcess"}) // no "process" key
	drain(t, conn, "error")
}

// ── WebSocket: broadcast ──────────────────────────────────────────────────────

func TestHandleBroadcasts_DeliverToClients(t *testing.T) {
	s, conn := newTestWS(t)
	// Directly inject a state update into the broadcast channel.
	update := &simulator.SimulationUpdate{
		State:     simulator.SimStateIdle,
		Algorithm: "test-broadcast",
	}
	select {
	case s.broadcast <- update:
	case <-time.After(time.Second):
		t.Fatal("broadcast channel blocked")
	}
	// The handleBroadcasts goroutine should deliver it to conn.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("did not receive broadcast: %v", err)
	}
	var got simulator.SimulationUpdate
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal broadcast: %v", err)
	}
	if got.Algorithm != "test-broadcast" {
		t.Errorf("Algorithm = %q, want \"test-broadcast\"", got.Algorithm)
	}
}

// ── WebSocket: Shutdown ───────────────────────────────────────────────────────

func TestShutdown_Idempotent(t *testing.T) {
	s := NewServer(testCfg())
	// Calling Shutdown twice must not panic.
	s.Shutdown()
	s.Shutdown()
}

func TestShutdown_ClosesConnectedClients(t *testing.T) {
	s := NewServer(testCfg())
	ts := httptest.NewServer(http.HandlerFunc(s.HandleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	s.Shutdown()

	// The connection should be closed by Shutdown.
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Error("expected connection to be closed after Shutdown, but ReadMessage succeeded")
	}
}

func TestShutdown_WithRunningSimulator(t *testing.T) {
	s, conn := newTestWS(t)
	initSim(t, conn, "fcfs")
	sendJSON(t, conn, map[string]interface{}{"type": "start"})
	drain(t, conn, "success")
	time.Sleep(60 * time.Millisecond)
	// Shutdown while sim is running must not panic or deadlock.
	done := make(chan struct{})
	go func() {
		s.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown hung with a running simulator")
	}
}
