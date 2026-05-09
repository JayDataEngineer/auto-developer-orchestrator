package observability

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics exposes Prometheus counters for the orchestrator.
type Metrics struct {
	RequestsTotal      *prometheus.CounterVec
	RequestDurationSec *prometheus.HistogramVec
	ToolCallsTotal     *prometheus.CounterVec
	ToolDurationSec    *prometheus.HistogramVec
	TokensInputTotal   prometheus.Counter
	TokensOutputTotal  prometheus.Counter
	ErrorsTotal        *prometheus.CounterVec
	LLMResponsesTotal  *prometheus.CounterVec

	reg    *prometheus.Registry
	once   sync.Once
	initFn func()
}

// NewMetrics creates and registers Prometheus metrics.
func NewMetrics() *Metrics {
	m := &Metrics{}
	m.initFn = func() {
		m.reg = prometheus.NewRegistry()

		m.RequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orchestrator_requests_total",
			Help: "Total number of agent requests.",
		}, []string{"project"})
		m.reg.MustRegister(m.RequestsTotal)

		m.RequestDurationSec = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orchestrator_request_duration_seconds",
			Help:    "Agent request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"project", "status"})
		m.reg.MustRegister(m.RequestDurationSec)

		m.ToolCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orchestrator_tool_calls_total",
			Help: "Total tool calls by name.",
		}, []string{"tool_name"})
		m.reg.MustRegister(m.ToolCallsTotal)

		m.ToolDurationSec = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orchestrator_tool_duration_seconds",
			Help:    "Tool execution duration in seconds.",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms to ~80s
		}, []string{"tool_name"})
		m.reg.MustRegister(m.ToolDurationSec)

		m.TokensInputTotal = prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orchestrator_tokens_input_total",
			Help: "Total input tokens consumed.",
		})
		m.reg.MustRegister(m.TokensInputTotal)

		m.TokensOutputTotal = prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orchestrator_tokens_output_total",
			Help: "Total output tokens produced.",
		})
		m.reg.MustRegister(m.TokensOutputTotal)

		m.ErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orchestrator_errors_total",
			Help: "Total errors by type.",
		}, []string{"error_type"})
		m.reg.MustRegister(m.ErrorsTotal)

		m.LLMResponsesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orchestrator_llm_responses_total",
			Help: "Total LLM responses by finish reason.",
		}, []string{"model", "finish_reason"})
		m.reg.MustRegister(m.LLMResponsesTotal)
	}
	return m
}

func (m *Metrics) ensure() {
	if m.initFn != nil {
		m.once.Do(m.initFn)
	}
}

// RecordRequest increments request counter and observes duration.
func (m *Metrics) RecordRequest(project, status string, duration time.Duration) {
	m.ensure()
	m.RequestsTotal.WithLabelValues(project).Inc()
	m.RequestDurationSec.WithLabelValues(project, status).Observe(duration.Seconds())
}

// RecordToolCall increments tool counter and observes duration.
func (m *Metrics) RecordToolCall(toolName string, duration time.Duration) {
	m.ensure()
	m.ToolCallsTotal.WithLabelValues(toolName).Inc()
	m.ToolDurationSec.WithLabelValues(toolName).Observe(duration.Seconds())
}

// RecordTokens adds to the token counters.
func (m *Metrics) RecordTokens(input, output int) {
	m.ensure()
	m.TokensInputTotal.Add(float64(input))
	m.TokensOutputTotal.Add(float64(output))
}

// RecordError increments the error counter for the given type.
func (m *Metrics) RecordError(errorType string) {
	m.ensure()
	m.ErrorsTotal.WithLabelValues(errorType).Inc()
}

// RecordLLMResponse increments the LLM response counter.
func (m *Metrics) RecordLLMResponse(model, finishReason string) {
	m.ensure()
	m.LLMResponsesTotal.WithLabelValues(model, finishReason).Inc()
}

// HTTPHandler returns an http.Handler for the /metrics endpoint.
func (m *Metrics) HTTPHandler() http.Handler {
	m.ensure()
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Registry returns the underlying Prometheus registry for external consumers
// (e.g., push gateway).
func (m *Metrics) Registry() *prometheus.Registry {
	m.ensure()
	return m.reg
}

// Middleware returns a func that records request metrics for an HTTP handler.
func (m *Metrics) Middleware(project string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := strconv.Itoa(sw.status)
		if sw.status == 0 {
			status = "200"
		}
		m.RecordRequest(project, status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
