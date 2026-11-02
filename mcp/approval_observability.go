package mcp

import (
	"context"
	"encoding/json"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type approvalTraceKey struct{}

type approvalTrace struct {
	Method          string `json:"approval_method_used"`
	Outcome         string `json:"approval_outcome"`
	Fallback        string `json:"fallback_reason"`
	NativeAttempted bool   `json:"native_elicitation_attempted"`
	NativeCompleted bool   `json:"native_elicitation_completed"`
}

func traceFrom(ctx context.Context) *approvalTrace {
	trace, _ := ctx.Value(approvalTraceKey{}).(*approvalTrace)
	return trace
}

func traceMethod(ctx context.Context, method, outcome, fallback string) {
	if trace := traceFrom(ctx); trace != nil {
		trace.Method, trace.Outcome, trace.Fallback = method, outcome, fallback
	}
}

func traceNativeResponse(ctx context.Context, response *sdk.ElicitResult, err error) {
	trace := traceFrom(ctx)
	if trace == nil {
		return
	}
	trace.NativeAttempted = true
	trace.Outcome = "unavailable"
	if err != nil || response == nil {
		return
	}
	switch response.Action {
	case "decline":
		trace.NativeCompleted, trace.Outcome = true, "declined"
	case "cancel":
		trace.NativeCompleted, trace.Outcome = true, "cancelled"
	case "accept":
		if confirm, ok := response.Content["confirm"].(bool); ok {
			trace.NativeCompleted = true
			trace.Outcome = "declined"
			if confirm {
				trace.Outcome = "accepted"
			}
		}
	}
}

func (trace approvalTrace) add(data map[string]any) {
	data["approval_method_used"] = trace.Method
	data["approval_outcome"] = trace.Outcome
	data["fallback_reason"] = trace.Fallback
	data["native_elicitation_attempted"] = trace.NativeAttempted
	data["native_elicitation_completed"] = trace.NativeCompleted
}

func approvalResultWithTrace(result *sdk.CallToolResult, trace approvalTrace) *sdk.CallToolResult {
	if result == nil {
		return nil
	}
	// MRTR input requests are intermediate protocol responses, not tool results.
	// The final continuation carries the native approval observation.
	if len(result.InputRequests) != 0 {
		return result
	}
	data := map[string]any{}
	if result.StructuredContent != nil {
		encoded, err := json.Marshal(result.StructuredContent)
		if err == nil {
			_ = json.Unmarshal(encoded, &data)
		}
	}
	if len(data) == 0 && len(result.Content) == 1 {
		if text, ok := result.Content[0].(*sdk.TextContent); ok {
			if json.Unmarshal([]byte(text.Text), &data) != nil || data == nil {
				data = map[string]any{"message": text.Text}
			}
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	if code, ok := data["approval_error_code"].(string); ok {
		switch code {
		case "timeout":
			trace.Outcome = "expired"
		case "cancelled":
			trace.Outcome = "cancelled"
		case "not_accepted", "not_confirmed":
			if trace.Outcome != "declined" && trace.Outcome != "cancelled" {
				trace.Outcome = "unavailable"
			}
		case "parameters_changed", "invalid_state", "invalid_details":
			trace.Outcome = "invalid"
		default:
			trace.Outcome = "unavailable"
		}
	}
	trace.add(data)
	copy := *result
	encoded, _ := json.Marshal(data)
	copy.StructuredContent = data
	copy.Content = []sdk.Content{&sdk.TextContent{Text: string(encoded)}}
	return &copy
}
