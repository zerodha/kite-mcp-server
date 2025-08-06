package metrics

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

	counters   sync.Map // map[string]*int64
	dailyUsers sync.Map // map[string]*userSet

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

	m := &Manager{
		serviceName:          cfg.ServiceName,
		adminSecretPath:      cfg.AdminSecretPath,
		historicalDays:       cfg.HistoricalDays,
		cleanupRetentionDays: cfg.CleanupRetentionDays,
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
	val, _ := m.counters.LoadOrStore(key, new(int64))
	atomic.AddInt64(val.(*int64), n)
}

// IncrementDaily atomically increments a daily counter for today
func (m *Manager) IncrementDaily(key string) {
	m.IncrementDailyBy(key, 1)
}

// IncrementDailyBy atomically increments a daily counter by n for today
func (m *Manager) IncrementDailyBy(key string, n int64) {
	today := time.Now().UTC().Format("2006-01-02")
	dailyKey := fmt.Sprintf("%s_%s", key, today)
	val, _ := m.counters.LoadOrStore(dailyKey, new(int64))
	atomic.AddInt64(val.(*int64), n)
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
	}
}

// GetCounterValue returns the current value of a counter
func (m *Manager) GetCounterValue(key string) int64 {
	if val, ok := m.counters.Load(key); ok {
		return atomic.LoadInt64(val.(*int64))
	}
	return 0
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

// isDailyMetric checks if a metric key has a date suffix (YYYY-MM-DD format)
func (m *Manager) isDailyMetric(key string) bool {
	parts := strings.Split(key, "_")
	if len(parts) < 2 {
		return false
	}

	// Check if the last part looks like a date (YYYY-MM-DD)
	lastPart := parts[len(parts)-1]
	if len(lastPart) != 10 || strings.Count(lastPart, "-") != 2 {
		return false
	}

	// Basic validation that it looks like YYYY-MM-DD
	dateParts := strings.Split(lastPart, "-")
	if len(dateParts) != 3 || len(dateParts[0]) != 4 || len(dateParts[1]) != 2 || len(dateParts[2]) != 2 {
		return false
	}

	// Check if we have a non-empty base name after removing date
	baseName := strings.Join(parts[:len(parts)-1], "_")
	return baseName != ""
}

// parseDailyMetric extracts base name, session type and date from a daily metric key
// Returns baseName, sessionType, date - sessionType will be empty if not present
func (m *Manager) parseDailyMetric(key string) (baseName, sessionType, date string) {
	if !m.isDailyMetric(key) {
		return "", "", ""
	}

	parts := strings.Split(key, "_")
	date = parts[len(parts)-1]

	// Check if the second-to-last part is a session type
	if len(parts) >= 3 {
		potentialSessionType := parts[len(parts)-2]
		if potentialSessionType == "sse" || potentialSessionType == "mcp" || potentialSessionType == "stdio" || potentialSessionType == "unknown" {
			sessionType = potentialSessionType
			baseName = strings.Join(parts[:len(parts)-2], "_")
			return baseName, sessionType, date
		}
	}

	// No session type found, return base name without session type
	baseName = strings.Join(parts[:len(parts)-1], "_")
	return baseName, "", date
}

// formatMetric formats a single metric in Prometheus format
func (m *Manager) formatMetric(buf *bytes.Buffer, name string, labels map[string]string, value float64) {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["service"] = m.serviceName

	var labelPairs []string
	for k, v := range labels {
		labelPairs = append(labelPairs, fmt.Sprintf(`%s="%s"`, k, v))
	}
	sort.Strings(labelPairs)

	fmt.Fprintf(buf, "%s{%s} %g\n", name, strings.Join(labelPairs, ","), value)
}

// WritePrometheus writes all metrics in Prometheus format
func (m *Manager) WritePrometheus(buf *bytes.Buffer) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")

	// Write counter metrics - separate daily and total counters
	m.counters.Range(func(key, val interface{}) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		value := atomic.LoadInt64(val.(*int64))

		// Check if this is a daily metric (has date suffix)
		if m.isDailyMetric(name) {
			baseName, sessionType, date := m.parseDailyMetric(name)
			if baseName != "" && date != "" {
				labels := map[string]string{"date": date}
				if sessionType != "" {
					labels["session_type"] = sessionType
				}
				m.formatMetric(buf, fmt.Sprintf("%s_total", baseName), labels, float64(value))
			}
		} else {
			// Regular total counter without date label
			m.formatMetric(buf, fmt.Sprintf("%s_total", name), nil, float64(value))
		}
		return true
	})

	// Write current daily user count
	todayCount := m.GetDailyUserCount(today)
	m.formatMetric(buf, "daily_unique_users_total", map[string]string{"date": today}, float64(todayCount))

	// Write historical daily user counts
	for i := 1; i <= m.historicalDays; i++ {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		count := m.GetDailyUserCount(date)
		if count > 0 {
			m.formatMetric(buf, "daily_unique_users_total", map[string]string{"date": date}, float64(count))
		}
	}
}

// HTTPHandler returns an HTTP handler for the metrics endpoint
func (m *Manager) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		buf := new(bytes.Buffer)
		m.WritePrometheus(buf)

		w.Header().Set("Content-Type", PrometheusContentType)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(buf.Bytes()); err != nil {
			// Log error but don't panic
			return
		}
	}
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
