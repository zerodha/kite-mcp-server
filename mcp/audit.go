package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/oauth"
)

type submissionAuditStateKey struct{}
type submissionAuditState struct{ attempted bool }

func auditActor(ctx context.Context, req *sdk.CallToolRequest) audit.Actor {
	if claims := oauth.ClaimsFromContext(ctx); claims != nil {
		return audit.Actor{UserID: claims.KiteUserID, SessionID: claims.SessionID, ClientID: claims.ClientID}
	}
	if approved, ok := ctx.Value(approvedBrowserOrderKey{}).(approvedBrowserOrder); ok {
		return audit.Actor{UserID: approved.user, SessionID: approved.session, ClientID: approved.client}
	}
	return audit.Actor{SessionID: GetSessionID(req)}
}

func auditResultData(result *sdk.CallToolResult) map[string]any {
	if result == nil {
		return nil
	}
	var data map[string]any
	if result.StructuredContent != nil {
		encoded, _ := json.Marshal(result.StructuredContent)
		_ = json.Unmarshal(encoded, &data)
	} else if len(result.Content) == 1 {
		if text, ok := result.Content[0].(*sdk.TextContent); ok {
			_ = json.Unmarshal([]byte(text.Text), &data)
		}
	}
	return data
}

// Every call of execute is one API submission attempt. It is never evidence of
// an exchange fill, even when Kite accepts the request.
func auditSubmission(logger *slog.Logger, actor audit.Actor, operation, method string, details any, requestID, approvalRef string, execute func() (*sdk.CallToolResult, error)) (*sdk.CallToolResult, error) {
	id := uuid.NewString()
	fields := []any{"operation", operation, "submission_id", id, "approval_method", method, "order_snapshot_sha256", audit.Digest(details), "order_terms", auditOrderTerms(details), "request_id", requestID}
	if approvalRef != "" {
		fields = append(fields, "approval_ref", approvalRef)
	}
	audit.Record(logger, "order_submission_started", actor, fields...)
	result, err := execute()
	outcome := "unconfirmed"
	if err == nil && result != nil && !result.IsError {
		outcome = "accepted"
	}
	fields = append(fields, "outcome", outcome)
	if orderID, ok := auditResultData(result)["order_id"].(string); ok && orderID != "" {
		fields = append(fields, "order_id", orderID)
	}
	audit.Record(logger, "order_submission_result", actor, fields...)
	return result, err
}

func (b *BrowserApprovals) auditOrder(p *browserOrder, event string, fields ...any) {
	attrs := []any{"operation", p.operation, "approval_ref", audit.Ref(p.id), "order_snapshot_sha256", audit.Digest(json.RawMessage(p.snapshot)), "order_terms", auditOrderTerms(json.RawMessage(p.snapshot))}
	attrs = append(attrs, fields...)
	audit.Record(b.manager.Logger, event, audit.Actor{UserID: p.user, SessionID: p.session, ClientID: p.client}, attrs...)
}

// GTT and Alerts/ATO do not use the orders approval guard. These events record
// authenticated tool requests and results, not an assertion of user approval.
func auditOtherMutation(manager *kc.Manager, ctx context.Context, req *sdk.CallToolRequest, operation string, execute func() (*sdk.CallToolResult, error)) (*sdk.CallToolResult, error) {
	actor := auditActor(ctx, req)
	if session, err := manager.SessionManager().Get(actor.SessionID); err == nil && session.Credentials != nil {
		actor.UserID = session.Credentials.UserID
	}
	id := uuid.NewString()
	fields := []any{"operation", operation, "request_id", id, "request_sha256", audit.Digest(GetArguments(req)), "approval_guard", "not_applicable", "request_targets", auditMutationTargets(operation, GetArguments(req))}
	audit.Record(manager.Logger, "mutation_requested", actor, fields...)
	result, err := execute()
	outcome := "tool_error"
	if err == nil && result != nil && !result.IsError {
		outcome = "tool_success"
	}
	audit.Record(manager.Logger, "mutation_result", actor, append(fields, "outcome", outcome, "result_targets", auditMutationResultTargets(operation, auditResultData(result)))...)
	return result, err
}

// Only fixed order fields may enter audit records. Exclude free-text tags,
// arbitrary nested metadata and all raw tool/HTTP payloads.
func auditOrderTerms(value any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var data map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&data) != nil {
		return nil
	}
	var filter func(map[string]any) map[string]any
	filter = func(source map[string]any) map[string]any {
		safe := map[string]any{}
		for _, key := range []string{"variety", "order_id", "exchange", "tradingsymbol", "transaction_type", "quantity", "product", "order_type", "price", "trigger_price", "validity", "validity_ttl", "disclosed_quantity", "iceberg_legs", "iceberg_quantity", "market_protection", "autoslice", "status", "filled_quantity", "pending_quantity", "cancelled_quantity", "average_price"} {
			switch v := source[key].(type) {
			case string:
				if len(v) <= 256 {
					safe[key] = v
				}
			case json.Number, bool:
				safe[key] = v
			}
		}
		for _, key := range []string{"parameters", "changes", "current_order"} {
			if nested, ok := source[key].(map[string]any); ok {
				safe[key] = filter(nested)
			}
		}
		return safe
	}
	return filter(data)
}

// Target identifiers use the same validation as their tool arguments. No names,
// conditions, request bodies or other response fields enter these records.
func auditMutationTargets(operation string, data map[string]any) map[string]any {
	targets := map[string]any{}
	switch operation {
	case "gtt_modify", "gtt_delete":
		if id, ok := auditTriggerID(data["trigger_id"]); ok {
			targets["trigger_id"] = id
		}
	case "alerts_modify", "alerts_delete":
		if ids := auditAlertIDs(data["uuids"]); len(ids) > 0 {
			targets["uuids"] = ids
		}
	}
	return targets
}

func auditMutationResultTargets(operation string, data map[string]any) map[string]any {
	targets := auditMutationTargets(operation, data)
	switch operation {
	case "gtt_place":
		if id, ok := auditTriggerID(data["trigger_id"]); ok {
			targets["trigger_id"] = id
		}
	case "alerts_create", "alerts_modify", "alerts_delete":
		if alert, ok := data["alert"].(map[string]any); ok {
			if id, ok := alert["uuid"].(string); ok && validatePathIdentifier(id, "uuid") == nil {
				targets["uuid"] = id
			}
		}
		for _, key := range []string{"deleted_uuids", "failed_uuids"} {
			if ids := auditAlertIDs(data[key]); len(ids) > 0 {
				targets[key] = ids
			}
		}
	}
	return targets
}

func auditTriggerID(value any) (int64, bool) {
	var number float64
	switch v := value.(type) {
	case int:
		number = float64(v)
	case int64:
		number = float64(v)
	case float64:
		number = v
	case json.Number:
		var err error
		number, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 1 || number > 2147483647 || number != math.Trunc(number) {
		return 0, false
	}
	return int64(number), true
}

func auditAlertIDs(value any) []string {
	var values []any
	switch v := value.(type) {
	case []any:
		values = v
	case []string:
		for _, id := range v {
			values = append(values, id)
		}
	default:
		return nil
	}
	if len(values) > 500 {
		return nil
	}
	var ids []string
	for _, value := range values {
		if id, ok := value.(string); ok && validatePathIdentifier(id, "uuid") == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
