package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

const tradeApprovalTimeout = 2 * time.Minute

// withTradeApproval is deliberately local to a single tool invocation. There is no
// reusable approval token, tool argument override, or automatic execution retry.
// details must contain the normalized parameters captured by execute, not raw
// assistant arguments. The client may confirm them, but cannot edit them.
func withTradeApproval(ctx context.Context, request *mcp.CallToolRequest, operation string, details any, execute func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	if request == nil || request.Session == nil {
		return approvalFailure("unsupported_client", "Use a trusted MCP client supporting form elicitation; no transaction was executed."), nil
	}
	caps := request.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil || (caps.Elicitation.Form == nil && caps.Elicitation.URL != nil) {
		return approvalFailure("unsupported_client", "Client did not negotiate form elicitation. Reconnect using a trusted elicitation-capable client; no transaction was executed."), nil
	}
	return elicitTradeApproval(ctx, tradeApprovalTimeout, request.Session.Elicit, operation, details, execute)
}

// Kept separate from transport for deterministic deadline and integrity tests.
func elicitTradeApproval(ctx context.Context, timeout time.Duration, elicit func(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error), operation string, details any, execute func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	snapshot, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return approvalFailure("invalid_details", err.Error()), nil
	}
	approvalCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := elicit(approvalCtx, &mcp.ElicitParams{
		Mode:            "form",
		Message:         fmt.Sprintf("Confirm %s. Review all transaction details below. Omitted modification fields remain unchanged; omitted placement fields use Kite defaults. MARKET/SL-M orders have no limit price; execution price is determined by the market. This confirmation authorizes exactly one API attempt.\n%s", operation, snapshot),
		RequestedSchema: json.RawMessage(`{"type":"object","properties":{"confirm":{"type":"boolean","title":"I confirm this exact transaction","description":"Confirm execution of the transaction shown above."}},"required":["confirm"],"additionalProperties":false}`),
	})
	if approvalCtx.Err() != nil {
		if approvalCtx.Err() == context.DeadlineExceeded {
			return approvalFailure("timeout", "Approval deadline exceeded; no transaction was executed."), nil
		}
		return approvalFailure("cancelled", "Tool call was cancelled; no transaction was executed."), nil
	}
	if err != nil {
		return approvalFailure("elicitation_failed", err.Error()), nil
	}
	if result == nil {
		return approvalFailure("missing_response", "No approval response received."), nil
	}
	if result.Action != "accept" {
		return approvalFailure("not_accepted", "User action: "+result.Action+"; no transaction was executed."), nil
	}
	confirmed, ok := result.Content["confirm"].(bool)
	if !ok || !confirmed {
		return approvalFailure("not_confirmed", "Explicit affirmative confirmation is required."), nil
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
	return NewToolResultError("Trade approval failed [" + code + "]: " + message)
}

// NewServer negotiates versions supporting nested elicitation/create. Newer
// protocols replace it with a different multi-round-trip execution model.
func NewServer(info *mcp.Implementation) *mcp.Server {
	versions := []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}
	server := mcp.NewServer(info, &mcp.ServerOptions{SupportedProtocolVersions: versions})
	// go-sdk v1.8.0 retains the requested initialize version, then uses it
	// instead of the negotiated version to allow/reject Elicit. Normalize the
	// request before the SDK publishes session state. This produces the same
	// negotiation response while ensuring its later checks use that response.
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method == "initialize" {
				if params, ok := request.GetParams().(*mcp.InitializeParams); ok && params != nil && !slices.Contains(versions, params.ProtocolVersion) {
					params.ProtocolVersion = versions[0]
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
