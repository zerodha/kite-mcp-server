package mcp

import (
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

// estimateOrderNotionalINR estimates order value in INR for budget checks.
func estimateOrderNotionalINR(client *kiteconnect.Client, exchange, tradingsymbol, orderType string, quantity int, price float64) (float64, error) {
	unitPrice := price

	if orderType == "MARKET" || orderType == "SL-M" || unitPrice <= 0 {
		instrument := fmt.Sprintf("%s:%s", exchange, tradingsymbol)
		ltp, err := client.GetLTP(instrument)
		if err != nil {
			return 0, fmt.Errorf("failed to fetch LTP for budget check: %w", err)
		}
		quote, ok := ltp[instrument]
		if !ok || quote.LastPrice <= 0 {
			return 0, fmt.Errorf("no LTP available for %s", instrument)
		}
		unitPrice = quote.LastPrice
	}

	return unitPrice * float64(quantity), nil
}

func checkAgenticBudget(manager *kc.Manager, sessionID string, session *kc.KiteSessionData, exchange, tradingsymbol, orderType string, quantity int, price float64) (*gomcp.CallToolResult, float64, bool) {
	if manager.Agentic == nil {
		return nil, 0, true
	}

	estimated, err := estimateOrderNotionalINR(session.Kite.Client, exchange, tradingsymbol, orderType, quantity, price)
	if err != nil {
		return gomcp.NewToolResultError(err.Error()), 0, false
	}

	ok, account, reason := manager.Agentic.CanSpend(sessionID, estimated)
	if !ok {
		return gomcp.NewToolResultText(fmt.Sprintf(
			"Order blocked: %s. Agent budget: ₹%.2f, spent: ₹%.2f, remaining: ₹%.2f, estimated order: ₹%.2f. Use get_agent_account for details or disconnect_agent to revoke access.",
			reason, account.BudgetINR, account.SpentINR, account.RemainingINR, estimated,
		)), estimated, false
	}

	return nil, estimated, true
}
