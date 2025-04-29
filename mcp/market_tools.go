package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting quotes", err)
			return nil, err
		}

		args := request.Params.Arguments

		instruments := assertStringArray(args["instruments"])

		quotes, err := kc.Kite.Client.GetQuote(instruments...)
		if err != nil {
			log.Println("error getting quotes", err)
			return nil, err
		}

		v, err := json.Marshal(quotes)
		if err != nil {
			log.Println("error marshalling quotes", err)
			return nil, err
		}

		quotesJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: quotesJSON,
				},
			},
		}, nil
	}
}

type InstrumentsSearchTool struct{}

func (*InstrumentsSearchTool) Tool() mcp.Tool {
	return mcp.NewTool("search_instruments", // TODO this can be multiplexed into various modes. Currently only the filter mode is implemented but other instruments queries in the instruments manager can be exposed here as well.
		mcp.WithDescription("Get a list of all instruments"),
		mcp.WithString("query",
			mcp.Description("Search query"),
			mcp.Required(),
		),
		mcp.WithString("filter_on",
			mcp.Description("Filter on a specific field. (Optional). [id(default)=exch:tradingsymbol, name=nice name of the instrument, tradingsymbol=used to trade in a specific exchange, isin=universal identifier for an instrument across exchanges], underlying=[query=underlying instrument, result=futures and options. note=query format -> exch:tradingsymbol where NSE/BSE:PNB converted to -> NFO/BFO:PNB for query since futures and options available under them]"),
			mcp.Enum("id", "name", "isin", "tradingsymbol", "underlying"),
		),
	)
}

func (*InstrumentsSearchTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments

		query := assertString(args["query"])
		filterOn := assertString(args["filter_on"])
		// TODO: maybe we can add some pagination here.

		manager.Instruments.UpdateInstruments()
		out := []instruments.Instrument{}

		switch filterOn {
		case "underlying":
			// query needs to be split by `:` into exch and underlying.
			if strings.Contains(query, ":") {
				parts := strings.Split(query, ":")
				if len(parts) != 2 {
					return nil, errors.New("invalid query format, specify exch:underlying, where exchange is BFO/NFO")
				}

				exch := parts[0]
				underlying := parts[1]

				instruments, _ := manager.Instruments.GetAllByUnderlying(exch, underlying)
				out = instruments
			} else {
				// Assume query is just the underlying symbol and try. Just to save prompt calls.
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

		v, err := json.Marshal(out)
		if err != nil {
			log.Println("error marshalling instruments", err)
			return nil, err
		}

		instrumentsJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: instrumentsJSON,
				},
			},
		}, nil
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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		args := request.Params.Arguments

		// Parse instrument token
		instrumentToken := assertInt(args["instrument_token"])

		// Parse from_date and to_date
		fromDate, err := time.Parse("2006-01-02 15:04:05", assertString(args["from_date"]))
		if err != nil {
			log.Println("error parsing from_date", err)
			return nil, err
		}

		toDate, err := time.Parse("2006-01-02 15:04:05", assertString(args["to_date"]))
		if err != nil {
			log.Println("error parsing to_date", err)
			return nil, err
		}

		// Get other parameters
		interval := assertString(args["interval"])
		continuous := false
		if args["continuous"] != nil {
			continuous = assertBool(args["continuous"])
		}
		oi := false
		if args["oi"] != nil {
			oi = assertBool(args["oi"])
		}

		// Get historical data
		historicalData, err := kc.Kite.Client.GetHistoricalData(
			instrumentToken,
			interval,
			fromDate,
			toDate,
			continuous,
			oi,
		)
		if err != nil {
			log.Println("error getting historical data", err)
			return nil, err
		}

		// Convert to JSON
		v, err := json.Marshal(historicalData)
		if err != nil {
			log.Println("error marshalling historical data", err)
			return nil, err
		}

		historicalDataJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: historicalDataJSON,
				},
			},
		}, nil
	}
}
