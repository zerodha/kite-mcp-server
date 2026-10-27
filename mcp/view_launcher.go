package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
)

const viewLauncher = "open_kite_view"

var kiteViews = []string{"holdings", "positions", "funds", "orders", "gtt", "option_chain", "quote", "historical", "search"}

func viewSource(view string) string {
	switch view {
	case "holdings", "positions", "funds":
		return "portfolio"
	case "orders":
		return "orders_read"
	case "gtt":
		return "gtt_read"
	case "option_chain", "quote", "historical", "search":
		return "market"
	default:
		return ""
	}
}
func viewExcluded(view string, excluded map[string]bool) bool {
	source := viewSource(view)
	return excluded[source] || (source == "orders_read" && excluded["orders"]) || (source == "gtt_read" && excluded["gtt"])
}

func registerViewLauncher(server *mcp.Server, manager *kc.Manager, excluded map[string]bool) {
	if excluded[viewLauncher] {
		return
	}
	available := []string{}
	for _, view := range kiteViews {
		if !viewExcluded(view, excluded) {
			available = append(available, view)
		}
	}
	if len(available) == 0 {
		return
	}
	properties := map[string]any{
		"view":             map[string]any{"type": "string", "enum": available, "description": "Kite widget to open: holdings allocation/treemap, positions P&L, funds, order book/timeline, GTT monitor, option chain, quote/depth, historical chart, or instrument search."},
		"underlying":       map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "default": "NIFTY", "description": "Option-chain underlying; defaults to NIFTY."},
		"exchange":         map[string]any{"type": "string", "description": "Option-chain exchange NFO/NSE or BFO/BSE; default NFO."},
		"expiry":           map[string]any{"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$", "description": "Option-chain expiry; omitted selects nearest valid expiry."},
		"instruments":      map[string]any{"type": "array", "minItems": 1, "maxItems": 500, "items": map[string]any{"type": "string", "minLength": 1}, "description": "Required for quote: EXCHANGE:TRADINGSYMBOL identifiers."},
		"instrument_token": map[string]any{"type": "integer", "minimum": 1, "description": "Required for historical."},
		"from_date":        map[string]any{"type": "string", "description": "Required for historical; YYYY-MM-DD or YYYY-MM-DD HH:MM:SS in IST."},
		"to_date":          map[string]any{"type": "string", "description": "Required for historical; YYYY-MM-DD or YYYY-MM-DD HH:MM:SS in IST."},
		"interval":         map[string]any{"type": "string", "enum": []string{"minute", "3minute", "5minute", "10minute", "15minute", "30minute", "60minute", "day"}, "description": "Required for historical."},
		"query":            map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "description": "Required for instrument search."},
		"filter_on":        map[string]any{"type": "string", "enum": []string{"id", "name", "tradingsymbol", "isin", "underlying"}, "description": "Search field: use name for company names, tradingsymbol for symbols, id for exchange:symbol, isin for identifiers, underlying for F&O groups."},
		"from":             map[string]any{"type": "integer", "minimum": 0, "maximum": MaxPaginationOffset, "default": 0},
		"limit":            map[string]any{"type": "integer", "minimum": 1, "maximum": 500, "default": 100},
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": []string{"view"}, "additionalProperties": false})
	tool := NewTool(viewLauncher, "Open an interactive read-only Kite widget when the user asks to see/show a portfolio treemap, holdings, position P&L chart, funds, orders or order timeline, GTT monitor, option chain, quote/depth, historical chart, or instrument finder. Choose the matching view. This displays the widget and its initial data; it never places, modifies, or cancels trades. For text-only answers or hosts without Apps support, use portfolio, market, orders, or gtt data tools instead.", schema)
	tool.Title = "Kite"
	tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	tool.Meta = mcp.Meta{"ui": map[string]any{"resourceUri": appResourceURI, "visibility": []string{"model", "app"}}, "ui/resourceUri": appResourceURI}
	server.AddTool(tool, wrapHandler(viewLauncherHandler(manager, excluded)))
}

// Build the delegated request from a strict view map, never forwarding an
// arbitrary mode or tool name supplied by a caller.
func kiteViewRequest(args map[string]interface{}, excluded map[string]bool) (string, map[string]interface{}, error) {
	view, ok := args["view"].(string)
	if !ok || !slices.Contains(kiteViews, view) {
		return "", nil, fmt.Errorf("view must be one of: %s", strings.Join(kiteViews, ", "))
	}
	if excluded[viewLauncher] || viewExcluded(view, excluded) {
		return "", nil, fmt.Errorf("the %s view is disabled by server configuration", view)
	}
	permitted := map[string]bool{"view": true, "from": true, "limit": true}
	switch view {
	case "option_chain":
		for _, key := range []string{"underlying", "exchange", "expiry"} {
			permitted[key] = true
		}
	case "quote":
		permitted["instruments"] = true
	case "historical":
		for _, key := range []string{"instrument_token", "from_date", "to_date", "interval"} {
			permitted[key] = true
		}
	case "search":
		permitted["query"] = true
		permitted["filter_on"] = true
	}
	for key := range args {
		if !permitted[key] {
			return "", nil, fmt.Errorf("parameter %q is not supported for the %s view", key, view)
		}
	}
	params, err := ParsePaginationParams(args)
	if err != nil {
		return "", nil, err
	}
	source := viewSource(view)
	next := map[string]interface{}{}
	switch view {
	case "holdings":
		next["mode"] = "holdings"
		next["type"] = "full"
	case "positions":
		next["mode"] = "positions"
	case "funds":
		next["mode"] = "margins"
	case "orders", "gtt":
		next["mode"] = "list"
	case "option_chain":
		next["mode"] = "option_chain"
		next["underlying"] = "NIFTY"
	case "quote", "historical", "search":
		next["mode"] = view
	}
	for key := range permitted {
		if key != "view" && key != "from" && key != "limit" {
			if value, exists := args[key]; exists {
				next[key] = value
			}
		}
	}
	if view != "funds" && view != "quote" && view != "historical" {
		next["from"] = params.From
		next["limit"] = params.Limit
	}
	// Runtime checks complement JSON Schema validation for direct handler calls.
	for _, key := range []string{"underlying", "exchange", "expiry", "from_date", "to_date", "interval", "query", "filter_on"} {
		if value, exists := next[key]; exists {
			if s, ok := value.(string); !ok || strings.TrimSpace(s) == "" {
				return "", nil, fmt.Errorf("%s must be a non-empty string", key)
			}
		}
	}
	if view == "quote" {
		if _, err := parseMarketInstruments(next); err != nil {
			return "", nil, err
		}
	}
	if view == "historical" {
		var token float64
		switch value := next["instrument_token"].(type) {
		case int:
			token = float64(value)
		case float64:
			token = value
		default:
			return "", nil, fmt.Errorf("instrument_token must be a positive integer")
		}
		if token <= 0 || math.IsNaN(token) || math.IsInf(token, 0) || math.Trunc(token) != token || token > 4294967295 {
			return "", nil, fmt.Errorf("instrument_token must be a positive 32-bit integer")
		}
		interval, _ := next["interval"].(string)
		if !slices.Contains([]string{"minute", "3minute", "5minute", "10minute", "15minute", "30minute", "60minute", "day"}, interval) {
			return "", nil, fmt.Errorf("historical interval is invalid")
		}
		if err := ValidateRequired(next, "instrument_token", "from_date", "to_date", "interval"); err != nil {
			return "", nil, err
		}
	}
	if view == "search" {
		if field, exists := next["filter_on"]; exists && !slices.Contains([]string{"id", "name", "tradingsymbol", "isin", "underlying"}, field.(string)) {
			return "", nil, fmt.Errorf("search filter_on is invalid")
		}
		if err := ValidateRequired(next, "query"); err != nil {
			return "", nil, err
		}
		next["search_mode"] = "search"
		next["verbosity"] = "full"
	}
	return source, next, nil
}

func viewLauncherHandler(manager *kc.Manager, excluded map[string]bool) ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		original := GetArguments(request)
		source, args, err := kiteViewRequest(original, excluded)
		if err != nil {
			return NewToolResultError(err.Error()), nil
		}
		var next ToolHandler
		switch source {
		case "portfolio":
			next = (&PortfolioTool{}).Handler(manager)
		case "market":
			next = (&MarketTool{}).Handler(manager)
		case "orders_read":
			next = readWidgetHandler([]string{"list"}, (&OrdersTool{}).Handler(manager))
		case "gtt_read":
			next = readWidgetHandler([]string{"list"}, (&GTTTool{}).Handler(manager))
		default:
			return NewToolResultError("Unsupported Kite view."), nil
		}
		copy := *request
		params := *request.Params
		params.Name = source
		params.Arguments, _ = json.Marshal(args)
		copy.Params = &params
		result, err := withAppData(source, next)(ctx, &copy)
		if err == nil && result != nil {
			if info, ok := result.Meta["trade.kite/app"].(map[string]any); ok {
				info["origin"] = map[string]any{"tool": viewLauncher, "arguments": original}
			}
		}
		return result, err
	}
}
