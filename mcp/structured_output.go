package mcp

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Upstream account objects vary by mode and may gain fields independently of
// this server. Keep those fields open rather than promising a fixed SDK snapshot.
var accountOutputSchema = json.RawMessage(`{
	"type":"object",
	"description":"Mode-specific account object. Bare upstream lists or null are wrapped in data; existing object fields are preserved.",
	"properties":{
		"data":{"type":["array","null"],"items":{"type":"object"}},
		"pagination":{"type":"object"}
	}
}`)

var ordersOutputSchema = json.RawMessage(`{
	"type":"object",
	"description":"Paginated orders, wrapped history/fill lists, an order submission result, browser approval state, or client permissions. Approval state is not evidence of submission.",
	"properties":{
		"data":{"type":["array","null"],"items":{"type":"object"}},
		"pagination":{"type":"object"},
		"order_id":{"type":"string"},
		"status":{"type":"string"},
		"approval_method_used":{"type":"string","enum":["none","native","browser","remembered_permission"]},
		"approval_outcome":{"type":"string"},
		"fallback_reason":{"type":"string"},
		"native_elicitation_attempted":{"type":"boolean"},
		"native_elicitation_completed":{"type":"boolean"},
		"approval_id":{"type":"string"},
		"approval_url":{"type":"string"},
		"expires_at":{"type":"string"},
		"message":{"type":"string"},
		"result":{"type":["object","null"]},
		"future_orders_allowed":{"type":"boolean"},
		"permissions_url":{"type":"string"}
	}
}`)

var gttOutputSchema = json.RawMessage(`{
	"type":"object",
	"description":"Paginated GTT list or mutation result with trigger_id.",
	"properties":{
		"data":{"type":["array","null"],"items":{"type":"object"}},
		"pagination":{"type":"object"},
		"trigger_id":{"type":"integer"}
	}
}`)

var alertsOutputSchema = json.RawMessage(`{
	"type":"object",
	"properties":{
		"alert":{"type":"object"},
		"alerts":{"type":["array","null"],"items":{"type":"object"}},
		"history":{"type":["array","null"],"items":{"type":"object"}},
		"histories":{"type":"object"},
		"success":{"type":"boolean"},
		"deleted_uuids":{"type":"array","items":{"type":"string"}},
		"deleted_count":{"type":"integer"}
	},
	"anyOf":[{"required":["alert"]},{"required":["alerts"]},{"required":["success","deleted_uuids","deleted_count"]}]
}`)

var mutualFundsOutputSchema = json.RawMessage(`{
	"type":"object","required":["data"],
	"properties":{"data":{"type":["array","null"],"items":{"type":"object"}}}
}`)

var sessionOutputSchema = json.RawMessage(`{
	"type":"object","required":["session_id","status"],
	"properties":{
		"session_id":{"type":"string"},"status":{"type":"string"},
		"authenticated":{"type":"boolean"},"terminated":{"type":"boolean"},
		"created_at":{"type":"string"},"expires_at":{"type":"string"},
		"kite_session_expires_at":{"type":"string"},"kite_session_expired":{"type":"boolean"},
		"refresh_available":{"type":"boolean"},"success":{"type":"boolean"}
	}
}`)

var helpOutputSchema = json.RawMessage(`{
	"type":"object","required":["key","text"],
	"properties":{"key":{"type":"string"},"text":{"type":"string"}}
}`)

// newDataTool advertises an object root for compatibility with older MCP
// protocols. Text-only tools can continue to use NewTool without an output schema.
func newDataTool(name, description string, input, output json.RawMessage) *mcp.Tool {
	tool := NewTool(name, description, input)
	tool.OutputSchema = output
	return tool
}

// structuredJSON uses the already-marshaled bytes so custom JSON encoders,
// omitted fields and nested SDK types match the text representation exactly.
func structuredJSON(encoded []byte) any {
	var object map[string]json.RawMessage
	if json.Unmarshal(encoded, &object) == nil && object != nil {
		return json.RawMessage(encoded)
	}
	return map[string]json.RawMessage{"data": encoded}
}
