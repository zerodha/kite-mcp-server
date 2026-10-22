package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type approvalContextKey struct{}

const (
	maxPendingTradeApprovals           = 1024
	maxPendingTradeApprovalsPerSession = 8
)

type pendingTradeApproval struct {
	owner, operation string
	snapshot         []byte
	expires          time.Time
}

type tradeApprovals struct {
	sync.Mutex
	pending map[string]pendingTradeApproval
}

func approvalOwner(req *mcp.CallToolRequest) string {
	if id := GetSessionID(req); id != "" {
		return id
	}
	// In-memory and stdio transports have no HTTP session ID.
	return fmt.Sprintf("transport:%p", req.Session)
}

// New-protocol capabilities must be supplied on this request; never inherit
// a discover request's capabilities when the current request omits them.
func requestCapabilities(req mcp.Request) *mcp.ClientCapabilities {
	provider, ok := req.(interface {
		ClientCapabilities() *mcp.ClientCapabilities
		ProtocolVersion() string
	})
	if !ok {
		return nil
	}
	if provider.ProtocolVersion() >= "2026-07-28" {
		if req.GetParams() == nil {
			return nil
		}
		if caps, present := req.GetParams().GetMeta()[mcp.MetaKeyClientCapabilities]; !present || caps == nil {
			return nil
		}
	}
	return provider.ClientCapabilities()
}

func supportsFormElicitation(caps *mcp.ClientCapabilities) bool {
	return caps != nil && caps.Elicitation != nil && (caps.Elicitation.Form != nil || caps.Elicitation.URL == nil)
}

// State is scoped to this server and bound to the authenticated session, exact
// normalized transaction, and deadline. Consuming state is atomic and single use.
func approvalFromContext(ctx context.Context, req *mcp.CallToolRequest, operation string, details any, execute func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	store, ok := ctx.Value(approvalContextKey{}).(*tradeApprovals)
	if !ok || req.Params == nil {
		return approvalFailure("invalid_state", "Approval session unavailable; no transaction was executed."), nil
	}
	snapshot, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return approvalFailure("invalid_details", err.Error()), nil
	}
	store.Lock()
	locked := true
	defer func() {
		if locked {
			store.Unlock()
		}
	}()
	now := time.Now()
	if token := req.Params.RequestState; token != "" {
		pending, found := store.pending[token]
		if !found || pending.owner != approvalOwner(req) {
			return approvalFailure("invalid_state", "Unknown or consumed approval; no transaction was executed."), nil
		}
		delete(store.pending, token)
		if now.After(pending.expires) {
			return approvalFailure("timeout", "Approval deadline exceeded; no transaction was executed."), nil
		}
		if pending.operation != operation || !bytes.Equal(pending.snapshot, snapshot) {
			return approvalFailure("parameters_changed", "Transaction parameters changed during approval; no transaction was executed."), nil
		}
		response, ok := req.Params.InputResponses["trade_approval"].(*mcp.ElicitResult)
		if !ok || response == nil || response.Action != "accept" {
			return approvalFailure("not_accepted", "Explicit approval required; no transaction was executed."), nil
		}
		confirmed, ok := response.Content["confirm"].(bool)
		if !ok || !confirmed {
			return approvalFailure("not_confirmed", "Explicit affirmative confirmation is required."), nil
		}
		if ctx.Err() != nil {
			return approvalFailure("cancelled", "Tool call was cancelled; no transaction was executed."), nil
		}
		// The token is consumed before any API attempt, including a failed one.
		locked = false
		store.Unlock()
		return execute()
	}
	if len(req.Params.InputResponses) != 0 {
		return approvalFailure("invalid_state", "Approval state is required; no transaction was executed."), nil
	}
	owner := approvalOwner(req)
	ownerPending := 0
	for token, pending := range store.pending {
		if now.After(pending.expires) {
			delete(store.pending, token)
			continue
		}
		if pending.owner == owner {
			ownerPending++
		}
	}
	if ownerPending >= maxPendingTradeApprovalsPerSession {
		return approvalFailure("busy", "Too many pending approvals for this session; no transaction was executed."), nil
	}
	if len(store.pending) >= maxPendingTradeApprovals {
		return approvalFailure("busy", "Too many pending approvals; no transaction was executed."), nil
	}
	token := uuid.NewString()
	store.pending[token] = pendingTradeApproval{owner, operation, snapshot, now.Add(tradeApprovalTimeout)}
	return &mcp.CallToolResult{
		RequestState: token,
		InputRequests: mcp.InputRequestMap{"trade_approval": &mcp.ElicitParams{
			Mode:            "form",
			Message:         fmt.Sprintf("Confirm %s. Review the exact transaction below. Omitted modification fields remain unchanged; omitted placement fields use Kite defaults. MARKET/SL-M execution price is determined by the market. Approval authorizes one API attempt.\n%s", operation, snapshot),
			RequestedSchema: json.RawMessage(`{"type":"object","properties":{"confirm":{"type":"boolean","title":"I confirm this exact transaction"}},"required":["confirm"],"additionalProperties":false}`),
		}},
	}, nil
}

func registerTradeApproval(server *mcp.Server) {
	store := &tradeApprovals{pending: make(map[string]pendingTradeApproval)}
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(context.WithValue(ctx, approvalContextKey{}, store), method, req)
		}
	})
}
