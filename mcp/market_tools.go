package mcp

import (
	"context"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

type QuotesTool struct{}

func (*QuotesTool) Tool() mcp.Tool {
	return mcp.NewTool("get_quotes",
		mcp.WithDescription("Get market data quotes for a list of instruments"),
		mcp.WithArray("instruments",
			mcp.Description("Eg. ['NSE:INFY', 'NSE:SBIN']. This API returns the complete market data snapshot of up to 500 instruments in one go. It includes the quantity, OHLC, and Open Interest fields, and the complete bid/ask market depth amongst others. Instruments are identified by the exchange:tradingsymbol combination and are passed as values to the query parameter i which is repeated for every instrument. If there is no data available for a given key, the key will be absent from the response."),
			mcp.Required(),
			mcp.Items(map[string]any{
				"type": "string",
			}),
		),
	)
}

func (*QuotesTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "instruments"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		instruments := SafeAssertStringArray(args["instruments"])
		return handler.WithKiteClient(ctx, "get_quotes", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			quotes, err := client.GetQuote(instruments...)
			if err != nil {
				return mcp.NewToolResultError("Failed to get quotes"), nil
			}
			return handler.MarshalResponse(quotes, "get_quotes")
		})
	}
}

type InstrumentsSearchTool struct{}

func (*InstrumentsSearchTool) Tool() mcp.Tool {
	return mcp.NewTool("search_instruments", // TODO this can be multiplexed into various modes. Currently only the filter mode is implemented but other instruments queries in the instruments manager can be exposed here as well.
		mcp.WithDescription("Search instruments. Supports pagination for large result sets."),
		mcp.WithString("query",
			mcp.Description("Search query"),
			mcp.Required(),
		),
		mcp.WithString("filter_on",
			mcp.Description("Filter on a specific field. (Optional). [id(default)=exch:tradingsymbol, name=nice name of the instrument, tradingsymbol=used to trade in a specific exchange, isin=universal identifier for an instrument across exchanges], underlying=[query=underlying instrument, result=futures and options. note=query format -> exch:tradingsymbol where NSE/BSE:PNB converted to -> NFO/BFO:PNB for query since futures and options available under them]"),
			mcp.Enum("id", "name", "isin", "tradingsymbol", "underlying"),
		),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of instruments to return. If not specified, returns all matching instruments. When specified, response includes pagination metadata."),
		),
	)
}

func (*InstrumentsSearchTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "query"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		query := SafeAssertString(args["query"], "")
		filterOn := SafeAssertString(args["filter_on"], "id")

		if manager.Instruments == nil {
			return mcp.NewToolResultError("Instrument manager is not initialized."), nil
		}

		if manager.Instruments.Count() == 0 {
			manager.Logger.Warn("No instruments loaded, search may return incomplete results")
		}

		var out []instruments.Instrument

		switch filterOn {
		case "underlying":
			if strings.Contains(query, ":") {
				parts := strings.Split(query, ":")
				if len(parts) != 2 {
					return mcp.NewToolResultError("Invalid query format, specify exch:underlying, where exchange is BFO/NFO"), nil
				}
				exch := parts[0]
				underlying := parts[1]
				instruments, _ := manager.Instruments.GetAllByUnderlying(exch, underlying)
				out = instruments
			} else {
				exch := "NFO"
				underlying := query
				instruments, _ := manager.Instruments.GetAllByUnderlying(exch, underlying)
				out = instruments
			}
		default:
			instruments := manager.Instruments.Filter(func(instrument instruments.Instrument) bool {
				switch filterOn {
				case "name":
					return strings.Contains(strings.ToLower(instrument.Name), strings.ToLower(query))
				case "tradingsymbol":
					return strings.Contains(strings.ToLower(instrument.Tradingsymbol), strings.ToLower(query))
				case "isin":
					return strings.Contains(strings.ToLower(instrument.ISIN), strings.ToLower(query))
				case "id":
					return strings.Contains(strings.ToLower(instrument.ID), strings.ToLower(query))
				default:
					return strings.Contains(strings.ToLower(instrument.ID), strings.ToLower(query))
				}
			})
			out = instruments
		}
		params := ParsePaginationParams(args)
		originalLength := len(out)
		paginatedData := ApplyPagination(out, params)
		var responseData interface{}
		if params.Limit > 0 {
			interfaceData := make([]interface{}, len(paginatedData))
			for i, instrument := range paginatedData {
				interfaceData[i] = instrument
			}
			responseData = CreatePaginatedResponse(out, interfaceData, params, originalLength)
		} else {
			responseData = paginatedData
		}
		return handler.MarshalResponse(responseData, "search_instruments")
	}
}

type HistoricalDataTool struct{}

func (*HistoricalDataTool) Tool() mcp.Tool {
	return mcp.NewTool("get_historical_data",
		mcp.WithDescription("Get historical price data for an instrument"),
		mcp.WithNumber("instrument_token",
			mcp.Description("Instrument token (can be obtained from search_instruments tool)"),
			mcp.Required(),
		),
		mcp.WithString("from_date",
			mcp.Description("From date in YYYY-MM-DD HH:MM:SS format"),
			mcp.Required(),
		),
		mcp.WithString("to_date",
			mcp.Description("To date in YYYY-MM-DD HH:MM:SS format"),
			mcp.Required(),
		),
		mcp.WithString("interval",
			mcp.Description("Candle interval"),
			mcp.Required(),
			mcp.Enum("minute", "day", "3minute", "5minute", "10minute", "15minute", "30minute", "60minute"),
		),
		mcp.WithBoolean("continuous",
			mcp.Description("Get continuous data (for futures and options)"),
			mcp.DefaultBool(false),
		),
		mcp.WithBoolean("oi",
			mcp.Description("Include open interest data"),
			mcp.DefaultBool(false),
		),
	)
}

func (*HistoricalDataTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "instrument_token", "from_date", "to_date", "interval"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		instrumentToken := SafeAssertInt(args["instrument_token"], 0)
		fromDate, err := time.Parse("2006-01-02 15:04:05", SafeAssertString(args["from_date"], ""))
		if err != nil {
			return mcp.NewToolResultError("Failed to parse from_date, use format YYYY-MM-DD HH:MM:SS"), nil
		}
		toDate, err := time.Parse("2006-01-02 15:04:05", SafeAssertString(args["to_date"], ""))
		if err != nil {
			return mcp.NewToolResultError("Failed to parse to_date, use format YYYY-MM-DD HH:MM:SS"), nil
		}
		interval := SafeAssertString(args["interval"], "")
		continuous := SafeAssertBool(args["continuous"], false)
		oi := SafeAssertBool(args["oi"], false)

		return handler.WithKiteClient(ctx, "get_historical_data", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			historicalData, err := client.GetHistoricalData(
				instrumentToken,
				interval,
				fromDate,
				toDate,
				continuous,
				oi,
			)
			if err != nil {
				return mcp.NewToolResultError("Failed to get historical data"), nil
			}
			return handler.MarshalResponse(historicalData, "get_historical_data")
		})
	}
}

type LTPTool struct{}

func (*LTPTool) Tool() mcp.Tool {
	return mcp.NewTool("get_ltp",
		mcp.WithDescription("Get latest trading prices for a list of instruments"),
		mcp.WithArray("instruments",
			mcp.Description("Eg. ['NSE:INFY', 'NSE:SBIN']. This API returns the lastest price for the given list of instruments in the format of exchange:tradingsymbol."),
			mcp.Required(),
			mcp.Items(map[string]any{
				"type": "string",
			}),
		),
	)
}

func (*LTPTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "instruments"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		instruments := SafeAssertStringArray(args["instruments"])
		if len(instruments) == 0 {
			return mcp.NewToolResultError("At least one instrument must be specified"), nil
		}
		return handler.WithKiteClient(ctx, "get_ltp", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			ltp, err := client.GetLTP(instruments...)
			if err != nil {
				return mcp.NewToolResultError("Failed to get latest trading prices"), nil
			}
			return handler.MarshalResponse(ltp, "get_ltp")
		})
	}
}

type OHLCTool struct{}

func (*OHLCTool) Tool() mcp.Tool {
	return mcp.NewTool("get_ohlc",
		mcp.WithDescription("Get OHLC (Open, High, Low, Close) data for a list of instruments"),
		mcp.WithArray("instruments",
			mcp.Description("Eg. ['NSE:INFY', 'NSE:SBIN']. This API returns OHLC data for the given list of instruments in the format of exchange:tradingsymbol."),
			mcp.Required(),
			mcp.Items(map[string]any{
				"type": "string",
			}),
		),
	)
}

func (*OHLCTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "instruments"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		instruments := SafeAssertStringArray(args["instruments"])
		if len(instruments) == 0 {
			return mcp.NewToolResultError("At least one instrument must be specified"), nil
		}
		return handler.WithKiteClient(ctx, "get_ohlc", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			ohlc, err := client.GetOHLC(instruments...)
			if err != nil {
				return mcp.NewToolResultError("Failed to get OHLC data"), nil
			}
			return handler.MarshalResponse(ohlc, "get_ohlc")
		})
	}
}
