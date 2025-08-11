package metrics

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDailyMetrics(t *testing.T) {
	m := New(Config{
		ServiceName: "test-service",
		AutoCleanup: false,
	})

	// Test daily increment methods
	m.IncrementDaily("tool_calls_quotes")
	m.IncrementDaily("tool_calls_quotes")
	m.IncrementDailyBy("tool_calls_login", 3)
	m.IncrementDaily("tool_errors_quotes_api_error")

	// Test that metrics were created by checking HTTP handler output
	handler := m.HTTPHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	output := w.Body.String()

	today := time.Now().UTC().Format("2006-01-02")

	expectedMetrics := []struct {
		name  string
		value string
	}{
		{"tool_calls_quotes", "2"},            // incremented twice
		{"tool_calls_login", "3"},             // incremented by 3
		{"tool_errors_quotes_api_error", "1"}, // incremented once
	}

	for _, metric := range expectedMetrics {
		expectedPattern := fmt.Sprintf(`%s{date="%s",service="test-service"} %s`, metric.name, today, metric.value)
		if !strings.Contains(output, expectedPattern) {
			t.Errorf("Expected output to contain: %s\nGot: %s", expectedPattern, output)
		}
	}
}

func TestDailyMetricsWithLabels(t *testing.T) {
	m := New(Config{
		ServiceName: "test-service",
		AutoCleanup: false,
	})

	m.IncrementDailyWithLabels("tool_calls", map[string]string{
		"tool":         "quotes",
		"session_type": "mcp",
	})
	m.IncrementDailyWithLabels("tool_calls", map[string]string{
		"tool":         "quotes",
		"session_type": "mcp",
	})

	m.IncrementDailyWithLabels("tool_errors", map[string]string{
		"tool":         "quotes",
		"error_type":   "api_error",
		"session_type": "mcp",
	})

	handler := m.HTTPHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	output := w.Body.String()
	today := time.Now().UTC().Format("2006-01-02")

	expectedPatterns := []struct {
		description string
		pattern     string
	}{
		{
			"tool calls with correct value and labels",
			fmt.Sprintf(`tool_calls_total{date="%s",service="test-service",session_type="mcp",tool="quotes"} 2`, today),
		},
		{
			"tool errors with correct value and labels",
			fmt.Sprintf(`tool_errors_total{date="%s",error_type="api_error",service="test-service",session_type="mcp",tool="quotes"} 1`, today),
		},
	}

	for _, expected := range expectedPatterns {
		if !strings.Contains(output, expected.pattern) {
			t.Errorf("Expected output to contain %s: %s\nFull output: %s", expected.description, expected.pattern, output)
		}
	}
}
