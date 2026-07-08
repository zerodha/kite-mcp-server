package mcp

import (
	"encoding/json"
	"fmt"
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
		"mode": {
			"type": "string",
			"description": "Operation mode: quote=full market data snapshot (OHLC, depth, OI), ltp=last traded price only, ohlc=today's open/high/low/close, historical=historical OHLC candles, search=search instrument master list",
			"enum": ["quote", "ltp", "ohlc", "historical", "search"]
		},
		"instruments": {
			"type": "array",
			"description": "List of instruments in exchange:tradingsymbol format, e.g. ['NSE:INFY', 'NSE:SBIN']. Required for quote, ltp, ohlc modes. Up to 500 instruments per call.",
			"items": { "type": "string" }
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
			"description": "Start date in YYYY-MM-DD HH:MM:SS format (required for historical mode)"
		},
		"to_date": {
			"type": "string",
			"description": "End date in YYYY-MM-DD HH:MM:SS format (required for historical mode)"
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
			"type": "number",
			"description": "Starting index for pagination (0-based). Applies to search mode. Default: 0"
		},
		"limit": {
			"type": "number",
			"description": "Maximum number of items to return. When specified, response includes pagination metadata"
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
	return NewTool("market",
		"Retrieve market data and search instruments. Use mode=quote for full market snapshots (OHLC, volume, bid/ask depth, OI) for up to 500 instruments, mode=ltp for last traded price only (lighter), mode=ohlc for today's OHLC, mode=historical for historical candle data, mode=search to find instruments by name, symbol, ISIN, or token across all exchanges. Search results come from the BOD instrument master by default; set include_live_quote=true to enrich returned results with live quote fields.",
		marketSchema,
	)
}

func (*MarketTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")

		switch mode {
		case "quote":
			if err := ValidateRequired(args, "instruments"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			instrumentsList := SafeAssertStringArray(args["instruments"])
			return handler.WithKiteClient(request, "market_quote", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				quotes, err := client.GetQuote(instrumentsList...)
				if err != nil {
					return NewToolResultError("Failed to get quotes"), nil
				}
				return handler.MarshalResponse(quotes, "market_quote")
			})

		case "ltp":
			if err := ValidateRequired(args, "instruments"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			instrumentsList := SafeAssertStringArray(args["instruments"])
			if len(instrumentsList) == 0 {
				return NewToolResultError("At least one instrument must be specified"), nil
			}
			return handler.WithKiteClient(request, "market_ltp", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				ltp, err := client.GetLTP(instrumentsList...)
				if err != nil {
					return NewToolResultError("Failed to get LTP"), nil
				}
				return handler.MarshalResponse(ltp, "market_ltp")
			})

		case "ohlc":
			if err := ValidateRequired(args, "instruments"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			instrumentsList := SafeAssertStringArray(args["instruments"])
			if len(instrumentsList) == 0 {
				return NewToolResultError("At least one instrument must be specified"), nil
			}
			return handler.WithKiteClient(request, "market_ohlc", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				ohlc, err := client.GetOHLC(instrumentsList...)
				if err != nil {
					return NewToolResultError("Failed to get OHLC data"), nil
				}
				return handler.MarshalResponse(ohlc, "market_ohlc")
			})

		case "historical":
			if err := ValidateRequired(args, "instrument_token", "from_date", "to_date", "interval"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			instrumentToken := SafeAssertInt(args["instrument_token"], 0)
			fromDate, err := time.Parse("2006-01-02 15:04:05", SafeAssertString(args["from_date"], ""))
			if err != nil {
				return NewToolResultError("Failed to parse from_date, use format YYYY-MM-DD HH:MM:SS"), nil
			}
			toDate, err := time.Parse("2006-01-02 15:04:05", SafeAssertString(args["to_date"], ""))
			if err != nil {
				return NewToolResultError("Failed to parse to_date, use format YYYY-MM-DD HH:MM:SS"), nil
			}
			interval := SafeAssertString(args["interval"], "")
			continuous := SafeAssertBool(args["continuous"], false)
			oi := SafeAssertBool(args["oi"], false)

			return handler.WithKiteClient(request, "market_historical", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				data, err := client.GetHistoricalData(instrumentToken, interval, fromDate, toDate, continuous, oi)
				if err != nil {
					return NewToolResultError("Failed to get historical data"), nil
				}
				return handler.MarshalResponse(data, "market_historical")
			})

		case "search":
			return handleMarketSearch(handler, manager, args, request)

		default:
			return NewToolResultError("Invalid mode. Must be one of: quote, ltp, ohlc, historical, search"), nil
		}
	}
}

func handleMarketSearch(handler *BaseToolHandler, manager *kc.Manager, args map[string]interface{}, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	params := ParsePaginationParams(args)
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
	for i, instrument := range paginatedData {
		if verbosity == "compact" {
			item := marketSearchCompactResult(instrument)
			if includeLiveQuote {
				quote := liveQuotes[instrument.ID]
				enrichMarketSearchResult(item, quote.LastPrice, quote.LowerCircuitLimit, quote.UpperCircuitLimit, quote.OHLC, quote.Timestamp)
			}
			finalData[i] = item
		} else {
			item := marketSearchFullResult(instrument)
			if includeLiveQuote {
				quote := liveQuotes[instrument.ID]
				enrichMarketSearchResult(item, quote.LastPrice, quote.LowerCircuitLimit, quote.UpperCircuitLimit, quote.OHLC, quote.Timestamp)
			}
			finalData[i] = item
		}
	}

	meta := map[string]interface{}{
		"instrument_data_source": "bod_instruments",
		"live_quote_enriched":    includeLiveQuote,
	}
	if includeLiveQuote {
		meta["live_quote_fields"] = []string{"last_price", "lower_circuit_limit", "upper_circuit_limit", "ohlc", "timestamp"}
	}

	var responseData interface{}
	if params.Limit > 0 {
		paginatedResponse := CreatePaginatedResponse(out, finalData, params, originalLength)
		responseData = map[string]interface{}{
			"data":       paginatedResponse.Data,
			"pagination": paginatedResponse.Pagination,
			"meta":       meta,
		}
	} else {
		responseData = map[string]interface{}{
			"data": finalData,
			"meta": meta,
		}
	}

	if manager.Metrics() != nil {
		manager.Metrics().IncrementDailyWithLabels("instruments_search_results", map[string]string{
			"mode":      searchMode,
			"verbosity": verbosity,
			"count":     fmt.Sprintf("%d", len(out)),
		})
	}

	return handler.MarshalResponse(responseData, "market_search")
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
