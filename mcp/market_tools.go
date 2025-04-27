package mcp

import (
	"context"
	"encoding/json"
	"log"
	"strings"

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
	)
}

func (*InstrumentsSearchTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments

		query := assertString(args["query"])

		manager.Instruments.UpdateInstruments()

		instruments := manager.Instruments.Filter(func(instrument instruments.Instrument) bool {
			return strings.Contains(strings.ToLower(instrument.ID), strings.ToLower(query))
		})

		v, err := json.Marshal(instruments)
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
