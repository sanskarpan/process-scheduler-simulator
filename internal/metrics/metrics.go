// Package metrics provides Prometheus instrumentation for the server: HTTP
// request latency, error counters, WebSocket client gauges, and simulation
// lifecycle counters.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// httpRequestsTotal counts HTTP requests by method, path template, and
	// status code class.
	httpRequestsTotal *prometheus.CounterVec

	// httpRequestDuration observes request latency in seconds.
	httpRequestDuration *prometheus.HistogramVec

	// httpRequestSize observes request body sizes in bytes.
	httpRequestSize *prometheus.HistogramVec

	// wsClientsGauge tracks the current number of connected WebSocket clients.
	wsClientsGauge prometheus.Gauge

	// wsErrorsTotal counts WebSocket read/write errors by direction.
	wsErrorsTotal *prometheus.CounterVec

	// simStartedTotal counts simulations started by algorithm.
	simStartedTotal *prometheus.CounterVec

	// simCompletedTotal counts simulations completed by algorithm.
	simCompletedTotal *prometheus.CounterVec

	// simStepsTotal counts simulation time units executed by algorithm.
	simStepsTotal *prometheus.CounterVec

	// simDuration observes end-to-end simulation wall-clock duration by
	// algorithm.
	simDuration *prometheus.HistogramVec

	// registry is the custom registry used by Handler(). Tests can register a
	// fresh registry via NewRegistry.
	registry *prometheus.Registry
)

func init() {
	registry = prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewBuildInfoCollector())
	register(registry)
}

func register(reg *prometheus.Registry) {
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_http_requests_total",
		Help: "Total HTTP requests by method, path, and status class.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "scheduler_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	httpRequestSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "scheduler_http_request_bytes",
		Help:    "HTTP request body size in bytes.",
		Buckets: prometheus.ExponentialBuckets(64, 4, 10),
	}, []string{"method", "path"})

	wsClientsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "scheduler_ws_clients",
		Help: "Current number of connected WebSocket clients.",
	})

	wsErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_ws_errors_total",
		Help: "WebSocket read/write errors by direction.",
	}, []string{"direction"})

	simStartedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_sim_started_total",
		Help: "Simulations started by algorithm.",
	}, []string{"algorithm"})

	simCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_sim_completed_total",
		Help: "Simulations completed by algorithm.",
	}, []string{"algorithm"})

	simStepsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_sim_steps_total",
		Help: "Simulation time units executed by algorithm.",
	}, []string{"algorithm"})

	simDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "scheduler_sim_duration_seconds",
		Help:    "End-to-end simulation wall-clock duration by algorithm.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
	}, []string{"algorithm"})

	reg.MustRegister(
		httpRequestsTotal, httpRequestDuration, httpRequestSize,
		wsClientsGauge, wsErrorsTotal,
		simStartedTotal, simCompletedTotal, simStepsTotal, simDuration,
	)
}

// Handler returns the Prometheus /metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// IncClient increments the connected-client gauge.
func IncClient() { wsClientsGauge.Inc() }

// DecClient decrements the connected-client gauge.
func DecClient() { wsClientsGauge.Dec() }

// IncWSError increments the WebSocket error counter by direction (read/write).
func IncWSError(direction string) { wsErrorsTotal.WithLabelValues(direction).Inc() }

// SimStarted increments the started counter for the given algorithm.
func SimStarted(algorithm string) { simStartedTotal.WithLabelValues(algorithm).Inc() }

// SimCompleted increments the completed counter and observes duration.
func SimCompleted(algorithm string, duration time.Duration) {
	simCompletedTotal.WithLabelValues(algorithm).Inc()
	simDuration.WithLabelValues(algorithm).Observe(duration.Seconds())
}

// SimSteps records the number of time units executed for an algorithm.
func SimSteps(algorithm string, steps int) {
	simStepsTotal.WithLabelValues(algorithm).Add(float64(steps))
}

// ObserveHTTPRequest records an HTTP request's method, path, status, and
// duration. path should be the matched route template (e.g. "/ws"), not the
// raw URL, to keep cardinality bounded.
func ObserveHTTPRequest(method, path string, status int, duration time.Duration, reqBytes int64) {
	sc := statusClass(status)
	httpRequestsTotal.WithLabelValues(method, path, sc).Inc()
	httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
	if reqBytes > 0 {
		httpRequestSize.WithLabelValues(method, path).Observe(float64(reqBytes))
	}
}

func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return strconv.Itoa(status)
	}
}
