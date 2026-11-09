package mcp

import (
	"context"
	"encoding/json"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/oauth"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/internal/audit/audittest"
)

func TestAuditSavedApprovalAndSubmissionAreSeparateAndSingleUse(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	var logs audittest.Recorder
	f.manager.Logger = logs.Logger()
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	args := browserOrderArgs()
	args["tag"] = "private-free-text"
	id := browserApprovalID(t, callBrowserOrder(t, cs, args))
	page := browserPage(t, f.b, id, identity)
	csrf := pageCSRF(t, page)
	form := url.Values{"csrf": {csrf}, "decision": {"save"}}
	postBrowserDecision(f.b, id, identity, form, "")
	postBrowserDecision(f.b, id, identity, form, "")
	require.Len(t, logs.Events("order_approval_saved"), 1)
	require.Empty(t, logs.Events("order_submission_started"))
	result := callBrowserOrder(t, cs, args)
	require.False(t, result.IsError)
	starts := logs.Events("order_submission_started")
	ends := logs.Events("order_submission_result")
	require.Len(t, starts, 1)
	require.Len(t, ends, 1)
	require.Equal(t, starts[0]["submission_id"], ends[0]["submission_id"])
	require.Equal(t, "accepted", ends[0]["outcome"])
	require.Equal(t, audit.Ref(id), starts[0]["approval_ref"])
	require.Equal(t, audit.Ref(id), ends[0]["approval_ref"])
	require.Equal(t, "INFY", starts[0]["order_terms"].(map[string]any)["parameters"].(map[string]any)["tradingsymbol"])
	require.Equal(t, "order-1", ends[0]["order_id"])
	require.Equal(t, logs.Events("order_approval_saved")[0]["order_snapshot_sha256"], starts[0]["order_snapshot_sha256"])
	callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id})
	require.Len(t, logs.Events("order_submission_started"), 1)
	encoded, err := json.Marshal(logs.Records())
	require.NoError(t, err)
	for _, secret := range []string{id, csrf, "private-free-text", "fresh-access", "test-access", "test-secret", "approval-test"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.Equal(t, audit.Ref(id), logs.Events("order_approval_saved")[0]["approval_ref"])
}

func TestAuditNativeDecisionMatchesSubmission(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "accept"}[accept], func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			var logs audittest.Recorder
			f.manager.Logger = logs.Logger()
			cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25", &sdk.ClientOptions{
				Capabilities: &sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{Form: &sdk.FormElicitationCapabilities{}}},
				ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
					if accept {
						return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
					}
					return &sdk.ElicitResult{Action: "decline"}, nil
				},
			})
			result := callBrowserOrder(t, cs, browserOrderArgs())
			require.Equal(t, !accept, result.IsError)
			events := logs.Events("order_approval_resolved")
			require.Len(t, events, 1)
			require.Equal(t, "native", events[0]["approval_method"])
			if accept {
				require.Equal(t, "approved", events[0]["outcome"])
				require.Len(t, logs.Events("order_submission_started"), 1)
				require.Equal(t, events[0]["request_id"], logs.Events("order_submission_started")[0]["request_id"])
			} else {
				require.Equal(t, "declined", events[0]["outcome"])
				require.Empty(t, logs.Events("order_submission_started"))
			}
		})
	}
}

func TestAuditPermissionGrantAndRevocation(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	var logs audittest.Recorder
	f.manager.Logger = logs.Logger()
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}
	postBrowserDecision(f.b, id, identity, form, "")
	postBrowserDecision(f.b, id, identity, form, "")
	grants := logs.Events("order_permission_granted")
	require.Len(t, grants, 1)
	require.Equal(t, "all_orders", grants[0]["scope"])
	manage := browserPage(t, f.b, "manage/approval-test", identity)
	postBrowserDecision(f.b, "manage/approval-test", identity, url.Values{"csrf": {pageCSRF(t, manage)}, "decision": {"revoke"}}, "")
	require.Len(t, logs.Events("order_permission_revoked"), 1)
	require.Len(t, logs.Events("order_submission_result"), 1)
}

func TestAuditSubmissionErrorDoesNotClaimAcceptance(t *testing.T) {
	var logs audittest.Recorder
	_, err := auditSubmission(logs.Logger(), audit.Actor{UserID: "AB1234"}, "orders_place", "browser", map[string]any{"quantity": "1"}, "request", "approval", func() (*sdk.CallToolResult, error) { return NewToolResultError("secret-upstream-error"), nil })
	require.NoError(t, err)
	records := logs.Events("order_submission_result")
	require.Len(t, records, 1)
	require.Equal(t, "unconfirmed", records[0]["outcome"])
	encoded, err := json.Marshal(logs.Records())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-upstream-error")
}

func TestAuditWrongAccountReviewCannotClaimLoginSuccess(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	var logs audittest.Recorder
	f.manager.Logger = logs.Logger()
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	req := reviewCallback(t, f.b, id, "other-account")
	handlers := oauth.NewHandlers(f.b.oauth, f.manager, f.manager.Logger)
	handlers.HandleCallback(httptest.NewRecorder(), req)
	require.Empty(t, logs.Events("order_review_login_succeeded"))
	require.Empty(t, logs.Events("user_login_success"))
	failures := logs.Events("order_review_login_failed")
	require.Len(t, failures, 1)
	require.Equal(t, "account_mismatch", failures[0]["reason"])
}

func TestAuditOtherMutationAndLogout(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	var logs audittest.Recorder
	f.manager.Logger = logs.Logger()
	handler := NewToolHandler(f.manager)
	req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: json.RawMessage(`{"mode":"delete","private":"must-not-log"}`)}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {"approval-test"}}}}
	result, err := handler.WithKiteClientContext(context.Background(), req, "gtt_delete", func(*kiteconnect.Client) (*sdk.CallToolResult, error) {
		return NewToolResultText(`{"trigger_id":123}`), nil
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, logs.Events("mutation_requested"), 1)
	require.Len(t, logs.Events("mutation_result"), 1)
	require.Equal(t, "not_applicable", logs.Events("mutation_requested")[0]["approval_guard"])
	require.Equal(t, logs.Events("mutation_requested")[0]["request_id"], logs.Events("mutation_result")[0]["request_id"])
	_, err = handleSessionLogout(handler, "approval-test")
	require.NoError(t, err)
	_, err = handleSessionLogout(handler, "approval-test")
	require.NoError(t, err)
	require.Len(t, logs.Events("session_logged_out"), 1)
	encoded, err := json.Marshal(logs.Records())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must-not-log")
}
