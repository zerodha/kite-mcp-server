package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

func TestKiteViewRoutingAndExclusions(t *testing.T) {
	for _, tc := range []struct{ view, source, mode string }{
		{"holdings", "portfolio", "holdings"}, {"positions", "portfolio", "positions"}, {"funds", "portfolio", "margins"}, {"orders", "orders_read", "list"}, {"gtt", "gtt_read", "list"}, {"option_chain", "market", "option_chain"}, {"quote", "market", "quote"}, {"historical", "market", "historical"}, {"search", "market", "search"},
	} {
		t.Run(tc.view, func(t *testing.T) {
			args := map[string]interface{}{"view": tc.view}
			if tc.view == "quote" {
				args["instruments"] = []interface{}{"NSE:INFY"}
			}
			if tc.view == "historical" {
				args["instrument_token"] = float64(123)
				args["from_date"] = "2026-01-01"
				args["to_date"] = "2026-01-02"
				args["interval"] = "day"
			}
			if tc.view == "search" {
				args["query"] = "Infosys"
				args["filter_on"] = "name"
			}
			source, next, err := kiteViewRequest(args, nil)
			require.NoError(t, err)
			require.Equal(t, tc.source, source)
			require.Equal(t, tc.mode, next["mode"])
			if tc.view == "search" {
				require.Equal(t, "name", next["filter_on"])
				require.Equal(t, "Infosys", next["query"])
			}
			if tc.view == "option_chain" {
				require.Equal(t, "NIFTY", next["underlying"])
			}
			for _, excluded := range []map[string]bool{{viewLauncher: true}, {tc.source: true}} {
				_, _, err = kiteViewRequest(args, excluded)
				require.Error(t, err)
			}
			if tc.view == "orders" || tc.view == "gtt" {
				_, _, err = kiteViewRequest(args, map[string]bool{tc.view: true})
				require.Error(t, err)
			}
		})
	}
	for _, args := range []map[string]interface{}{
		{"view": "place"}, {"view": "delete"}, {"view": 123}, {"view": "orders", "mode": "cancel"}, {"view": "gtt", "trigger_id": 1}, {"view": "holdings", "limit": 501}, {"view": "positions", "from": -1}, {"view": "quote"}, {"view": "search", "query": "INFY", "filter_on": "bad"}, {"view": "historical"}, {"view": "search"}, {"view": "option_chain", "underlying": 123}, {"view": "orders", "instruments": []interface{}{"NSE:INFY"}},
	} {
		_, _, err := kiteViewRequest(args, nil)
		require.Error(t, err, "args=%v", args)
	}
}

func TestKiteViewInitialSnapshotsAndOrigins(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method, "view launcher must never mutate upstream data")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/portfolio/holdings", "/orders", "/gtt/triggers":
			fmt.Fprint(w, `{"status":"success","data":[]}`)
		case "/portfolio/positions":
			fmt.Fprint(w, `{"status":"success","data":{"net":[],"day":[]}}`)
		case "/user/margins":
			fmt.Fprint(w, `{"status":"success","data":{"equity":{"enabled":true,"net":1000},"commodity":{"enabled":false}}}`)
		default:
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	})
	manager.Instruments.LoadMap(map[uint32]*instruments.Instrument{11: {ID: "NFO:NIFTY99JAN20000CE", InstrumentToken: 11, Exchange: "NFO", Tradingsymbol: "NIFTY99JAN20000CE", Name: "NIFTY", InstrumentType: "CE", ExpiryDate: "2099-01-29", Strike: 20000, Active: true}})
	for _, view := range kiteViews[:6] {
		t.Run(view, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]interface{}{"view": view})
			result, err := viewLauncherHandler(manager, nil)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: viewLauncher, Arguments: raw}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"read-contract"}}}})
			require.NoError(t, err)
			require.False(t, result.IsError)
			require.NotNil(t, result.StructuredContent)
			info := result.Meta["trade.kite/app"].(map[string]any)
			require.Equal(t, viewSource(view), info["tool"])
			require.Equal(t, map[string]any{"tool": viewLauncher, "arguments": map[string]any{"view": view}}, info["origin"])
			if view == "orders" || view == "gtt" {
				require.Equal(t, "list", info["arguments"].(map[string]interface{})["mode"])
			}
		})
	}
}

func TestKiteViewLauncherRegistrationExclusions(t *testing.T) {
	for _, excluded := range []string{viewLauncher, "portfolio,market,orders,gtt"} {
		t.Run(excluded, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server := NewServer(&sdk.Implementation{Name: "test", Version: "1"})
			RegisterTools(server, nil, excluded, slog.New(slog.NewTextHandler(io.Discard, nil)))
			caps := &sdk.ClientCapabilities{}
			caps.AddExtension(appExtension, map[string]any{"mimeTypes": []string{appMIMEType}})
			client := sdk.NewClient(&sdk.Implementation{Name: "client", Version: "1"}, &sdk.ClientOptions{Capabilities: caps})
			ct, st := sdk.NewInMemoryTransports()
			ss, err := server.Connect(ctx, st, nil)
			require.NoError(t, err)
			defer ss.Close()
			cs, err := client.Connect(ctx, ct, nil)
			require.NoError(t, err)
			defer cs.Close()
			listing, err := cs.ListTools(ctx, nil)
			require.NoError(t, err)
			for _, tool := range listing.Tools {
				require.NotEqual(t, viewLauncher, tool.Name)
			}
		})
	}
}
