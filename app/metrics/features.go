package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func (m *Manager) initFeatureMetrics() {
	opts := prometheus.CounterOpts{Name: "kite_mcp_operations_total", Help: "Completed MCP and HTTP operations, including credential renewal, by bounded operation and outcome.", ConstLabels: prometheus.Labels{"service": m.serviceName}}
	m.operations = prometheus.NewCounterVec(opts, []string{"tool", "mode", "outcome"})
	m.operationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "kite_mcp_operation_duration_seconds", Help: "Server operation latency, including native approval waits but excluding subsequent browser review.", ConstLabels: prometheus.Labels{"service": m.serviceName}, Buckets: []float64{.01, .05, .1, .5, 1, 5, 15, 60, 300}}, []string{"tool", "mode"})
	opts.Name, opts.Help = "kite_mcp_approval_observations_total", "Guarded order request observations, including pending native continuations and completed browser submissions; not unique orders or fills."
	m.approvals = prometheus.NewCounterVec(opts, []string{"operation", "method", "outcome", "fallback_reason", "native_attempted", "native_completed"})
	opts.Name, opts.Help = "kite_mcp_browser_approval_events_total", "Browser review, sign-in and future-order permission lifecycle events. Expiry is counted when observed."
	m.browserEvents = prometheus.NewCounterVec(opts, []string{"operation", "event"})
	m.registry.MustRegister(m.operations, m.operationDuration, m.approvals, m.browserEvents)
}

// ObserveOperation takes catalog-normalized tool and mode labels. Callers must
// never pass raw names, IDs, resource URIs, errors or arguments from requests.
func (m *Manager) ObserveOperation(tool, mode, outcome string, elapsed time.Duration) {
	m.operations.WithLabelValues(tool, mode, outcome).Inc()
	m.operationDuration.WithLabelValues(tool, mode).Observe(elapsed.Seconds())
}

func category(value string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "other"
}

func approvalOperation(operation string) string {
	return category(operation, "orders_place", "orders_modify", "orders_cancel", "permissions")
}

func (m *Manager) ObserveApproval(operation, method, outcome, fallback string, attempted, completed bool) {
	method = category(method, "none", "native", "browser", "remembered_permission")
	outcome = category(outcome, "pending", "required", "accepted", "approved", "declined", "cancelled", "expired", "invalid", "unavailable")
	fallback = category(fallback, "", "browser_unavailable", "form_not_supported", "form_support_unspecified", "native_transport_error", "malformed_native_response")
	if fallback == "" {
		fallback = "none"
	}
	m.approvals.WithLabelValues(approvalOperation(operation), method, outcome, fallback, strconv.FormatBool(attempted), strconv.FormatBool(completed)).Inc()
}

func (m *Manager) RecordBrowserApprovalEvent(operation, event string) {
	event = category(event, "created", "declined", "expired", "submission_started", "submitted", "failed", "permission_granted", "permission_revoked", "permission_expired", "login_started", "login_verified", "login_failed")
	m.browserEvents.WithLabelValues(approvalOperation(operation), event).Inc()
}

// HTTPMiddleware records only the fixed route category supplied by the caller.
// Raw paths, query strings and response bodies are never used as labels.
func (m *Manager) HTTPMiddleware(tool, mode string, next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		response := &metricResponse{ResponseWriter: w, status: http.StatusOK}
		completed := false
		defer func() {
			outcome := "success"
			switch {
			case response.status >= 500:
				outcome = "server_error"
			case response.status >= 400:
				outcome = "client_error"
			case response.status >= 300:
				outcome = "redirect"
			}
			if !completed {
				outcome = "server_error"
			}
			m.ObserveOperation(tool, mode, outcome, time.Since(start))
		}()
		next.ServeHTTP(response, r)
		completed = true
	})
}

type metricResponse struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *metricResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *metricResponse) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	// Match net/http: informational responses do not commit the final status,
	// except for a protocol switch, which ends ordinary HTTP response handling.
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricResponse) FlushError() error {
	// Resolve the same interfaces as ResponseController before committing a
	// header. An unsupported flush must leave the response writable as before.
	underlying := w.ResponseWriter
	for {
		switch flusher := underlying.(type) {
		case interface{ FlushError() error }:
			if !w.wroteHeader {
				w.WriteHeader(http.StatusOK)
			}
			return flusher.FlushError()
		case http.Flusher:
			if !w.wroteHeader {
				w.WriteHeader(http.StatusOK)
			}
			flusher.Flush()
			return nil
		case interface{ Unwrap() http.ResponseWriter }:
			underlying = flusher.Unwrap()
		default:
			return http.ErrNotSupported
		}
	}
}

func (w *metricResponse) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
