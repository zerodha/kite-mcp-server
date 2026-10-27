package mcp

import (
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
)

// Option-chain discovery uses the complete, atomically loaded BOD catalog.
// No quote request is made and no instrument-master price is presented as live.
func handleMarketOptionChain(handler *BaseToolHandler, manager *kc.Manager, args map[string]interface{}) (*mcp.CallToolResult, error) {
	params, err := ParsePaginationParams(args)
	if err != nil {
		return NewToolResultError(err.Error()), nil
	}
	underlying := strings.TrimSpace(SafeAssertString(args["underlying"], ""))
	if underlying == "" || len(underlying) > 200 {
		return NewToolResultError("underlying must be a non-empty name or symbol of at most 200 bytes."), nil
	}
	exchange := "NFO"
	if raw, exists := args["exchange"]; exists {
		value, ok := raw.(string)
		if !ok {
			return NewToolResultError("option_chain exchange must be NFO/NSE or BFO/BSE."), nil
		}
		exchange = strings.ToUpper(strings.TrimSpace(value))
	}
	if exchange != "NFO" && exchange != "NSE" && exchange != "BFO" && exchange != "BSE" {
		return NewToolResultError("option_chain exchange must be NFO/NSE or BFO/BSE."), nil
	}
	expiry := ""
	if raw, exists := args["expiry"]; exists {
		var ok bool
		expiry, ok = raw.(string)
		if !ok {
			return NewToolResultError("expiry must be a date in YYYY-MM-DD format."), nil
		}
		parsed, parseErr := time.Parse("2006-01-02", expiry)
		if parseErr != nil || parsed.Format("2006-01-02") != expiry {
			return NewToolResultError("expiry must be a valid date in YYYY-MM-DD format."), nil
		}
	}
	snapshot := ""
	if raw, exists := args["snapshot"]; exists {
		var ok bool
		snapshot, ok = raw.(string)
		if !ok || snapshot == "" || len(snapshot) > 80 {
			return NewToolResultError("snapshot must be the catalog_revision string returned by option_chain."), nil
		}
	}
	if manager.Instruments == nil {
		return NewToolResultError("Instrument manager is not initialized."), nil
	}
	chain, err := manager.Instruments.GetOptionChain(exchange, underlying, expiry)
	if err != nil {
		return NewToolResultError(err.Error()), nil
	}
	revision := chain.LoadedAt.Format(time.RFC3339Nano)
	if snapshot != "" && snapshot != revision {
		return NewToolResultError("Instrument catalog changed; restart option-chain discovery from the first page."), nil
	}
	total := len(chain.Strikes)
	chain.Strikes = ApplyPagination(chain.Strikes, params)
	page := CreatePaginatedResponse(chain.Strikes, params, total)
	return marshalMarketResponse(handler, map[string]interface{}{
		"data":       chain,
		"pagination": page.Pagination,
		"meta": map[string]interface{}{
			"timezone":               "Asia/Kolkata",
			"instrument_data_source": "bod_instruments",
			"catalog_revision":       revision,
			"live_quote_enriched":    false,
			"pagination_unit":        "strikes",
			"total_strikes":          total,
			"complete":               params.From == 0 && len(chain.Strikes) == total,
			"truncated":              params.From > 0 || len(chain.Strikes) < total,
			"scope":                  "selected_expiry",
			"timestamp_note":         "Instrument-master prices are reference values; fetch mode=quote for current prices.",
		},
	}, "market_option_chain")
}
