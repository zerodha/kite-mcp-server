package metrics

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestInstrumentSearchMetrics(t *testing.T) {
	m := New(Config{ServiceName: "test-service"})
	m.IncrementDailyWithLabels("instruments_search_mode", map[string]string{"mode": "get_by_id"})
	m.IncrementDailyWithLabels("instruments_search_verbosity", map[string]string{"verbosity": "compact"})
	m.IncrementDailyWithLabelsBy("instruments_search_results", map[string]string{
		"mode":      "get_by_id",
		"verbosity": "compact",
		"count":     "3",
	}, 3)

	w := httptest.NewRecorder()
	m.HTTPHandler()(w, httptest.NewRequest("GET", "/metrics", nil))

	today := time.Now().UTC().Format("2006-01-02")
	for _, expected := range []string{
		fmt.Sprintf(`instruments_search_mode_total{date="%s",mode="get_by_id",service="test-service"} 1`, today),
		fmt.Sprintf(`instruments_search_verbosity_total{date="%s",service="test-service",verbosity="compact"} 1`, today),
		fmt.Sprintf(`instruments_search_results_total{count="3",date="%s",mode="get_by_id",service="test-service",verbosity="compact"} 3`, today),
	} {
		if !strings.Contains(w.Body.String(), expected) {
			t.Errorf("expected metric %q in:\n%s", expected, w.Body.String())
		}
	}
}

func TestTrackDailyUserExportsLatestCountUnderConcurrency(t *testing.T) {
	m := New(Config{ServiceName: "test-service"})
	const users = 500

	var wg sync.WaitGroup
	wg.Add(users)
	for i := 0; i < users; i++ {
		go func(i int) {
			defer wg.Done()
			m.TrackDailyUser(fmt.Sprintf("user-%d", i))
		}(i)
	}
	wg.Wait()

	if got := m.GetTodayUserCount(); got != users {
		t.Fatalf("unique user count = %d, want %d", got, users)
	}
	w := httptest.NewRecorder()
	m.HTTPHandler()(w, httptest.NewRequest("GET", "/metrics", nil))
	today := time.Now().UTC().Format("2006-01-02")
	expected := fmt.Sprintf(`daily_unique_users_total{date="%s",service="test-service"} %d`, today, users)
	if !strings.Contains(w.Body.String(), expected) {
		t.Errorf("expected metric %q in:\n%s", expected, w.Body.String())
	}
}
