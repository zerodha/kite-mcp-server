package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

const tradeApprovalTimeout = 5 * time.Minute

// withTradeApproval validates native approval, a browser-confirmed snapshot, or
// an explicitly remembered account/client permission. Tool arguments cannot
// grant approval. details must contain normalized execution parameters.
func withTradeApproval(ctx context.Context, request *mcp.CallToolRequest, operation string, details any, execute func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	trace := &approvalTrace{Method: "none", Outcome: "unavailable"}
	ctx = context.WithValue(ctx, approvalTraceKey{}, trace)
	trackedExecute := func() (*mcp.CallToolResult, error) {
		trace.Outcome = "approved"
		return execute()
	}
	result, err := runTradeApproval(ctx, request, operation, details, trackedExecute)
	result = approvalResultWithTrace(result, *trace)
	observeApproval(metricsFromContext(ctx), operation, result, *trace)
	return result, err
}

func runTradeApproval(ctx context.Context, request *mcp.CallToolRequest, operation string, details any, execute func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	if _, ok := ctx.Value(approvedBrowserOrderKey{}).(approvedBrowserOrder); ok {
		traceMethod(ctx, "browser", "unavailable", "")
	}
	if result, err, handled := executeBrowserApproved(ctx, request, operation, details, execute); handled {
		return result, err
	}
	if request == nil || request.Session == nil {
		return approvalFailure("unsupported_client", "Order approval is unavailable in this MCP session; no transaction was executed. "+approvalClientGuidance()), nil
	}
	// A continuation must still validate its original approval, even if the
	// client omits capabilities on the retry. Never turn a declined or malformed
	// approval response into an unconfirmed execution.
	if request.Params != nil && (request.Params.RequestState != "" || len(request.Params.InputResponses) != 0) {
		traceMethod(ctx, "native", "pending", "")
		return approvalFromContext(ctx, request, operation, details, execute)
	}
	if ctx.Err() != nil {
		return approvalFailure("cancelled", "Tool call was cancelled; no transaction was executed."), nil
	}
	if SafeAssertString(GetArguments(request)["approval_method"], "auto") == "browser" {
		if browser := browserApprovalsFrom(ctx); browser != nil {
			traceMethod(ctx, "browser", "required", "")
			return browser.request(ctx, request, operation, details)
		}
		traceMethod(ctx, "none", "unavailable", "browser_unavailable")
		return approvalFailure("unsupported_client", "Browser approval is unavailable on this connection; no transaction was executed."), nil
	}
	if browser := browserApprovalsFrom(ctx); browser != nil {
		if result, err, handled := browser.executeSaved(ctx, request, operation, details, execute); handled {
			return result, err
		}
	}
	if browser := browserApprovalsFrom(ctx); browser != nil && browser.trusted(ctx, request) {
		traceMethod(ctx, "remembered_permission", "approved", "")
		return execute()
	}
	capabilities := requestCapabilities(request)
	if !supportsFormElicitation(capabilities) {
		if browser := browserApprovalsFrom(ctx); browser != nil {
			traceMethod(ctx, "browser", "required", "form_not_supported")
			return browser.request(ctx, request, operation, details)
		}
		traceMethod(ctx, "none", "unavailable", "form_not_supported")
		return approvalFailure("unsupported_client", "This client does not support the order approval form; no transaction was executed. "+approvalClientGuidance()), nil
	}
	// A generic legacy elicitation capability does not identify a usable form
	// surface. Prefer browser review when the server can provide it; preserve
	// legacy form compatibility for transports without a browser approval broker.
	if capabilities.Elicitation.Form == nil {
		if browser := browserApprovalsFrom(ctx); browser != nil {
			traceMethod(ctx, "browser", "required", "form_support_unspecified")
			return browser.request(ctx, request, operation, details)
		}
	}
	if request.ProtocolVersion() >= "2026-07-28" {
		traceMethod(ctx, "native", "pending", "")
		return approvalFromContext(ctx, request, operation, details, execute)
	}
	traceMethod(ctx, "native", "pending", "")
	unavailable := false
	fallbackReason := "malformed_native_response"
	result, err := elicitTradeApproval(ctx, tradeApprovalTimeout, func(formCtx context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error) {
		response, err := request.Session.Elicit(formCtx, params)
		traceNativeResponse(ctx, response, err)
		if err != nil {
			fallbackReason = "native_transport_error"
		}
		unavailable = formCtx.Err() == nil && nativeApprovalUnavailable(response, err)
		return response, err
	}, operation, details, execute)
	if unavailable && ctx.Err() == nil {
		if browser := browserApprovalsFrom(ctx); browser != nil {
			traceMethod(ctx, "browser", "required", fallbackReason)
			return browser.request(ctx, request, operation, details)
		}
	}
	return result, err
}

// Kept separate from transport for deterministic deadline and integrity tests.
func elicitTradeApproval(ctx context.Context, timeout time.Duration, elicit func(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error), operation string, details any, execute func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	snapshot, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return approvalFailure("invalid_details", err.Error()), nil
	}
	approvalCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := elicit(approvalCtx, tradeApprovalForm(operation, snapshot))
	if approvalCtx.Err() != nil {
		if approvalCtx.Err() == context.DeadlineExceeded {
			return approvalFailure("timeout", "Approval deadline exceeded; no transaction was executed."), nil
		}
		return approvalFailure("cancelled", "Tool call was cancelled; no transaction was executed."), nil
	}
	if err != nil {
		return approvalFailure("elicitation_failed", "The order approval form could not be completed; no transaction was executed. "+approvalClientGuidance()+" Details: "+err.Error()), nil
	}
	if failure := validateTradeApproval(result); failure != nil {
		return failure, nil
	}
	current, err := json.MarshalIndent(details, "", "  ")
	if err != nil || !bytes.Equal(snapshot, current) {
		return approvalFailure("parameters_changed", "Transaction parameters changed during approval; no transaction was executed."), nil
	}
	// Validation can take time; acceptance does not authorize execution after
	// cancellation or expiry of the approval window.
	if approvalCtx.Err() != nil {
		if approvalCtx.Err() == context.DeadlineExceeded {
			return approvalFailure("timeout", "Approval deadline exceeded; no transaction was executed."), nil
		}
		return approvalFailure("cancelled", "Tool call was cancelled; no transaction was executed."), nil
	}
	// Never retry: Kite may have executed even if its response is lost.
	return execute()
}

func approvalFailure(code, message string) *mcp.CallToolResult {
	result := NewToolResultError("Trade approval failed [" + code + "]: " + message)
	result.StructuredContent = map[string]any{"approval_error_code": code, "message": result.Content[0].(*mcp.TextContent).Text}
	return result
}

// NewServer supports both multi-round-trip and legacy nested elicitation.
func NewServer(info *mcp.Implementation) *mcp.Server {
	versions := []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}
	//nolint:staticcheck // SA1019: Logging is retained for the negotiated pre-2026 protocol versions.
	capabilities := &mcp.ServerCapabilities{Logging: &mcp.LoggingCapabilities{}}
	capabilities.AddExtension(appExtension, map[string]any{})
	capabilities.AddExtension(skillsExtension, map[string]any{})
	capabilities.Resources = &mcp.ResourceCapabilities{}
	server := mcp.NewServer(info, &mcp.ServerOptions{
		SupportedProtocolVersions: versions, Capabilities: capabilities,
		Instructions: "When open_kite_view is listed, use it to show interactive Kite views for portfolio allocation, positions, funds, order status and fills, GTT triggers, option chains, quotes, historical charts, or instrument search only when the user explicitly asks for an interactive view or a chart. Answer ordinary data questions and order updates in text. Do not open a widget after every data read or approval status check. Reuse an existing interactive view through its controls where possible. Select its matching view and supply the relevant instrument or underlying arguments. The view tool is read-only; use the existing data tools for text-only analysis and the existing transaction tools for separately authorized mutations. If open_kite_view is unavailable, return results through the ordinary data tools. Only request order mutations when the user asks for the action. Orders use a native approval form or a browser approval link unless the user explicitly allowed future orders for this account/client login session. Choose the approval method automatically from the client capabilities. Generic elicitation without explicit form support uses browser review when available. If future_orders_allowed=true, use approval_method=auto to honour that permission unless the user explicitly requests another review. When a saved exact-order approval exists and the user later asks to place it, use approval_method=auto to consume it once. For ChatGPT mobile or a client known not to display approval forms, set approval_method=browser on the first mutation call that needs a new approval. Do not ask the user to request an approval link. If a native form is unavailable, share the browser link returned by the server. An explicit decline, cancellation, or negative confirmation stops the request; do not start another approval without a new user request. When orders returns status=approval_required, no order has been submitted: share approval_url as a clickable link. The user can open it in any browser and must sign in to the matching Kite account before reviewing the order. Do not open or approve it for the user, and do not repeat the mutation. After the user reviews it, use orders mode=approval_status with approval_id to check its status and result. If approval_status returns approved_for_later, nothing was placed. Wait for a new user instruction to place that exact order. Never automatically resubmit after saving approval. Never fabricate approval, poll indefinitely, or automatically retry an uncertain mutation. Use orders mode=permissions when the user asks to view or revoke future order permission; share its browser link if permission is enabled. Revoking permission does not cancel existing orders. GTT and Alerts/ATO changes are outside the orders approval guard.",
	})
	registerTradeApproval(server)
	registerAppResource(server)
	registerSkills(server)
	// go-sdk v1.8.0 retains the requested initialize version, then uses it
	// instead of the negotiated version to allow/reject Elicit. Normalize the
	// request before the SDK publishes session state. This produces the same
	// negotiation response while ensuring its later checks use that response.
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method == "initialize" {
				if params, ok := request.GetParams().(*mcp.InitializeParams); ok && params != nil && (!slices.Contains(versions, params.ProtocolVersion) || params.ProtocolVersion >= "2026-07-28") {
					// initialize is the legacy handshake; 2026 clients use server/discover.
					params.ProtocolVersion = "2025-11-25"
				}
			}
			return next(ctx, method, request)
		}
	})
	return server
}

// Marshal the same URL fields as gokiteconnect, including its omitempty rules.
// Holding the private params pointer also makes the integrity check cover the
// values that execution will actually send, rather than a detached display copy.
type approvalOrderParams struct{ params *kiteconnect.OrderParams }

func (p approvalOrderParams) MarshalJSON() ([]byte, error) {
	values, err := query.Values(p.params)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]string, len(values))
	for key := range values {
		fields[key] = values.Get(key)
	}
	return json.Marshal(fields)
}
