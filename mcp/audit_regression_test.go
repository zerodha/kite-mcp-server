package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/internal/audit/audittest"
)

func TestAuditBrowserFailureBeforeGuardHasTerminalOutcome(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	var logs audittest.Recorder
	f.manager.Logger = logs.Logger()
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	args := map[string]any{"mode": "modify", "order_id": "order-1", "order_type": "LIMIT", "price": 1600}
	id := browserApprovalID(t, callBrowserOrder(t, cs, args))
	page := browserPage(t, f.b, id, identity)
	f.blockHistory.Store(true)
	form := url.Values{"csrf": {pageCSRF(t, page)}, "decision": {"approve"}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("POST", f.b.approvalURL(id), strings.NewReader(form.Encode())).WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	f.b.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "Check the order status")
	require.Zero(t, f.calls.Load())
	require.Empty(t, logs.Events("order_submission_started"))
	require.Empty(t, logs.Events("order_submission_result"))
	events := logs.Events("browser_approval_result")
	require.Len(t, events, 1)
	require.Equal(t, "failed", events[0]["status"])
	require.Equal(t, "unavailable", events[0]["approval_outcome"])
	require.Equal(t, false, events[0]["submission_attempted"])
	require.Equal(t, "not_attempted", events[0]["submission_outcome"])
	require.Equal(t, audit.Ref(id), events[0]["approval_ref"])
	postBrowserDecision(f.b, id, identity, form, "")
	require.Len(t, logs.Events("browser_approval_result"), 1, "replaying a failed review must not duplicate its terminal record")
}

func TestAuditBrowserTerminalOutcomeMatchesSubmission(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "unconfirmed"}[fail], func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			var logs audittest.Recorder
			f.manager.Logger = logs.Logger()
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
			form := url.Values{"csrf": {csrf}, "decision": {"approve"}}
			f.failMutations.Store(fail)
			postBrowserDecision(f.b, id, identity, form, "")
			postBrowserDecision(f.b, id, identity, form, "")
			events := logs.Events("browser_approval_result")
			require.Len(t, events, 1)
			require.Equal(t, true, events[0]["submission_attempted"])
			require.Equal(t, map[bool]string{false: "accepted", true: "unconfirmed"}[fail], events[0]["submission_outcome"])
			require.Equal(t, "approved", events[0]["approval_outcome"])
			require.Len(t, logs.Events("order_submission_started"), 1)
			require.EqualValues(t, 1, f.calls.Load())
		})
	}
}

func TestAuditMutationTargetsAndResults(t *testing.T) {
	for _, tc := range []struct {
		operation, arguments, response string
		requestTargets, resultTargets  map[string]any
		failed                         bool
	}{
		{"gtt_place", `{"mode":"place","trigger_id":999,"name":"private-name"}`, `{"trigger_id":321,"access_token":"private-access"}`, map[string]any{}, map[string]any{"trigger_id": float64(321)}, false},
		{"gtt_modify", `{"mode":"modify","trigger_id":123}`, `{"trigger_id":123}`, map[string]any{"trigger_id": float64(123)}, map[string]any{"trigger_id": float64(123)}, false},
		{"gtt_delete", `{"mode":"delete","trigger_id":123}`, `{"trigger_id":123}`, map[string]any{"trigger_id": float64(123)}, map[string]any{"trigger_id": float64(123)}, false},
		{"alerts_create", `{"mode":"create","uuids":["ignored-id"],"name":"private-name","condition":"private-condition"}`, `{"alert":{"uuid":"alert-new","name":"private-name","access_token":"private-access"}}`, map[string]any{}, map[string]any{"uuid": "alert-new"}, false},
		{"alerts_modify", `{"mode":"modify","uuids":["alert-old"]}`, `{"alert":{"uuid":"alert-old"}}`, map[string]any{"uuids": []any{"alert-old"}}, map[string]any{"uuid": "alert-old"}, false},
		{"alerts_delete", `{"mode":"delete","uuids":["alert-a","alert-b"]}`, `{"success":true,"deleted_uuids":["alert-a","alert-b"],"deleted_count":2}`, map[string]any{"uuids": []any{"alert-a", "alert-b"}}, map[string]any{"deleted_uuids": []any{"alert-a", "alert-b"}}, false},
		{"alerts_delete", `{"mode":"delete","uuids":["alert-a","alert-b"]}`, `{"deleted_uuids":["alert-a"],"failed_uuids":["alert-b"],"error":"private-error"}`, map[string]any{"uuids": []any{"alert-a", "alert-b"}}, map[string]any{"deleted_uuids": []any{"alert-a"}, "failed_uuids": []any{"alert-b"}}, true},
	} {
		t.Run(tc.operation+tc.response, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			var logs audittest.Recorder
			f.manager.Logger = logs.Logger()
			req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: json.RawMessage(tc.arguments)}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {"approval-test"}}}}
			_, err := NewToolHandler(f.manager).WithKiteClientContext(context.Background(), req, tc.operation, func(*kiteconnect.Client) (*sdk.CallToolResult, error) {
				result := NewToolResultText(tc.response)
				result.IsError = tc.failed
				return result, nil
			})
			require.NoError(t, err)
			starts := logs.Events("mutation_requested")
			ends := logs.Events("mutation_result")
			require.Len(t, starts, 1)
			require.Len(t, ends, 1)
			require.Equal(t, tc.requestTargets, starts[0]["request_targets"])
			require.Equal(t, tc.requestTargets, ends[0]["request_targets"])
			require.Equal(t, tc.resultTargets, ends[0]["result_targets"])
			require.Equal(t, starts[0]["request_id"], ends[0]["request_id"])
			require.Equal(t, map[bool]string{false: "tool_success", true: "tool_error"}[tc.failed], ends[0]["outcome"])
			encoded, err := json.Marshal(logs.Records())
			require.NoError(t, err)
			for _, private := range []string{"private-name", "private-condition", "private-access", "private-error"} {
				require.NotContains(t, string(encoded), private)
			}
		})
	}
}

func TestAuditMutationTargetsRejectInvalidIdentifiers(t *testing.T) {
	require.Empty(t, auditMutationTargets("gtt_delete", map[string]any{"trigger_id": 123.5}))
	require.Empty(t, auditMutationTargets("gtt_delete", map[string]any{"trigger_id": -1}))
	require.Empty(t, auditMutationTargets("alerts_delete", map[string]any{"uuids": []any{"https://private.example/token", "private\nname", 12}}))
	require.Empty(t, auditMutationResultTargets("alerts_create", map[string]any{"alert": map[string]any{"uuid": "bearer.secret/value", "name": "private-name"}}))
}
