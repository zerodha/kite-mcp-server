package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

// testClientSession is a minimal server.ClientSession for invoking tool handlers.
type testClientSession struct{ id string }

func (s testClientSession) Initialize()                                         {}
func (s testClientSession) Initialized() bool                                   { return true }
func (s testClientSession) NotificationChannel() chan<- mcp.JSONRPCNotification { return nil }
func (s testClientSession) SessionID() string                                   { return s.id }

func TestModifyGTTOrderRequiresProduct(t *testing.T) {
	def := (&ModifyGTTOrderTool{}).Tool()
	assert.Contains(t, def.InputSchema.Properties, "product")
	assert.Contains(t, def.InputSchema.Required, "product")
}

// TestModifyGTTOrderSendsProduct calls the modify_gtt_order handler against a
// fake Kite API and checks that the product is sent instead of defaulting to CNC.
func TestModifyGTTOrderSendsProduct(t *testing.T) {
	type captured struct {
		method, path string
		form         url.Values
	}
	reqs := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		reqs <- captured{r.Method, r.URL.Path, r.PostForm}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"trigger_id":123}}`))
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instrumentsConfig := instruments.DefaultUpdateConfig()
	instrumentsConfig.EnableScheduler = false
	instrumentsManager, err := instruments.New(instruments.Config{
		UpdateConfig: instrumentsConfig,
		Logger:       logger,
		TestData:     map[uint32]*instruments.Instrument{},
	})
	require.NoError(t, err)

	manager, err := kc.New(kc.Config{
		APIKey:             "test_key",
		APISecret:          "test_secret",
		Logger:             logger,
		InstrumentsManager: instrumentsManager,
	})
	require.NoError(t, err)
	defer manager.Shutdown()

	sessionID := manager.GenerateSession()
	session, err := manager.GetSession(sessionID)
	require.NoError(t, err)
	session.Kite.Client.SetBaseURI(srv.URL)

	ctx := server.NewMCPServer("test", "test").WithContext(context.Background(), testClientSession{id: sessionID})
	var request mcp.CallToolRequest
	request.Params.Arguments = map[string]interface{}{
		"trigger_id":       123.0,
		"exchange":         "NFO",
		"tradingsymbol":    "NIFTY25DECFUT",
		"last_price":       24000.0,
		"transaction_type": "SELL",
		"product":          "NRML",
		"trigger_type":     "single",
		"trigger_value":    23000.0,
		"quantity":         75.0,
		"limit_price":      22990.0,
	}

	result, err := (&ModifyGTTOrderTool{}).Handler(manager)(ctx, request)
	require.NoError(t, err)
	require.False(t, result.IsError, "unexpected tool error: %v", result.Content)

	req := <-reqs
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/gtt/triggers/123", req.path)

	var orders []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(req.form.Get("orders")), &orders))
	require.Len(t, orders, 1)
	assert.Equal(t, "NRML", orders[0]["product"])
}
