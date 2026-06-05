package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderAndTradeToolsDescribeCurrentDayScope(t *testing.T) {
	tests := []struct {
		name string
		tool Tool
		want []string
	}{
		{
			name: "get_trades",
			tool: &TradesTool{},
			want: []string{"current trading day only", "does not return historical trades"},
		},
		{
			name: "get_orders",
			tool: &OrdersTool{},
			want: []string{"current trading day only", "does not return historical orders"},
		},
		{
			name: "get_order_history",
			tool: &OrderHistoryTool{},
			want: []string{"specific current-day order", "does not list historical orders across days"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			description := strings.ToLower(tt.tool.Tool().Description)
			for _, want := range tt.want {
				assert.Contains(t, description, strings.ToLower(want))
			}
		})
	}
}

func TestTradebookAvailabilityToolRegistered(t *testing.T) {
	toolNames := make(map[string]bool)
	for _, tool := range GetAllTools() {
		toolNames[tool.Tool().Name] = true
	}

	assert.True(t, toolNames["get_tradebook"])
}

func TestTradebookAvailabilityToolDescription(t *testing.T) {
	tool := (&TradebookAvailabilityTool{}).Tool()

	assert.Equal(t, "get_tradebook", tool.Name)
	assert.Contains(t, strings.ToLower(tool.Description), "historical stock transaction tradebook")
	assert.Contains(t, strings.ToLower(tool.Description), "only current-day orders/trades")
}
