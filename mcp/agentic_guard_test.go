package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/agentic"
	"github.com/zerodha/kite-mcp-server/kc"
)

func newTestKiteClientWithLTP(t *testing.T, instrument string, lastPrice float64) *kiteconnect.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quote" {
			http.NotFound(w, r)
			return
		}
		payload := map[string]any{
			"status": "success",
			"data": map[string]any{
				instrument: map[string]any{
					"instrument_token": 1,
					"last_price":       lastPrice,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)

	client := kiteconnect.New("test_api_key")
	client.SetBaseURI(srv.URL)
	return client
}

func newAgenticTestManager(t *testing.T, withBudget bool, budgetINR float64, sessionID string) (*kc.Manager, *kc.KiteSessionData) {
	t.Helper()

	manager, err := kc.New(kc.Config{
		APIKey:    "test_key",
		APISecret: "test_secret",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)

	manager.Agentic = agentic.NewStore()
	if withBudget {
		manager.Agentic.Setup(sessionID, budgetINR)
	}

	session := &kc.KiteSessionData{
		Kite: &kc.KiteConnect{Client: kiteconnect.New("test_api_key")},
	}
	return manager, session
}

func TestEstimateOrderNotionalINR_LimitOrderUsesPrice(t *testing.T) {
	client := kiteconnect.New("test_key")

	notional, err := estimateOrderNotionalINR(client, "NSE", "RELIANCE", "LIMIT", 2, 2500.50)
	require.NoError(t, err)
	assert.InDelta(t, 5001.0, notional, 0.01)
}

func TestEstimateOrderNotionalINR_MarketOrderUsesLTP(t *testing.T) {
	client := newTestKiteClientWithLTP(t, "NSE:RELIANCE", 1234.5)

	notional, err := estimateOrderNotionalINR(client, "NSE", "RELIANCE", "MARKET", 3, 0)
	require.NoError(t, err)
	assert.InDelta(t, 3703.5, notional, 0.01)
}

func TestCheckAgenticBudget_DisabledStoreAllowsAll(t *testing.T) {
	manager, session := newAgenticTestManager(t, false, 0, "sess-a")
	manager.Agentic = nil

	result, estimated, ok := checkAgenticBudget(manager, "sess-a", session, "NSE", "RELIANCE", "LIMIT", 1, 100)
	assert.Nil(t, result)
	assert.Equal(t, 0.0, estimated)
	assert.True(t, ok)
}

func TestCheckAgenticBudget_NoAccountConfiguredAllowsOrder(t *testing.T) {
	manager, session := newAgenticTestManager(t, false, 0, "sess-b")

	result, estimated, ok := checkAgenticBudget(manager, "sess-b", session, "NSE", "RELIANCE", "LIMIT", 1, 100)
	assert.Nil(t, result)
	assert.InDelta(t, 100.0, estimated, 0.01)
	assert.True(t, ok)
}

func TestCheckAgenticBudget_WithinBudget(t *testing.T) {
	const sessionID = "sess-c"
	manager, session := newAgenticTestManager(t, true, 10000, sessionID)

	result, estimated, ok := checkAgenticBudget(manager, sessionID, session, "NSE", "RELIANCE", "LIMIT", 2, 1000)
	assert.Nil(t, result)
	assert.InDelta(t, 2000.0, estimated, 0.01)
	assert.True(t, ok)
}

func TestCheckAgenticBudget_ExceedsBudget(t *testing.T) {
	const sessionID = "sess-d"
	manager, session := newAgenticTestManager(t, true, 1000, sessionID)

	result, estimated, ok := checkAgenticBudget(manager, sessionID, session, "NSE", "RELIANCE", "LIMIT", 5, 500)
	require.NotNil(t, result)
	assert.False(t, ok)
	assert.InDelta(t, 2500.0, estimated, 0.01)
	text, ok := gomcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	assert.Contains(t, text.Text, "Order blocked")
}

func TestCheckAgenticBudget_MarketOrderBlockedWhenOverBudget(t *testing.T) {
	const sessionID = "sess-e"
	manager, session := newAgenticTestManager(t, true, 500, sessionID)
	session.Kite.Client = newTestKiteClientWithLTP(t, "NSE:RELIANCE", 1000)

	result, _, ok := checkAgenticBudget(manager, sessionID, session, "NSE", "RELIANCE", "MARKET", 1, 0)
	require.NotNil(t, result)
	assert.False(t, ok)
}
