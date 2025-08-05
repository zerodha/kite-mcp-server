package metrics

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDailyMetrics(t *testing.T) {
	m := New(Config{
		ServiceName: "test-service",
		AutoCleanup: false,
	})

	today := time.Now().UTC().Format("2006-01-02")

	// Test daily increment methods
	m.IncrementDaily("tool_calls_quotes")
	m.IncrementDaily("tool_calls_quotes")
	m.IncrementDailyBy("tool_calls_login", 3)
	m.IncrementDaily("tool_errors_quotes_api_error")

	// Test regular increment (non-daily)
	m.Increment("legacy_counter")

	// Check internal storage with date suffix
	expectedQuotesKey := fmt.Sprintf("tool_calls_quotes_%s", today)
	expectedLoginKey := fmt.Sprintf("tool_calls_login_%s", today)
	expectedErrorKey := fmt.Sprintf("tool_errors_quotes_api_error_%s", today)

	if got := m.GetCounterValue(expectedQuotesKey); got != 2 {
		t.Errorf("Expected tool_calls_quotes_%s = 2, got %d", today, got)
	}

	if got := m.GetCounterValue(expectedLoginKey); got != 3 {
		t.Errorf("Expected tool_calls_login_%s = 3, got %d", today, got)
	}

	if got := m.GetCounterValue(expectedErrorKey); got != 1 {
		t.Errorf("Expected tool_errors_quotes_api_error_%s = 1, got %d", today, got)
	}

	if got := m.GetCounterValue("legacy_counter"); got != 1 {
		t.Errorf("Expected legacy_counter = 1, got %d", got)
	}
}

func TestDailyMetricsPrometheusOutput(t *testing.T) {
	m := New(Config{
		ServiceName: "test-service",
		AutoCleanup: false,
	})

	today := time.Now().UTC().Format("2006-01-02")

	// Add some daily metrics
	m.IncrementDaily("tool_calls_quotes")
	m.IncrementDaily("tool_calls_quotes")
	m.IncrementDaily("tool_errors_quotes_api_error")

	// Add a regular metric
	m.Increment("legacy_counter")

	// Generate Prometheus output
	var buf bytes.Buffer
	m.WritePrometheus(&buf)
	output := buf.String()

	// Check that daily metrics have date labels
	expectedQuotesLine := fmt.Sprintf(`tool_calls_quotes_total{date="%s",service="test-service"} 2`, today)
	expectedErrorLine := fmt.Sprintf(`tool_errors_quotes_api_error_total{date="%s",service="test-service"} 1`, today)
	expectedLegacyLine := `legacy_counter_total{service="test-service"} 1`

	if !strings.Contains(output, expectedQuotesLine) {
		t.Errorf("Expected output to contain: %s\nGot: %s", expectedQuotesLine, output)
	}

	if !strings.Contains(output, expectedErrorLine) {
		t.Errorf("Expected output to contain: %s\nGot: %s", expectedErrorLine, output)
	}

	if !strings.Contains(output, expectedLegacyLine) {
		t.Errorf("Expected output to contain: %s\nGot: %s", expectedLegacyLine, output)
	}
}

func TestIsDailyMetric(t *testing.T) {
	m := New(Config{ServiceName: "test"})

	tests := []struct {
		key      string
		expected bool
	}{
		{"tool_calls_quotes_2025-08-05", true},
		{"tool_errors_login_session_error_2025-12-31", true},
		{"legacy_counter", false},
		{"tool_calls_quotes", false},
		{"tool_calls_quotes_20250805", false}, // Wrong date format
		{"tool_calls_quotes_2025-8-5", false}, // Wrong date format
		{"", false},
		{"_2025-08-05", false}, // Empty base name
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := m.isDailyMetric(tt.key); got != tt.expected {
				t.Errorf("isDailyMetric(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestParseDailyMetric(t *testing.T) {
	m := New(Config{ServiceName: "test"})

	tests := []struct {
		key          string
		expectedBase string
		expectedDate string
	}{
		{"tool_calls_quotes_2025-08-05", "tool_calls_quotes", "2025-08-05"},
		{"tool_errors_login_session_error_2025-12-31", "tool_errors_login_session_error", "2025-12-31"},
		{"legacy_counter", "", ""}, // Not a daily metric
		{"tool_calls_quotes_20250805", "", ""}, // Wrong date format
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			gotBase, gotDate := m.parseDailyMetric(tt.key)
			if gotBase != tt.expectedBase || gotDate != tt.expectedDate {
				t.Errorf("parseDailyMetric(%q) = (%q, %q), want (%q, %q)",
					tt.key, gotBase, gotDate, tt.expectedBase, tt.expectedDate)
			}
		})
	}
}