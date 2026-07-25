package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{"with service name", Config{ServiceName: "test-service"}, "test-service"},
		{"empty service name uses default", Config{}, DefaultServiceName},
		{"with admin path", Config{AdminSecretPath: "secret123"}, DefaultServiceName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(tt.config)
			if m.serviceName != tt.expected {
				t.Errorf("expected service name %q, got %q", tt.expected, m.serviceName)
			}
			if tt.config.AdminSecretPath != "" && m.adminSecretPath != tt.config.AdminSecretPath {
				t.Errorf("expected admin secret path %q, got %q", tt.config.AdminSecretPath, m.adminSecretPath)
			}
		})
	}
}

func TestIncrement(t *testing.T) {
	m := New(Config{ServiceName: "test"})

	// Test counters by checking they appear in metrics output
	m.Increment("test_counter")
	m.Increment("test_counter")
	m.Increment("test_counter")
	m.Increment("other_counter")

	// Check via HTTP handler
	handler := m.HTTPHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	output := w.Body.String()

	// Check for specific values
	expectedTestCounter := `test_counter{service="test"} 3`
	expectedOtherCounter := `other_counter{service="test"} 1`

	if !strings.Contains(output, expectedTestCounter) {
		t.Errorf("expected output to contain: %s, got: %s", expectedTestCounter, output)
	}

	if !strings.Contains(output, expectedOtherCounter) {
		t.Errorf("expected output to contain: %s, got: %s", expectedOtherCounter, output)
	}
}

func TestIncrementBy(t *testing.T) {
	m := New(Config{ServiceName: "test"})

	m.IncrementBy("test_counter", 5)
	m.IncrementBy("test_counter", 3)

	// Check via HTTP handler with correct value
	handler := m.HTTPHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	output := w.Body.String()

	// Check for specific value (5 + 3 = 8)
	expectedPattern := `test_counter{service="test"} 8`
	if !strings.Contains(output, expectedPattern) {
		t.Errorf("expected output to contain: %s, got: %s", expectedPattern, output)
	}
}

func TestConcurrentIncrement(t *testing.T) {
	m := New(Config{ServiceName: "test"})
	const numGoroutines = 100
	const incrementsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				m.Increment("concurrent_counter")
			}
		}()
	}

	wg.Wait()

	handler := m.HTTPHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	output := w.Body.String()

	expectedValue := numGoroutines * incrementsPerGoroutine
	expectedPattern := fmt.Sprintf(`concurrent_counter{service="test"} %d`, expectedValue)

	if !strings.Contains(output, expectedPattern) {
		t.Errorf("expected output to contain: %s, got: %s", expectedPattern, output)
	}
}

func TestTrackDailyUser(t *testing.T) {
	m := New(Config{ServiceName: "test"})

	// Test empty user ID (should be ignored)
	m.TrackDailyUser("")
	if count := m.GetTodayUserCount(); count != 0 {
		t.Errorf("expected empty user ID to be ignored, got count %d", count)
	}

	// Track same user multiple times
	m.TrackDailyUser("user1")
	m.TrackDailyUser("user1")
	m.TrackDailyUser("user1")

	if count := m.GetTodayUserCount(); count != 1 {
		t.Errorf("expected today's user count 1, got %d", count)
	}

	// Track different users
	m.TrackDailyUser("user2")
	m.TrackDailyUser("user3")

	if count := m.GetTodayUserCount(); count != 3 {
		t.Errorf("expected today's user count 3, got %d", count)
	}
}

func TestGetDailyUserCount(t *testing.T) {
	m := New(Config{ServiceName: "test"})

	// Test non-existent date
	if count := m.GetDailyUserCount("2023-01-01"); count != 0 {
		t.Errorf("expected count 0 for non-existent date, got %d", count)
	}

	// Track users for today
	m.TrackDailyUser("user1")
	m.TrackDailyUser("user2")

	today := time.Now().UTC().Format("2006-01-02")
	if count := m.GetDailyUserCount(today); count != 2 {
		t.Errorf("expected count 2 for today, got %d", count)
	}
}

func TestCleanupOldData(t *testing.T) {
	m := New(Config{ServiceName: "test", CleanupRetentionDays: 5})

	// Manually add old data
	oldDate := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	oldUserSet := &userSet{}
	oldUserSet.users.Store("user1", true)
	oldUserSet.count = 1
	m.dailyUsers.Store(oldDate, oldUserSet)

	// Add recent data
	m.TrackDailyUser("user2")

	// Verify old data exists
	if count := m.GetDailyUserCount(oldDate); count != 1 {
		t.Errorf("expected old data count 1, got %d", count)
	}

	// Cleanup old data
	if err := m.CleanupOldData(); err != nil {
		t.Errorf("cleanup failed: %v", err)
	}

	// Verify old data is removed
	if count := m.GetDailyUserCount(oldDate); count != 0 {
		t.Errorf("expected old data to be cleaned up, got count %d", count)
	}

	// Verify recent data still exists
	if count := m.GetTodayUserCount(); count != 1 {
		t.Errorf("expected recent data to remain, got count %d", count)
	}
}

func TestPrometheusMetrics(t *testing.T) {
	m := New(Config{ServiceName: "test-service", HistoricalDays: 3})

	// Add some metrics
	m.Increment("login_count")
	m.Increment("login_count")
	m.IncrementBy("error_count", 3)
	m.TrackDailyUser("user1")
	m.TrackDailyUser("user2")

	handler := m.HTTPHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	output := w.Body.String()

	// Check that user metrics are present
	expectedMetrics := []string{
		"daily_unique_users_total{",
		"service=\"test-service\"",
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(output, expected) {
			t.Errorf("expected output to contain %q, got:\n%s", expected, output)
		}
	}
}

func TestHTTPHandler(t *testing.T) {
	m := New(Config{ServiceName: "test"})

	// Add a tool metric to test
	m.IncrementDailyWithLabels("tool_calls", map[string]string{
		"tool":         "test_tool",
		"session_type": "mcp",
	})

	m.TrackDailyUser("test_user")

	handler := m.HTTPHandler()

	// Test GET request
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "tool_calls_total") {
		t.Errorf("expected body to contain tool_calls_total, got:\n%s", body)
	}

	if !strings.Contains(body, "daily_unique_users_total") {
		t.Errorf("expected body to contain daily_unique_users_total, got:\n%s", body)
	}
}

func TestAdminHTTPHandler(t *testing.T) {
	tests := []struct {
		name           string
		adminPath      string
		requestPath    string
		expectedStatus int
	}{
		{
			name:           "disabled when no admin path",
			adminPath:      "",
			requestPath:    "/admin/secret/metrics",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "enabled with correct path",
			adminPath:      "secret123",
			requestPath:    "/admin/secret123/metrics",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "wrong path returns 404",
			adminPath:      "secret123",
			requestPath:    "/admin/wrong/metrics",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "missing metrics suffix returns 404",
			adminPath:      "secret123",
			requestPath:    "/admin/secret123/",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Config{
				ServiceName:     "test",
				AdminSecretPath: tt.adminPath,
			})
			m.Increment("test_metric")
			m.TrackDailyUser("test_user")

			handler := m.AdminHTTPHandler()
			req := httptest.NewRequest("GET", tt.requestPath, nil)
			w := httptest.NewRecorder()
			handler(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedStatus == http.StatusOK {
				body := w.Body.String()
				if !strings.Contains(body, "daily_unique_users_total") {
					t.Errorf("expected body to contain metrics, got:\n%s", body)
				}
			}
		})
	}
}

func TestConcurrentDailyUserTracking(t *testing.T) {
	m := New(Config{ServiceName: "test"})
	const numGoroutines = 50
	const usersPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < usersPerGoroutine; j++ {
				userID := fmt.Sprintf("user_%d_%d", goroutineID, j)
				m.TrackDailyUser(userID)
			}
		}(i)
	}

	wg.Wait()

	expected := int64(numGoroutines * usersPerGoroutine)
	if count := m.GetTodayUserCount(); count != expected {
		t.Errorf("expected today's user count %d, got %d", expected, count)
	}
}

func TestGetNextCleanupTime(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time
		expected int // expected days from now
	}{
		{
			name:     "Monday should wait until Saturday",
			now:      time.Date(2025, 6, 9, 10, 0, 0, 0, time.UTC), // Monday
			expected: 5,
		},
		{
			name:     "Saturday before 3 AM should wait until 3 AM same day",
			now:      time.Date(2025, 6, 14, 1, 0, 0, 0, time.UTC), // Saturday 1 AM
			expected: 0,
		},
		{
			name:     "Saturday after 3 AM should wait until next Saturday",
			now:      time.Date(2025, 6, 14, 10, 0, 0, 0, time.UTC), // Saturday 10 AM
			expected: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := getNextCleanupTime(tt.now)

			// Check that it's at 3 AM
			if next.Hour() != DefaultCleanupHour {
				t.Errorf("expected hour %d, got %d", DefaultCleanupHour, next.Hour())
			}

			// Check that it's on Saturday
			if next.Weekday() != time.Saturday {
				t.Errorf("expected Saturday, got %v", next.Weekday())
			}

			// Check approximate days difference
			daysDiff := int(next.Sub(tt.now).Hours() / 24)
			if tt.expected == 0 {
				// Same day case - should be within hours
				if daysDiff > 0 {
					t.Errorf("expected same day, got %d days difference", daysDiff)
				}
			} else {
				if daysDiff < tt.expected-1 || daysDiff > tt.expected+1 {
					t.Errorf("expected approximately %d days, got %d", tt.expected, daysDiff)
				}
			}
		})
	}
}

func TestShutdown(t *testing.T) {
	m := New(Config{ServiceName: "test", AutoCleanup: true})

	// Should not panic
	m.Shutdown()

	// Should be safe to call multiple times
	m.Shutdown()
	m.Shutdown()
}

func TestAutoCleanupDisabled(t *testing.T) {
	m := New(Config{ServiceName: "test", AutoCleanup: false})

	// Should not have started cleanup routine
	// We can't easily test this without exposing internals,
	// but at least verify it doesn't panic
	m.Shutdown()
}
