package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/kc"
)

func metricValue(t *testing.T, m *metrics.Manager, name string, labels map[string]string) float64 {
	t.Helper()
	w := httptest.NewRecorder()
	m.HTTPHandler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(w.Body.String()))
	require.NoError(t, err)
	family := families[name]
	if family == nil {
		return 0
	}
	var total float64
	for _, metric := range family.Metric {
		found := map[string]string{}
		for _, l := range metric.Label {
			found[l.GetName()] = l.GetValue()
		}
		matches := true
		for k, v := range labels {
			if found[k] != v {
				matches = false
			}
		}
		if matches {
			if metric.Counter != nil {
				total += metric.Counter.GetValue()
			} else if metric.Histogram != nil {
				total += float64(metric.Histogram.GetSampleCount())
			}
		}
	}
	return total
}

func TestOperationMetricsOnWire(t *testing.T) {
	m := metrics.New(metrics.Config{})
	t.Cleanup(m.Shutdown)
	manager := approvalTestManager(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"success","data":[]}`)
	}, m)
	server := NewServer(&sdk.Implementation{Name: "metrics-test", Version: "1"})
	RegisterTools(server, manager, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if call, ok := req.(*sdk.CallToolRequest); ok {
				call.Extra = &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {"approval-test"}}}
			}
			return next(ctx, method, req)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ct, st := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, ss.Close()) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "private-client-must-not-appear", Version: "1"}, nil)
	require.NoError(t, sdk.AddSendingCustomMethod[*listSkillsParams, *listSkillsResult](client, "skills/list"))
	require.NoError(t, sdk.AddSendingCustomMethod[*getSkillParams, *getSkillResult](client, "skills/get"))
	cs, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, cs.Close()) }()
	for _, key := range []string{"/client-integration/compare-quotes", "/secret-account-and-order-id"} {
		_, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "help", Arguments: map[string]any{"key": key}})
		require.NoError(t, err)
	}
	_, err = cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: appResourceURI})
	require.NoError(t, err)
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_operations_total", map[string]string{"tool": "help", "mode": "/client-integration/compare-quotes", "outcome": "success"}))
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_operations_total", map[string]string{"tool": "help", "mode": "other", "outcome": "tool_error"}))
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_operations_total", map[string]string{"tool": "resources", "mode": "app", "outcome": "success"}))
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_operation_duration_seconds", map[string]string{"tool": "help", "mode": "other"}))
	require.EqualValues(t, 1, metricValue(t, m, "tool_errors_total", map[string]string{"tool": "help_other"}))
	_, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: viewLauncher, Arguments: map[string]any{"view": "search", "query": "INFY", "filter_on": "tradingsymbol", "limit": 5}})
	require.NoError(t, err)
	require.EqualValues(t, 1, metricValue(t, m, "tool_calls_total", map[string]string{"tool": "market_search"}))
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_operations_total", map[string]string{"tool": viewLauncher, "mode": "search"}))
	listed, err := sdk.CallCustomMethod[*listSkillsParams, *listSkillsResult](ctx, cs, "skills/list", &listSkillsParams{})
	require.NoError(t, err)
	_, err = sdk.CallCustomMethod[*getSkillParams, *getSkillResult](ctx, cs, "skills/get", &getSkillParams{URI: listed.Skills[0].URI})
	require.NoError(t, err)
	_, err = cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: listed.Skills[0].URI})
	require.NoError(t, err)
	for _, mode := range []string{"list", "get"} {
		require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_operations_total", map[string]string{"tool": "skills", "mode": mode, "outcome": "success"}))
	}
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_operations_total", map[string]string{"tool": "resources", "mode": "skill", "outcome": "success"}))
	w := httptest.NewRecorder()
	m.HTTPHandler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.NotContains(t, w.Body.String(), "secret-account-and-order-id")
	require.NotContains(t, w.Body.String(), "private-client-must-not-appear")
}

func TestMetricLabelNormalization(t *testing.T) {
	for _, tc := range []struct{ tool, mode string }{
		{"portfolio", "holdings"}, {"portfolio", "margins"}, {"market", "option_chain"}, {"market", "historical"},
		{"orders", "approval_status"}, {"orders", "permissions"}, {"session", "logout"}, {"alerts", "create"},
		{"gtt", "modify"}, {"mutual_funds", "holdings"},
	} {
		req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: tc.tool, Arguments: json.RawMessage(`{"mode":"` + tc.mode + `"}`)}}
		name, label, tracked := metricOperation("tools/call", req)
		require.True(t, tracked)
		require.Equal(t, tc.tool, name)
		require.Equal(t, tc.mode, label)
	}
	for _, name := range []string{"market", "orders_read", viewLauncher, "untrusted-tool"} {
		req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: name, Arguments: json.RawMessage(`{"mode":"private-value","view":"private-value"}`)}}
		tool, mode, tracked := metricOperation("tools/call", req)
		require.True(t, tracked)
		require.NotContains(t, tool, "untrusted")
		require.Equal(t, "other", mode)
	}
	for _, tc := range []struct {
		ctx    context.Context
		result sdk.Result
		err    error
		want   string
	}{
		{context.Background(), NewToolResultError("private error"), nil, "tool_error"},
		{context.Background(), &sdk.CallToolResult{StructuredContent: map[string]any{"meta": map[string]any{"partial": true}}}, nil, "partial"},
		{context.Background(), &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"approval": &sdk.ElicitParams{}}}, nil, "input_required"},
		{context.Background(), nil, context.DeadlineExceeded, "timeout"},
		{context.Background(), nil, context.Canceled, "cancelled"},
	} {
		require.Equal(t, tc.want, operationOutcome(tc.ctx, tc.result, tc.err))
	}
}

func TestBrowserMetricsCountTransitionsNotPollsOrReplays(t *testing.T) {
	m := metrics.New(metrics.Config{})
	t.Cleanup(m.Shutdown)
	f := newBrowserApprovalFixture(t, m)
	cs, identity := f.connect(t, "approval-test", "Private client", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	for range 3 {
		callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id})
	}
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_approval_observations_total", map[string]string{"method": "browser", "outcome": "required", "fallback_reason": "form_not_supported"}))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}
	for range 2 {
		require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, form, "").Code)
	}
	for _, event := range []string{"created", "submission_started", "submitted"} {
		require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"operation": "orders_place", "event": event}))
	}
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "permission_granted"}))
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_approval_observations_total", map[string]string{"method": "browser", "outcome": "approved", "fallback_reason": "form_not_supported"}))
	callBrowserOrder(t, cs, browserOrderArgs())
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_approval_observations_total", map[string]string{"method": "remembered_permission", "outcome": "approved"}))
	manage := browserPage(t, f.b, "manage/approval-test", identity)
	revoke := url.Values{"csrf": {pageCSRF(t, manage)}, "decision": {"revoke"}}
	for range 2 {
		require.Equal(t, http.StatusOK, postBrowserDecision(f.b, "manage/approval-test", identity, revoke, "").Code)
	}
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "permission_revoked"}))
	id = browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	f.b.mu.Lock()
	f.b.pending.Get(id).expires = time.Now().Add(-time.Second)
	f.b.mu.Unlock()
	for range 2 {
		callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id})
	}
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "expired"}))
	require.EqualValues(t, 2, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "login_verified"}))
	w := httptest.NewRecorder()
	m.HTTPHandler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, secret := range []string{id, "Private client", "INFY", "approval-test"} {
		require.NotContains(t, w.Body.String(), secret)
	}
}

func TestBrowserMetricsFailedAndDeclinedReviews(t *testing.T) {
	for _, scenario := range []string{"failed", "declined"} {
		t.Run(scenario, func(t *testing.T) {
			m := metrics.New(metrics.Config{})
			t.Cleanup(m.Shutdown)
			f := newBrowserApprovalFixture(t, m)
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
			decision := "decline"
			if scenario == "failed" {
				decision = "approve"
				f.failMutations.Store(true)
			}
			form := url.Values{"csrf": {csrf}, "decision": {decision}, "remember": {"yes"}}
			for range 2 {
				require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, form, "").Code)
			}
			require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": scenario}))
			require.Zero(t, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "submitted"}))
			require.Zero(t, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "permission_granted"}))
		})
	}
}

func TestApprovalMetricsNativeAndInvalidCategories(t *testing.T) {
	m := metrics.New(metrics.Config{})
	t.Cleanup(m.Shutdown)
	observeApproval(m, "orders_place", &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"trade_approval": &sdk.ElicitParams{}}}, approvalTrace{Method: "native", Outcome: "pending"})
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_approval_observations_total", map[string]string{"method": "native", "outcome": "pending", "native_attempted": "true", "native_completed": "false"}))
	result := approvalResultWithTrace(approvalFailure("timeout", "private text"), approvalTrace{Method: "native", Outcome: "pending"})
	observeApproval(m, "orders_place", result, approvalTrace{})
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_approval_observations_total", map[string]string{"method": "native", "outcome": "expired"}))
	m.ObserveApproval("private-operation", "private-method", "private-outcome", "private-fallback", false, false)
	m.RecordBrowserApprovalEvent("private-operation", "private-event")
	w := httptest.NewRecorder()
	m.HTTPHandler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.NotContains(t, w.Body.String(), "private-")
}

func TestSuccessfulResponsesAreNotReclassifiedByLateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result := NewToolResultText(`{"order_id":"accepted"}`)
	cancel()
	require.Equal(t, "success", operationOutcome(ctx, result, nil))
	require.Equal(t, "cancelled", operationOutcome(ctx, nil, ctx.Err()))
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	require.Equal(t, "success", operationOutcome(expired, &sdk.ReadResourceResult{}, nil))
	require.Equal(t, "timeout", operationOutcome(expired, nil, nil))
}

func TestOverlappingBrowserReviewsGrantPermissionOnce(t *testing.T) {
	m := metrics.New(metrics.Config{})
	t.Cleanup(m.Shutdown)
	f := newBrowserApprovalFixture(t, m)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	// Both reviews start before permission is granted and have the same generation.
	first := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	second := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	firstCSRF := pageCSRF(t, browserPage(t, f.b, first, identity))
	secondCSRF := pageCSRF(t, browserPage(t, f.b, second, identity))
	for _, review := range []struct{ id, csrf string }{{first, firstCSRF}, {second, secondCSRF}} {
		form := url.Values{"csrf": {review.csrf}, "decision": {"approve"}, "remember": {"yes"}}
		require.Equal(t, http.StatusOK, postBrowserDecision(f.b, review.id, identity, form, "").Code)
	}
	require.EqualValues(t, 2, f.calls.Load())
	require.EqualValues(t, 2, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "submitted"}))
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "permission_granted"}))
}

func TestBrowserAuthenticationFailureIsNotAnApprovedObservation(t *testing.T) {
	m := metrics.New(metrics.Config{})
	t.Cleanup(m.Shutdown)
	f := newBrowserApprovalFixture(t, m)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	require.NoError(t, f.manager.SessionManager().UpdateCredentials("approval-test", &kc.KiteCredentials{UserID: "user", AccessToken: "expired", ExpiresAt: time.Now().Add(-time.Second)}))
	form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, form, "").Code)
	require.Zero(t, f.calls.Load())
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "failed"}))
	require.EqualValues(t, 1, metricValue(t, m, "kite_mcp_approval_observations_total", map[string]string{"method": "browser", "outcome": "unavailable"}))
	require.Zero(t, metricValue(t, m, "kite_mcp_approval_observations_total", map[string]string{"outcome": "approved"}))
	require.Zero(t, metricValue(t, m, "kite_mcp_browser_approval_events_total", map[string]string{"event": "permission_granted"}))
}
