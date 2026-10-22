package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

type MarketTool struct{}

var marketSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"_app_request_id": {
			"type": "string",
			"maxLength": 80,
			"description": "Optional opaque correlation ID supplied by the embedded explorer. Other callers should omit it."
		},
		"mode": {
			"type": "string",
			"description": "Operation mode: quote=full market data snapshot (OHLC, depth, OI), ltp=last traded price only, ohlc=today's open/high/low/close, historical=historical OHLC candles, search=search instrument master list",
			"enum": ["quote", "ltp", "ohlc", "historical", "search"]
		},
		"instruments": {
			"type": "array",
			"description": "List of instruments in exchange:tradingsymbol format, e.g. ['NSE:INFY', 'NSE:SBIN']. Required for quote, ltp, ohlc modes. Up to 500 instruments per call.",
			"minItems": 1,
			"maxItems": 500,
			"items": { "type": "string", "minLength": 1 }
		},
		"instrument_token": {
			"type": "number",
			"description": "Numeric instrument token (required for historical mode). Use search mode to find this."
		},
		"interval": {
			"type": "string",
			"description": "Candle interval (required for historical mode)",
			"enum": ["minute", "3minute", "5minute", "10minute", "15minute", "30minute", "60minute", "day"]
		},
		"from_date": {
			"type": "string",
			"description": "Start date in IST: YYYY-MM-DD HH:MM:SS or YYYY-MM-DD (start of day; required for historical mode)"
		},
		"to_date": {
			"type": "string",
			"description": "End date in IST: YYYY-MM-DD HH:MM:SS or YYYY-MM-DD (end of day; required for historical mode)"
		},
		"continuous": {
			"type": "boolean",
			"description": "Stitch futures contracts into a continuous series (historical mode only)",
			"default": false
		},
		"oi": {
			"type": "boolean",
			"description": "Include open interest data (historical mode only)",
			"default": false
		},
		"search_mode": {
			"type": "string",
			"description": "Search lookup mode (only for mode=search): search=text search, get_by_id=exact ID, get_by_tradingsymbol=exchange+symbol, get_by_isin=ISIN, get_by_inst_token=instrument token, get_by_exch_token=exchange token",
			"default": "search",
			"enum": ["search", "get_by_id", "get_by_tradingsymbol", "get_by_isin", "get_by_inst_token", "get_by_exch_token"]
		},
		"verbosity": {
			"type": "string",
			"description": "Response verbosity for search mode: compact=essential fields (recommended), full=all instrument details",
			"default": "compact",
			"enum": ["compact", "full"]
		},
		"query": {
			"type": "string",
			"description": "Search text (required for mode=search with search_mode=search)"
		},
		"filter_on": {
			"type": "string",
			"description": "Field to search on (for search_mode=search): id=exchange:tradingsymbol, name=instrument name, isin=ISIN, tradingsymbol=symbol only, underlying=F&O underlying (query format: exch:underlying, e.g. NFO:NIFTY)",
			"enum": ["id", "name", "isin", "tradingsymbol", "underlying"]
		},
		"id": {
			"type": "string",
			"description": "Instrument ID in EXCHANGE:TRADINGSYMBOL format (for search_mode=get_by_id)"
		},
		"exchange": {
			"type": "string",
			"description": "Exchange code (for search_mode=get_by_tradingsymbol and get_by_exch_token)"
		},
		"tradingsymbol": {
			"type": "string",
			"description": "Trading symbol (for search_mode=get_by_tradingsymbol)"
		},
		"isin": {
			"type": "string",
			"description": "ISIN identifier (for search_mode=get_by_isin)"
		},
		"inst_token": {
			"type": "number",
			"description": "Instrument token (for search_mode=get_by_inst_token)"
		},
		"exch_token": {
			"type": "number",
			"description": "Exchange token (for search_mode=get_by_exch_token)"
		},
		"from": {
			"type": "integer",
			"minimum": 0,
			"maximum": 2147483647,
			"default": 0,
			"description": "Starting index for pagination (0-based). Applies to search mode. Default: 0"
		},
		"limit": {
			"type": "integer",
			"minimum": 1,
			"maximum": 500,
			"default": 100,
			"description": "Page size (integer 1-500, default 100). Responses always include pagination metadata"
		},
		"include_live_quote": {
			"type": "boolean",
			"description": "For mode=search, optionally enrich returned results with live quote fields like last_price and circuit limits using the quote endpoint",
			"default": false
		}
	},
	"required": ["mode"]
}`)

func (*MarketTool) Definition() *mcp.Tool {
	tool := NewTool("market",
		"Retrieve market data and search instruments. Use mode=quote for full market snapshots (OHLC, volume, bid/ask depth, OI) for up to 500 instruments, mode=ltp for last traded price only (lighter), mode=ohlc for today's OHLC, mode=historical for historical candle data, mode=search to find instruments by name, symbol, ISIN, or token across all exchanges. Search returns data/pagination/meta, sorted by ID then token, with default limit 100 and maximum 500. Quote/LTP/OHLC return data/meta with missing_instruments; entirely unresolved requests return an error. Search results come from the BOD instrument master by default; set include_live_quote=true to enrich returned results with live quote fields and quote_timestamp.",
		marketSchema,
	)
	// Negotiated protocols through 2025-11-25 require object output schemas
	// and object structuredContent, including for historical candle lists.
	tool.OutputSchema = json.RawMessage(`{
		"type":"object", "required":["data","meta"], "properties":{
			"data":{"type":["object","array"]}, "meta":{"type":"object"},
			"pagination":{"type":"object"}, "error":{"type":"string"}
		}
	}`)
	return tool
}

func (*MarketTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")

		switch mode {
		case "quote":
			requested, err := parseMarketInstruments(args)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return handler.WithKiteClient(request, "market_quote", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				quotes, err := client.GetQuote(requested...)
				if err != nil {
					return NewToolResultError("Failed to get quotes"), nil
				}
				return marshalMarketData(handler, requested, quotes, "market_quote")
			})

		case "ltp":
			requested, err := parseMarketInstruments(args)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return handler.WithKiteClient(request, "market_ltp", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				ltp, err := client.GetLTP(requested...)
				if err != nil {
					return NewToolResultError("Failed to get LTP"), nil
				}
				return marshalMarketData(handler, requested, ltp, "market_ltp")
			})

		case "ohlc":
			requested, err := parseMarketInstruments(args)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return handler.WithKiteClient(request, "market_ohlc", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				ohlc, err := client.GetOHLC(requested...)
				if err != nil {
					return NewToolResultError("Failed to get OHLC data"), nil
				}
				return marshalMarketData(handler, requested, ohlc, "market_ohlc")
			})

		case "historical":
			if err := ValidateRequired(args, "instrument_token", "from_date", "to_date", "interval"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			instrumentToken := SafeAssertInt(args["instrument_token"], 0)
			fromDate, err := parseHistoricalDate(SafeAssertString(args["from_date"], ""), false)
			if err != nil {
				return NewToolResultError("Failed to parse from_date, use format YYYY-MM-DD HH:MM:SS or YYYY-MM-DD (IST)"), nil
			}
			toDate, err := parseHistoricalDate(SafeAssertString(args["to_date"], ""), true)
			if err != nil {
				return NewToolResultError("Failed to parse to_date, use format YYYY-MM-DD HH:MM:SS or YYYY-MM-DD (IST)"), nil
			}
			if fromDate.After(toDate) {
				return NewToolResultError("from_date must be earlier than or equal to to_date (IST); correct the date range"), nil
			}
			interval := SafeAssertString(args["interval"], "")
			continuous := SafeAssertBool(args["continuous"], false)
			oi := SafeAssertBool(args["oi"], false)

			return handler.WithKiteClient(request, "market_historical", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				data, err := client.GetHistoricalData(instrumentToken, interval, fromDate, toDate, continuous, oi)
				if err != nil {
					return NewToolResultError("Failed to get historical data"), nil
				}
				if data == nil {
					data = make([]kiteconnect.HistoricalData, 0)
				}
				return marshalMarketResponse(handler, map[string]interface{}{
					"data": data,
					"meta": map[string]interface{}{"timezone": "Asia/Kolkata"},
				}, "market_historical")
			})

		case "search":
			return handleMarketSearch(handler, manager, args, request)

		default:
			return NewToolResultError("Invalid mode. Must be one of: quote, ltp, ohlc, historical, search"), nil
		}
	}
}

func handleMarketSearch(handler *BaseToolHandler, manager *kc.Manager, args map[string]interface{}, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params, parseErr := ParsePaginationParams(args)
	if parseErr != nil {
		return NewToolResultError(parseErr.Error()), nil
	}
	searchMode := SafeAssertString(args["search_mode"], "search")
	verbosity := SafeAssertString(args["verbosity"], "compact")

	if manager.Metrics() != nil {
		manager.Metrics().IncrementDailyWithLabels("tool_calls", map[string]string{"tool": "market_search"})
		manager.Metrics().IncrementDailyWithLabels("instruments_search_mode", map[string]string{"mode": searchMode})
		manager.Metrics().IncrementDailyWithLabels("instruments_search_verbosity", map[string]string{"verbosity": verbosity})
	}

	if manager.Instruments == nil {
		return NewToolResultError("Instrument manager is not initialized."), nil
	}

	if manager.Instruments.Count() == 0 {
		manager.Logger.Warn("No instruments loaded, search may return incomplete results")
	}

	var out []instruments.Instrument
	var err error

	switch searchMode {
	case "get_by_id":
		if err := ValidateRequired(args, "id"); err != nil {
			return NewToolResultError(err.Error()), nil
		}
		id := SafeAssertString(args["id"], "")
		var instrument instruments.Instrument
		instrument, err = manager.Instruments.GetByID(id)
		if err == nil {
			out = []instruments.Instrument{instrument}
		}

	case "get_by_tradingsymbol":
		if err := ValidateRequired(args, "exchange", "tradingsymbol"); err != nil {
			return NewToolResultError(err.Error()), nil
		}
		exchange := SafeAssertString(args["exchange"], "")
		tradingsymbol := SafeAssertString(args["tradingsymbol"], "")
		var instrument instruments.Instrument
		instrument, err = manager.Instruments.GetByTradingsymbol(exchange, tradingsymbol)
		if err == nil {
			out = []instruments.Instrument{instrument}
		}

	case "get_by_isin":
		if err := ValidateRequired(args, "isin"); err != nil {
			return NewToolResultError(err.Error()), nil
		}
		isn := SafeAssertString(args["isin"], "")
		out, err = manager.Instruments.GetByISIN(isn)

	case "get_by_inst_token":
		if err := ValidateRequired(args, "inst_token"); err != nil {
			return NewToolResultError(err.Error()), nil
		}
		token := SafeAssertInt(args["inst_token"], 0)
		var instrument instruments.Instrument
		instrument, err = manager.Instruments.GetByInstToken(uint32(token))
		if err == nil {
			out = []instruments.Instrument{instrument}
		}

	case "get_by_exch_token":
		if err := ValidateRequired(args, "exchange", "exch_token"); err != nil {
			return NewToolResultError(err.Error()), nil
		}
		exchange := SafeAssertString(args["exchange"], "")
		exchToken := SafeAssertInt(args["exch_token"], 0)
		var instrument instruments.Instrument
		instrument, err = manager.Instruments.GetByExchToken(exchange, uint32(exchToken))
		if err == nil {
			out = []instruments.Instrument{instrument}
		}

	default: // "search"
		if err := ValidateRequired(args, "query"); err != nil {
			return NewToolResultError(err.Error()), nil
		}
		query := SafeAssertString(args["query"], "")
		filterOn := SafeAssertString(args["filter_on"], "id")

		switch filterOn {
		case "underlying":
			if strings.Contains(query, ":") {
				parts := strings.Split(query, ":")
				if len(parts) != 2 {
					return NewToolResultError("Invalid query format, specify exch:underlying, where exchange is BFO/NFO"), nil
				}
				out, _ = manager.Instruments.GetAllByUnderlying(parts[0], parts[1])
			} else {
				out, _ = manager.Instruments.GetAllByUnderlying("NFO", query)
			}
		default:
			out = manager.Instruments.Filter(func(instrument instruments.Instrument) bool {
				switch filterOn {
				case "name":
					return strings.Contains(strings.ToLower(instrument.Name), strings.ToLower(query))
				case "tradingsymbol":
					return strings.Contains(strings.ToLower(instrument.Tradingsymbol), strings.ToLower(query))
				case "isin":
					return strings.Contains(strings.ToLower(instrument.ISIN), strings.ToLower(query))
				default: // "id"
					return strings.Contains(strings.ToLower(instrument.ID), strings.ToLower(query))
				}
			})
		}
	}

	if err != nil {
		return NewToolResultError("Instrument not found"), nil
	}

	// Sort every multi-result lookup before slicing, including ISIN and
	// underlying searches. Instrument token breaks ties in the public ID.
	slices.SortFunc(out, func(a, b instruments.Instrument) int {
		if order := cmp.Compare(a.ID, b.ID); order != 0 {
			return order
		}
		return cmp.Compare(a.InstrumentToken, b.InstrumentToken)
	})
	originalLength := len(out)
	paginatedData := ApplyPagination(out, params)
	includeLiveQuote := SafeAssertBool(args["include_live_quote"], false)

	var liveQuotes kiteconnect.Quote
	if includeLiveQuote && len(paginatedData) > 0 {
		client, authErr := manager.GetAuthenticatedClient(GetSessionID(request))
		if authErr != nil {
			return NewToolResultError(authErr.Error()), nil
		}

		instrumentIDs := make([]string, 0, len(paginatedData))
		for _, instrument := range paginatedData {
			instrumentIDs = append(instrumentIDs, instrument.ID)
		}

		liveQuotes, err = client.GetQuote(instrumentIDs...)
		if err != nil {
			return NewToolResultError("Failed to get live quotes for search enrichment"), nil
		}
	}

	finalData := make([]interface{}, len(paginatedData))
	enrichedCount := 0
	missingQuotes := make([]string, 0)
	for i, instrument := range paginatedData {
		var item map[string]interface{}
		if verbosity == "compact" {
			item = marketSearchCompactResult(instrument)
		} else {
			item = marketSearchFullResult(instrument)
		}
		if includeLiveQuote {
			if quote, ok := liveQuotes[instrument.ID]; ok {
				enrichMarketSearchResult(item, quote.LastPrice, quote.LowerCircuitLimit, quote.UpperCircuitLimit, quote.OHLC, quote.Timestamp)
				enrichedCount++
			} else {
				missingQuotes = append(missingQuotes, instrument.ID)
			}
		}
		finalData[i] = item
	}
	meta := map[string]interface{}{
		"instrument_data_source": "bod_instruments",
		"live_quote_enriched":    enrichedCount > 0,
	}
	if includeLiveQuote {
		fields := make([]string, 0)
		if enrichedCount > 0 {
			fields = []string{"last_price", "lower_circuit_limit", "upper_circuit_limit", "ohlc", "quote_timestamp"}
		}
		meta["live_quote_fields"] = fields
		meta["missing_live_quotes"] = missingQuotes
	}
	page := CreatePaginatedResponse(finalData, params, originalLength)
	responseData := map[string]interface{}{"data": page.Data, "pagination": page.Pagination, "meta": meta}

	if manager.Metrics() != nil {
		manager.Metrics().IncrementDailyWithLabels("instruments_search_results", map[string]string{
			"mode":      searchMode,
			"verbosity": verbosity,
			"count":     fmt.Sprintf("%d", len(out)),
		})
	}

	return marshalMarketResponse(handler, responseData, "market_search")
}

func marketSearchCompactResult(instrument instruments.Instrument) map[string]interface{} {
	compact := instrument.ToCompact()
	return map[string]interface{}{
		"id":               compact.ID,
		"instrument_token": compact.InstrumentToken,
		"tradingsymbol":    compact.Tradingsymbol,
		"exchange":         compact.Exchange,
		"name":             compact.Name,
		"instrument_type":  compact.InstrumentType,
		"segment":          compact.Segment,
		"lot_size":         compact.LotSize,
		"active":           compact.Active,
	}
}

func marketSearchFullResult(instrument instruments.Instrument) map[string]interface{} {
	return map[string]interface{}{
		"id":                  instrument.ID,
		"instrument_token":    instrument.InstrumentToken,
		"exchange_token":      instrument.ExchangeToken,
		"tradingsymbol":       instrument.Tradingsymbol,
		"exchange":            instrument.Exchange,
		"isin":                instrument.ISIN,
		"name":                instrument.Name,
		"series":              instrument.Series,
		"last_price":          instrument.LastPrice,
		"strike":              instrument.Strike,
		"tick_size":           instrument.TickSize,
		"lot_size":            instrument.LotSize,
		"multiplier":          instrument.Multiplier,
		"instrument_type":     instrument.InstrumentType,
		"segment":             instrument.Segment,
		"delivery_units":      instrument.DeliveryUnits,
		"price_units":         instrument.PriceUnits,
		"freeze_quantity":     instrument.FreezeQuantity,
		"max_order_quantity":  instrument.MaxOrderQuantity,
		"expiry_type":         instrument.ExpiryType,
		"expiry_date":         instrument.ExpiryDate,
		"exercise_start_date": instrument.ExerciseStartDate,
		"exercise_end_date":   instrument.ExerciseEndDate,
		"issue_date":          instrument.IssueDate,
		"listing_date":        instrument.ListingDate,
		"maturity_date":       instrument.MaturityDate,
		"lower_circuit_limit": instrument.LowerCircuitLimit,
		"upper_circuit_limit": instrument.UpperCircuitLimit,
		"active":              instrument.Active,
	}
}

func enrichMarketSearchResult(item map[string]interface{}, lastPrice, lowerCircuitLimit, upperCircuitLimit float64, ohlc interface{}, timestamp interface{}) {
	item["last_price"] = lastPrice
	item["lower_circuit_limit"] = lowerCircuitLimit
	item["upper_circuit_limit"] = upperCircuitLimit
	item["ohlc"] = ohlc
	item["quote_timestamp"] = timestamp
}

// parseMarketInstruments validates instead of silently dropping or stringifying
// malformed array elements. Bound the upstream request before making any calls.
func parseMarketInstruments(args map[string]interface{}) ([]string, error) {
	raw, ok := args["instruments"].([]interface{})
	if !ok || len(raw) == 0 || len(raw) > 500 {
		return nil, ValidationError{"instruments", "must be an array of 1-500 non-empty strings"}
	}
	requested := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, value := range raw {
		symbol, ok := value.(string)
		if !ok || strings.TrimSpace(symbol) == "" {
			return nil, ValidationError{"instruments", "must contain only non-empty strings"}
		}
		symbol = strings.TrimSpace(symbol)
		if !seen[symbol] {
			requested = append(requested, symbol)
			seen[symbol] = true
		}
	}
	return requested, nil
}

func marshalMarketData[T any](handler *BaseToolHandler, requested []string, returned map[string]T, toolName string) (*mcp.CallToolResult, error) {
	data := make(map[string]T, len(requested))
	missing := make([]string, 0)
	for _, symbol := range requested {
		if value, ok := returned[symbol]; ok {
			data[symbol] = value
		} else {
			missing = append(missing, symbol)
		}
	}
	response := map[string]interface{}{
		"data": data,
		"meta": map[string]interface{}{
			"requested_count": len(requested), "returned_count": len(data),
			"missing_instruments": missing, "partial": len(missing) > 0 && len(data) > 0,
		},
	}
	if toolName == "market_ltp" || toolName == "market_ohlc" {
		response["meta"].(map[string]interface{})["as_of_available"] = false
		response["meta"].(map[string]interface{})["timestamp_note"] = "Upstream does not provide an as-of timestamp; use mode=quote for timestamp and last_trade_time"
	}
	if len(data) == 0 {
		response["error"] = "No market data found for the requested instruments; check exchange and trading symbols"
	}
	result, err := marshalMarketResponse(handler, response, toolName)
	if err == nil && len(data) == 0 {
		result.IsError = true
	}
	return result, err
}

// Date-only bounds include the whole requested day in the exchange timezone.
func parseHistoricalDate(value string, endOfDay bool) (time.Time, error) {
	location := time.FixedZone("IST", 19800)
	if len(value) != len("2006-01-02") {
		return time.ParseInLocation("2006-01-02 15:04:05", value, location)
	}
	date, err := time.ParseInLocation("2006-01-02", value, location)
	if err == nil && endOfDay {
		date = date.Add(24*time.Hour - time.Second)
	}
	return date, err
}

// Keep existing JSON text shapes while exposing the identical structured value.
func marshalMarketResponse(handler *BaseToolHandler, data interface{}, toolName string) (*mcp.CallToolResult, error) {
	result, err := handler.MarshalResponse(data, toolName)
	if err == nil && !result.IsError {
		result.StructuredContent = data
	}
	return result, err
}
