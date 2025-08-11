package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	DefaultServiceName          = "kite-mcp"
	DefaultHistoricalDays       = 7
	DefaultCleanupRetentionDays = 30
	DefaultCleanupHour          = 3 // 3 AM
	DefaultCleanupDay           = 6 // Saturday (0=Sunday, 6=Saturday)

	PrometheusContentType = "text/plain; version=0.0.4; charset=utf-8"
	AdminPathPrefix       = "/admin/"
	MetricsPathSuffix     = "/metrics"
)

// Config holds configuration for creating a metrics manager
type Config struct {
	ServiceName          string // defaults to DefaultServiceName
	AdminSecretPath      string // required for admin endpoint, empty = disabled
	HistoricalDays       int    // defaults to DefaultHistoricalDays
	CleanupRetentionDays int    // defaults to DefaultCleanupRetentionDays
	AutoCleanup          bool   // defaults to true
}

// Manager handles metrics collection and export
type Manager struct {
	serviceName          string
	adminSecretPath      string
	historicalDays       int
	cleanupRetentionDays int

	// User tracking for daily metrics
	dailyUsers sync.Map // map[string]*userSet

	// Prometheus metrics
	registry        *prometheus.Registry
	toolCallsVec    *prometheus.CounterVec
	toolErrorsVec   *prometheus.CounterVec
	dailyUsersVec   *prometheus.GaugeVec
	genericCounters sync.Map // map[string]prometheus.Counter for dynamic counters

	cleanupStop chan struct{}
	cleanupOnce sync.Once
}

// userSet holds unique users for a day with count
type userSet struct {
	users sync.Map // map[string]bool
	count int64    // atomic counter
}

// New creates a new metrics manager with the given configuration
func New(cfg Config) *Manager {
	if cfg.ServiceName == "" {
		cfg.ServiceName = DefaultServiceName
	}
	if cfg.HistoricalDays == 0 {
		cfg.HistoricalDays = DefaultHistoricalDays
	}
	if cfg.CleanupRetentionDays == 0 {
		cfg.CleanupRetentionDays = DefaultCleanupRetentionDays
	}

	// Create Prometheus registry
	registry := prometheus.NewRegistry()

	// Create Prometheus metrics with proper labeling
	toolCallsVec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_calls_total",
			Help: "Total number of tool calls",
		},
		[]string{"tool", "session_type", "date", "service"},
	)

	toolErrorsVec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_errors_total",
			Help: "Total number of tool errors",
		},
		[]string{"tool", "error_type", "session_type", "date", "service"},
	)

	dailyUsersVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "daily_unique_users_total",
			Help: "Number of unique users per day",
		},
		[]string{"date", "service"},
	)

	// Register metrics
	registry.MustRegister(toolCallsVec, toolErrorsVec, dailyUsersVec)

	m := &Manager{
		serviceName:          cfg.ServiceName,
		adminSecretPath:      cfg.AdminSecretPath,
		historicalDays:       cfg.HistoricalDays,
		cleanupRetentionDays: cfg.CleanupRetentionDays,
		registry:             registry,
		toolCallsVec:         toolCallsVec,
		toolErrorsVec:        toolErrorsVec,
		dailyUsersVec:        dailyUsersVec,
		cleanupStop:          make(chan struct{}),
	}

	if cfg.AutoCleanup {
		m.startCleanupRoutine()
	}

	return m
}

// Increment atomically increments a counter
func (m *Manager) Increment(key string) {
	m.IncrementBy(key, 1)
}

// IncrementBy atomically increments a counter by n
func (m *Manager) IncrementBy(key string, n int64) {
	// Get or create Prometheus counter for this key
	counterInterface, _ := m.genericCounters.LoadOrStore(key, prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: strings.ReplaceAll(key, "-", "_"),
			Help: fmt.Sprintf("Count for %s", key),
			ConstLabels: prometheus.Labels{
				"service": m.serviceName,
			},
		},
	))

	if counter, ok := counterInterface.(prometheus.Counter); ok {
		// Try to register the counter (ignore already registered errors)
		m.registry.Register(counter) //nolint:all
		counter.Add(float64(n))
	}
}

// IncrementDaily atomically increments a daily counter for today
func (m *Manager) IncrementDaily(key string) {
	m.IncrementDailyBy(key, 1)
}

// IncrementDailyBy atomically increments a daily counter by n for today
func (m *Manager) IncrementDailyBy(key string, n int64) {
	today := time.Now().UTC().Format("2006-01-02")
	dailyKey := fmt.Sprintf("%s_%s", key, today)

	// Get or create Prometheus counter for this daily key
	counterInterface, _ := m.genericCounters.LoadOrStore(dailyKey, prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: strings.ReplaceAll(key, "-", "_"),
			Help: fmt.Sprintf("Daily count for %s", key),
			ConstLabels: prometheus.Labels{
				"service": m.serviceName,
				"date":    today,
			},
		},
	))

	if counter, ok := counterInterface.(prometheus.Counter); ok {
		// Try to register the counter (ignore already registered errors)
		m.registry.Register(counter) //nolint:all
		counter.Add(float64(n))
	}
}

// IncrementDailyWithLabels atomically increments a daily counter with labels for today
func (m *Manager) IncrementDailyWithLabels(key string, labels map[string]string) {
	m.IncrementDailyWithLabelsBy(key, labels, 1)
}

// IncrementDailyWithLabelsBy atomically increments a daily counter with labels by n for today
func (m *Manager) IncrementDailyWithLabelsBy(key string, labels map[string]string, n int64) {
	today := time.Now().UTC().Format("2006-01-02")

	// Use Prometheus metrics for tool calls and errors
	switch key {
	case "tool_calls":
		if tool, ok := labels["tool"]; ok {
			sessionType := labels["session_type"]
			if sessionType == "" {
				sessionType = "unknown"
			}
			m.toolCallsVec.WithLabelValues(tool, sessionType, today, m.serviceName).Add(float64(n))
		}
	case "tool_errors":
		if tool, ok := labels["tool"]; ok {
			errorType := labels["error_type"]
			sessionType := labels["session_type"]
			if sessionType == "" {
				sessionType = "unknown"
			}
			if errorType == "" {
				errorType = "unknown"
			}
			m.toolErrorsVec.WithLabelValues(tool, errorType, sessionType, today, m.serviceName).Add(float64(n))
		}
	}
}

// TrackDailyUser tracks a unique user login for today
func (m *Manager) TrackDailyUser(userID string) {
	if userID == "" {
		return
	}

	today := time.Now().UTC().Format("2006-01-02")

	dayUsersInterface, _ := m.dailyUsers.LoadOrStore(today, &userSet{})
	dayUsers, ok := dayUsersInterface.(*userSet)
	if !ok {
		return // Skip if type assertion fails
	}

	if _, exists := dayUsers.users.LoadOrStore(userID, true); !exists {
		atomic.AddInt64(&dayUsers.count, 1)
		// Update Prometheus gauge
		m.dailyUsersVec.WithLabelValues(today, m.serviceName).Set(float64(atomic.LoadInt64(&dayUsers.count)))
	}
}

// GetDailyUserCount returns unique user count for a specific date
func (m *Manager) GetDailyUserCount(date string) int64 {
	if dayUsersInterface, ok := m.dailyUsers.Load(date); ok {
		if dayUsers, ok := dayUsersInterface.(*userSet); ok {
			return atomic.LoadInt64(&dayUsers.count)
		}
	}
	return 0
}

// GetTodayUserCount returns today's unique user count
func (m *Manager) GetTodayUserCount() int64 {
	today := time.Now().UTC().Format("2006-01-02")
	return m.GetDailyUserCount(today)
}

// CleanupOldData removes user data older than the configured retention period
func (m *Manager) CleanupOldData() error {
	cutoff := time.Now().UTC().AddDate(0, 0, -m.cleanupRetentionDays)

	var keysToDelete []string
	m.dailyUsers.Range(func(key, _ interface{}) bool {
		dateStr, ok := key.(string)
		if !ok {
			return true
		}

		if date, err := time.Parse("2006-01-02", dateStr); err == nil && date.Before(cutoff) {
			keysToDelete = append(keysToDelete, dateStr)
		}
		return true
	})

	for _, key := range keysToDelete {
		m.dailyUsers.Delete(key)
	}

	return nil
}

// startCleanupRoutine starts automatic cleanup every Saturday at 3 AM UTC
func (m *Manager) startCleanupRoutine() {
	go func() {
		for {
			now := time.Now().UTC()
			next := getNextCleanupTime(now)
			delay := next.Sub(now)

			select {
			case <-time.After(delay):
				_ = m.CleanupOldData()
			case <-m.cleanupStop:
				return
			}
		}
	}()
}

// getNextCleanupTime calculates the next Saturday at 3 AM UTC
func getNextCleanupTime(now time.Time) time.Time {
	// Find next Saturday at 3 AM
	daysUntilSaturday := (DefaultCleanupDay - int(now.Weekday()) + 7) % 7
	if daysUntilSaturday == 0 && (now.Hour() >= DefaultCleanupHour) {
		daysUntilSaturday = 7 // Next Saturday if we're past 3 AM today
	}

	next := now.AddDate(0, 0, daysUntilSaturday)
	return time.Date(next.Year(), next.Month(), next.Day(), DefaultCleanupHour, 0, 0, 0, time.UTC)
}

// Shutdown stops the cleanup routine
func (m *Manager) Shutdown() {
	m.cleanupOnce.Do(func() {
		close(m.cleanupStop)
	})
}

// HTTPHandler returns an HTTP handler for the metrics endpoint
func (m *Manager) HTTPHandler() http.HandlerFunc {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}).ServeHTTP
}

// AdminHTTPHandler returns an HTTP handler with admin path protection
func (m *Manager) AdminHTTPHandler() http.HandlerFunc {
	if m.adminSecretPath == "" {
		return m.disabledHandler()
	}

	expectedPath := AdminPathPrefix + m.adminSecretPath + MetricsPathSuffix

	return func(w http.ResponseWriter, r *http.Request) {
		if !m.isValidAdminPath(r.URL.Path, expectedPath) {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		m.HTTPHandler()(w, r)
	}
}

// disabledHandler returns a handler that always returns 404
func (m *Manager) disabledHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Admin endpoint disabled", http.StatusNotFound)
	}
}

// isValidAdminPath checks if the request path matches the expected admin path
func (m *Manager) isValidAdminPath(requestPath, expectedPath string) bool {
	return requestPath == expectedPath
}
