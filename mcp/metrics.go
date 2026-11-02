package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/app/metrics"
)

type operationMetricsKey struct{}

func metricsFromContext(ctx context.Context) *metrics.Manager {
	m, _ := ctx.Value(operationMetricsKey{}).(*metrics.Manager)
	return m
}

var metricModes = func() map[string]map[string]bool {
	modes := make(map[string]map[string]bool)
	for _, tool := range GetAllTools() {
		def := tool.Definition()
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		encoded, _ := json.Marshal(def.InputSchema)
		_ = json.Unmarshal(encoded, &schema)
		modes[def.Name] = make(map[string]bool)
		for _, mode := range schema.Properties["mode"].Enum {
			modes[def.Name][mode] = true
		}
	}
	return modes
}()

// Use schema enums and the embedded catalog, never caller-supplied labels.
func metricOperation(method string, req sdk.Request) (string, string, bool) {
	switch method {
	case "tools/call":
		p, ok := req.GetParams().(*sdk.CallToolParamsRaw)
		if !ok || p == nil {
			return "other", "other", true
		}
		var args map[string]any
		_ = json.Unmarshal(p.Arguments, &args)
		if modes, known := metricModes[p.Name]; known {
			if p.Name == "help" {
				key, ok := args["key"].(string)
				if !ok || key == "" {
					key = "/"
				}
				if key != "/" {
					key = strings.TrimSuffix(key, "/")
				}
				if _, known := workflowHelp.pages[key]; known {
					return "help", key, true
				}
				return "help", "other", true
			}
			mode, _ := args["mode"].(string)
			if modes[mode] {
				return p.Name, mode, true
			}
			return p.Name, "other", true
		}
		switch p.Name {
		case appLauncher:
			return appLauncher, "holdings", true
		case viewLauncher:
			view, _ := args["view"].(string)
			switch view {
			case "holdings", "positions", "funds", "orders", "gtt", "option_chain", "quote", "historical", "search":
				return viewLauncher, view, true
			}
			return viewLauncher, "other", true
		case "orders_read", "gtt_read":
			mode, _ := args["mode"].(string)
			switch mode {
			case "list", "history", "trades", "all_trades":
				return p.Name, mode, true
			}
			return p.Name, "other", true
		}
		return "other", "other", true
	case "skills/list":
		return "skills", "list", true
	case "skills/get":
		return "skills", "get", true
	case "resources/read":
		if p, ok := req.GetParams().(*sdk.ReadResourceParams); ok && p != nil {
			if p.URI == appResourceURI {
				return "resources", "app", true
			}
			if _, ok := workflowHelp.files[p.URI]; ok {
				return "resources", "skill", true
			}
		}
		return "resources", "other", true
	}
	return "", "", false
}

func operationOutcome(ctx context.Context, result sdk.Result, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case err != nil:
		return "error"
	}
	if r, ok := result.(*sdk.CallToolResult); ok {
		if r == nil {
			return "error"
		}
		if r.IsError {
			return "tool_error"
		}
		if len(r.InputRequests) != 0 {
			return "input_required"
		}
		if data, ok := r.StructuredContent.(map[string]any); ok {
			if meta, ok := data["meta"].(map[string]any); ok && meta["partial"] == true {
				return "partial"
			}
		}
	}
	// A completed response is authoritative, particularly after an irreversible
	// submission. Cancellation can race with recording that successful response.
	if result != nil {
		return "success"
	}
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return "timeout"
	case context.Canceled:
		return "cancelled"
	default:
		return "error"
	}
}

func registerOperationMetrics(server *sdk.Server, m *metrics.Manager) {
	if m == nil {
		return
	}
	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (result sdk.Result, err error) {
			ctx = context.WithValue(ctx, operationMetricsKey{}, m)
			tool, mode, tracked := metricOperation(method, req)
			if !tracked {
				return next(ctx, method, req)
			}
			start := time.Now()
			completed := false
			defer func() {
				outcome := operationOutcome(ctx, result, err)
				if !completed {
					outcome = "error"
				}
				m.ObserveOperation(tool, mode, outcome, time.Since(start))
				if method == "tools/call" {
					legacyTool := tool + "_" + mode
					// Search previously counted inside the shared handler, including view launches.
					if tool == viewLauncher && mode == "search" {
						legacyTool = "market_search"
					}
					m.IncrementDailyWithLabels("tool_calls", map[string]string{"tool": legacyTool})
					if outcome == "error" || outcome == "tool_error" || outcome == "timeout" || outcome == "cancelled" {
						m.IncrementDailyWithLabels("tool_errors", map[string]string{"tool": legacyTool, "error_type": outcome})
					}
				}
			}()
			result, err = next(ctx, method, req)
			completed = true
			return result, err
		}
	})
}

func observeApproval(m *metrics.Manager, operation string, result *sdk.CallToolResult, trace approvalTrace) {
	if m == nil {
		return
	}
	if result != nil && len(result.InputRequests) != 0 {
		trace.NativeAttempted = true
	} else if result != nil && result.StructuredContent != nil {
		encoded, _ := json.Marshal(result.StructuredContent)
		_ = json.Unmarshal(encoded, &trace)
	}
	m.ObserveApproval(operation, trace.Method, trace.Outcome, trace.Fallback, trace.NativeAttempted, trace.NativeCompleted)
}
