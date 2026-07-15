package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sanskar/scheduler-simulator/internal/config"
	"github.com/sanskar/scheduler-simulator/internal/logging"
	"github.com/sanskar/scheduler-simulator/internal/metrics"
	"github.com/sanskar/scheduler-simulator/internal/process"
	"github.com/sanskar/scheduler-simulator/internal/scheduler"
	"github.com/sanskar/scheduler-simulator/internal/simulator"
)

// wsConn wraps a WebSocket connection with a per-connection write mutex.
// gorilla/websocket requires that at most one goroutine writes at a time;
// the mutex serializes the broadcast goroutine, the pinger, and any
// in-handler response writes.
type wsConn struct {
	conn      *websocket.Conn
	mu        sync.Mutex
	writeWait time.Duration
}

func newWSConn(conn *websocket.Conn, writeWait time.Duration) *wsConn {
	return &wsConn{conn: conn, writeWait: writeWait}
}

func (c *wsConn) writeJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
	return c.conn.WriteJSON(v)
}

func (c *wsConn) writeMessage(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
	return c.conn.WriteMessage(msgType, data)
}

// Server manages WebSocket connections and the simulator. It is safe for
// concurrent use by many WebSocket handlers.
type Server struct {
	cfg       config.Config
	mu        sync.RWMutex
	simulator *simulator.Simulator
	clients   map[*wsConn]struct{}
	broadcast chan *simulator.SimulationUpdate
	server    *http.Server
	closed    chan struct{}
	closeOnce sync.Once
}

// NewServer creates a new server with the given configuration.
func NewServer(cfg config.Config) *Server {
	s := &Server{
		cfg:       cfg,
		clients:   make(map[*wsConn]struct{}),
		broadcast: make(chan *simulator.SimulationUpdate, cfg.BroadcastBufferSize),
		closed:    make(chan struct{}),
	}
	go s.handleBroadcasts()
	return s
}

// upgrader returns a pointer to a WebSocket upgrader with origin checks based
// on config. A pointer is required because gorilla's Upgrade method has a
// pointer receiver.
func (s *Server) upgrader() *websocket.Upgrader {
	allowed := s.cfg.WSOriginAllow
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			host := r.Host
			if origin == "http://"+host || origin == "https://"+host {
				return true
			}
			for _, a := range allowed {
				if a == "*" || a == origin {
					return true
				}
			}
			// Permit local development from common dev ports.
			for _, p := range []string{"http://localhost", "http://127.0.0.1"} {
				if origin == p || (len(origin) > len(p) && origin[:len(p)] == p && origin[len(p)] == ':') {
					return true
				}
			}
			return false
		},
	}
}

// handleBroadcasts fans updates out to all connected clients. Each write is
// serialized through the per-connection wsConn mutex.
func (s *Server) handleBroadcasts() {
	for update := range s.broadcast {
		s.mu.RLock()
		clients := make([]*wsConn, 0, len(s.clients))
		for c := range s.clients {
			clients = append(clients, c)
		}
		s.mu.RUnlock()

		for _, wc := range clients {
			if err := wc.writeJSON(update); err != nil {
				logging.Logger.Warn("websocket write error", "error", err)
				metrics.IncWSError("write")
				s.unregisterClient(wc)
			}
		}
	}
}

func (s *Server) unregisterClient(wc *wsConn) {
	s.mu.Lock()
	if _, ok := s.clients[wc]; ok {
		delete(s.clients, wc)
		s.mu.Unlock()
		_ = wc.conn.Close()
		return
	}
	s.mu.Unlock()
}

// HandleWebSocket handles WebSocket connections.
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader().Upgrade(w, r, nil)
	if err != nil {
		logging.FromContext(r.Context()).Warn("websocket upgrade error", "error", err)
		return
	}

	wc := newWSConn(conn, s.cfg.WSWriteWait)

	s.mu.Lock()
	if s.cfg.MaxClients > 0 && len(s.clients) >= s.cfg.MaxClients {
		s.mu.Unlock()
		_ = conn.Close()
		return
	}
	s.clients[wc] = struct{}{}
	n := len(s.clients)
	s.mu.Unlock()
	metrics.IncClient()
	logging.FromContext(r.Context()).Info("client connected", "clients", n)

	// Send initial state if a simulator exists.
	if sim := s.getSimulator(); sim != nil {
		_ = wc.writeJSON(sim.GetCurrentState())
	}

	// Reader loop setup — only the reader goroutine reads; writes go via wc.
	conn.SetReadLimit(s.cfg.WSReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(s.cfg.WSPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(s.cfg.WSPongWait))
		return nil
	})

	// Pinger: keeps the connection alive and detects dead clients.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(s.cfg.WSPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.mu.RLock()
				_, ok := s.clients[wc]
				s.mu.RUnlock()
				if !ok {
					return
				}
				if err := wc.writeMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-done:
				return
			case <-s.closed:
				return
			}
		}
	}()

	defer func() {
		close(done)
		s.mu.Lock()
		delete(s.clients, wc)
		n := len(s.clients)
		s.mu.Unlock()
		metrics.DecClient()
		_ = conn.Close()
		logging.FromContext(r.Context()).Info("client disconnected", "clients", n)
	}()

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				metrics.IncWSError("read")
				logging.FromContext(r.Context()).Warn("websocket read error", "error", err)
			}
			break
		}
		s.handleMessage(wc, msg)
	}
}

// getSimulator returns the current simulator under the read lock.
func (s *Server) getSimulator() *simulator.Simulator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.simulator
}

// handleMessage processes messages from clients.
func (s *Server) handleMessage(wc *wsConn, msg map[string]interface{}) {
	msgType, ok := msg["type"].(string)
	if !ok {
		s.sendError(wc, "Invalid message format: missing 'type'")
		return
	}

	switch msgType {
	case "init":
		s.handleInit(wc, msg)
	case "start":
		s.handleStart(wc)
	case "pause":
		s.handlePause(wc)
	case "resume":
		s.handleResume(wc)
	case "stop":
		s.handleStop(wc)
	case "reset":
		s.handleReset(wc)
	case "step":
		s.handleStep(wc)
	case "speed":
		s.handleSpeed(wc, msg)
	case "addProcess":
		s.handleAddProcess(wc, msg)
	case "getState":
		s.handleGetState(wc)
	default:
		s.sendError(wc, fmt.Sprintf("Unknown message type: %s", msgType))
	}
}

// parseProcess extracts a process from a loosely-typed JSON map. Returns an
// error instead of panicking on missing/invalid fields.
func parseProcess(pMap map[string]interface{}) (*process.Process, error) {
	getInt := func(key string) (int, error) {
		v, ok := pMap[key]
		if !ok {
			return 0, fmt.Errorf("missing field %q", key)
		}
		f, ok := v.(float64)
		if !ok {
			return 0, fmt.Errorf("field %q must be a number", key)
		}
		return int(f), nil
	}
	getString := func(key string) (string, error) {
		v, ok := pMap[key]
		if !ok {
			return "", fmt.Errorf("missing field %q", key)
		}
		str, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("field %q must be a string", key)
		}
		return str, nil
	}

	pid, err := getInt("pid")
	if err != nil {
		return nil, err
	}
	name, err := getString("name")
	if err != nil {
		// Default name if omitted.
		name = fmt.Sprintf("P%d", pid)
	}
	arrival, err := getInt("arrivalTime")
	if err != nil {
		return nil, err
	}
	burst, err := getInt("burstTime")
	if err != nil {
		return nil, err
	}
	priority, err := getInt("priority")
	if err != nil {
		// Priority is optional; default 0.
		priority = 0
	}
	if arrival < 0 {
		return nil, errors.New("arrivalTime must be >= 0")
	}
	if burst < 0 {
		return nil, errors.New("burstTime must be >= 0")
	}
	if pid < 0 {
		return nil, errors.New("pid must be >= 0")
	}
	return process.NewProcess(pid, name, arrival, burst, priority), nil
}

// handleInit initializes the simulator with algorithm and processes.
func (s *Server) handleInit(wc *wsConn, msg map[string]interface{}) {
	algorithm, _ := msg["algorithm"].(string)
	timeQuantum := 4 // Default time quantum
	if tq, ok := msg["timeQuantum"].(float64); ok {
		timeQuantum = int(tq)
	}

	// Create scheduler based on algorithm
	var sched scheduler.Scheduler
	switch algorithm {
	case "fcfs":
		sched = scheduler.NewFCFSScheduler()
	case "sjf":
		sched = scheduler.NewSJFScheduler()
	case "srtf":
		sched = scheduler.NewSRTFScheduler()
	case "rr":
		sched = scheduler.NewRoundRobinScheduler(timeQuantum)
	case "priority":
		sched = scheduler.NewPriorityScheduler(true)
	case "priority_np":
		sched = scheduler.NewPriorityScheduler(false)
	case "cfs":
		sched = scheduler.NewCFSScheduler()
	case "mlfq":
		sched = scheduler.NewMLFQScheduler()
	case "lottery":
		sched = scheduler.NewLotteryScheduler(timeQuantum, nil)
	case "mlq":
		sched = scheduler.NewMLQScheduler(3, timeQuantum)
	default:
		s.sendError(wc, fmt.Sprintf("unknown algorithm %q; valid values: fcfs, sjf, srtf, rr, priority, priority_np, cfs, mlfq, lottery, mlq", algorithm))
		return
	}

	// Stop the prior simulator (if any) before replacing it, so its engine
	// goroutine exits cleanly and does not leak.
	if old := s.getSimulator(); old != nil {
		old.Stop()
	}

	newSim := simulator.NewSimulator(sched)
	newSim.SetUpdateCallback(func(update *simulator.SimulationUpdate) {
		// Non-blocking send: if the broadcast queue is full, drop the update
		// rather than stalling the engine. This bounds memory under load.
		select {
		case s.broadcast <- update:
		case <-s.closed:
		default:
			logging.Logger.Warn("broadcast queue full; dropping simulation update")
		}
	})

	// Add processes if provided
	if processesData, ok := msg["processes"].([]interface{}); ok {
		for _, pData := range processesData {
			pMap, ok := pData.(map[string]interface{})
			if !ok {
				s.sendError(wc, "Invalid process entry: expected object")
				return
			}
			proc, err := parseProcess(pMap)
			if err != nil {
				s.sendError(wc, fmt.Sprintf("Invalid process data: %v", err))
				return
			}
			newSim.AddProcess(proc)
		}
	}

	s.mu.Lock()
	s.simulator = newSim
	s.mu.Unlock()

	s.sendSuccess(wc, "Simulator initialized")
	_ = wc.writeJSON(newSim.GetCurrentState())
}

// handleStart starts the simulation
func (s *Server) handleStart(wc *wsConn) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}
	sim.Start()
	s.sendSuccess(wc, "Simulation started")
}

// handlePause pauses the simulation
func (s *Server) handlePause(wc *wsConn) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}
	sim.Pause()
	s.sendSuccess(wc, "Simulation paused")
}

// handleResume resumes the simulation
func (s *Server) handleResume(wc *wsConn) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}
	sim.Resume()
	s.sendSuccess(wc, "Simulation resumed")
}

// handleStop stops the simulation
func (s *Server) handleStop(wc *wsConn) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}
	sim.Stop()
	s.sendSuccess(wc, "Simulation stopped")
}

// handleReset resets the simulation
func (s *Server) handleReset(wc *wsConn) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}
	sim.Reset()
	s.sendSuccess(wc, "Simulation reset")

	// Send updated state (non-blocking; broadcast goroutine delivers)
	state := sim.GetCurrentState()
	select {
	case s.broadcast <- state:
	case <-s.closed:
	default:
	}
}

// handleStep executes one simulation step
func (s *Server) handleStep(wc *wsConn) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}
	sim.Step()
	s.sendSuccess(wc, "Step executed")
}

// handleSpeed changes simulation speed
func (s *Server) handleSpeed(wc *wsConn, msg map[string]interface{}) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}

	speed, ok := msg["speed"].(float64)
	if !ok {
		s.sendError(wc, "Invalid speed value")
		return
	}
	if speed < 1 {
		s.sendError(wc, "speed must be >= 1")
		return
	}
	sim.SetSpeed(int(speed))
	s.sendSuccess(wc, fmt.Sprintf("Speed set to %d ms/unit", int(speed)))
}

// handleAddProcess adds a process dynamically
func (s *Server) handleAddProcess(wc *wsConn, msg map[string]interface{}) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}

	processData, ok := msg["process"].(map[string]interface{})
	if !ok {
		s.sendError(wc, "Invalid process data")
		return
	}

	proc, err := parseProcess(processData)
	if err != nil {
		s.sendError(wc, fmt.Sprintf("Invalid process data: %v", err))
		return
	}
	sim.AddProcess(proc)
	s.sendSuccess(wc, fmt.Sprintf("Process %s added", proc.Name))

	// Send updated state (non-blocking)
	state := sim.GetCurrentState()
	select {
	case s.broadcast <- state:
	case <-s.closed:
	default:
	}
}

// handleGetState returns current simulation state
func (s *Server) handleGetState(wc *wsConn) {
	sim := s.getSimulator()
	if sim == nil {
		s.sendError(wc, "Simulator not initialized")
		return
	}
	_ = wc.writeJSON(sim.GetCurrentState())
}

// sendSuccess sends a success message
func (s *Server) sendSuccess(wc *wsConn, message string) {
	response := map[string]interface{}{
		"type":    "success",
		"message": message,
	}
	if err := wc.writeJSON(response); err != nil {
		logging.Logger.Warn("websocket write error", "error", err)
	}
}

// sendError sends an error message
func (s *Server) sendError(wc *wsConn, message string) {
	response := map[string]interface{}{
		"type":    "error",
		"message": message,
	}
	if err := wc.writeJSON(response); err != nil {
		logging.Logger.Warn("websocket write error", "error", err)
	}
}

// HandleHealth returns server health status
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	s.mu.RLock()
	clientCount := len(s.clients)
	sim := s.simulator
	s.mu.RUnlock()

	status := map[string]interface{}{
		"status":         "healthy",
		"clients":        clientCount,
		"simulatorReady": sim != nil,
	}

	if sim != nil {
		state := sim.GetCurrentState()
		status["simulationState"] = state.State
		status["currentTime"] = state.CurrentTime
	}

	_ = json.NewEncoder(w).Encode(status)
}

// Shutdown gracefully stops the HTTP server, closes all WebSocket connections,
// and stops the simulator. It is safe to call multiple times.
func (s *Server) Shutdown() {
	s.closeOnce.Do(func() {
		close(s.closed)
		close(s.broadcast) // unblocks handleBroadcasts goroutine

		if sim := s.getSimulator(); sim != nil {
			sim.Stop()
		}

		s.mu.Lock()
		for wc := range s.clients {
			_ = wc.conn.Close()
			delete(s.clients, wc)
		}
		s.mu.Unlock()

		if s.server != nil {
			_ = s.server.Close()
		}
	})
}

// SetHTTPServer records the underlying *http.Server so Shutdown can close it.
func (s *Server) SetHTTPServer(srv *http.Server) {
	s.mu.Lock()
	s.server = srv
	s.mu.Unlock()
}

// DefaultPort returns the configured listen port from the PORT env var, or the
// provided fallback if unset/invalid.
func DefaultPort(fallback string) string {
	if p := os.Getenv("PORT"); p != "" {
		if _, err := strconv.Atoi(p); err == nil {
			return ":" + p
		}
	}
	return fallback
}
