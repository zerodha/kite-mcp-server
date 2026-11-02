package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const orderApprovalHelpURL = "https://mcp.kite.trade/reference/order-approvals/"

func approvalClientGuidance() string {
	return "Use browser review automatically when available (approval_method=browser); do not ask the user to request a link. If the connected tool lacks browser approval, refresh its tool metadata or use a client with order approval forms, such as ChatGPT web. Client guidance: " + orderApprovalHelpURL
}

// A transport failure or malformed response means the form was unavailable.
// Explicit user decisions and cancellation/expiry must never start a new review.
func nativeApprovalUnavailable(result *mcp.ElicitResult, err error) bool {
	if err != nil {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}
	if result == nil {
		return true
	}
	switch result.Action {
	case "decline", "cancel":
		return false
	case "accept":
		_, valid := result.Content["confirm"].(bool)
		return !valid
	default:
		return true
	}
}

// Format the same immutable snapshot that is checked before execution. The
// presentation never replaces the full snapshot used for approval integrity.
func tradeApprovalForm(operation string, snapshot []byte) *mcp.ElicitParams {
	title, confirm := "Review transaction", "Confirm this transaction"
	switch operation {
	case "orders_place":
		title, confirm = "Place order", "Place this order"
	case "orders_modify":
		title, confirm = "Modify order", "Apply these changes"
	case "orders_cancel":
		title, confirm = "Cancel order", "Cancel this order"
	}
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	decoder.UseNumber()
	var body strings.Builder
	body.WriteString(title + "\n\n")
	if err := decoder.Decode(&fields); err != nil {
		body.Write(snapshot)
	} else {
		writeApprovalFields(&body, fields, "")
	}
	for _, note := range tradeApprovalNotes(operation, fields) {
		body.WriteString("\n" + note + "\n")
	}
	body.WriteString("\nApproval allows one submission of this transaction.")
	schema, _ := json.Marshal(map[string]any{
		"type": "object", "properties": map[string]any{"confirm": map[string]any{
			"type": "boolean", "title": confirm,
		}}, "required": []string{"confirm"}, "additionalProperties": false,
	})
	return &mcp.ElicitParams{Mode: "form", Message: body.String(), RequestedSchema: json.RawMessage(schema)}
}

// Keep review notes consistent across native forms and browser pages. Only
// fixed server text belongs here; caller-supplied values remain in the rows.
func tradeApprovalNotes(operation string, fields map[string]any) []string {
	var notes []string
	switch operation {
	case "orders_place":
		notes = append(notes, "Unspecified fields use Kite defaults.")
	case "orders_modify":
		notes = append(notes, "Only the listed changes will be sent. Other fields stay unchanged.")
	case "orders_cancel":
		notes = append(notes, "Cancellation does not reverse any fills.")
	}
	params, _ := fields["parameters"].(map[string]any)
	if changes, ok := fields["changes"].(map[string]any); ok {
		params = changes
	}
	if params["order_type"] == "MARKET" || params["order_type"] == "SL-M" {
		notes = append(notes, "Execution price is set by the market.")
	}
	return notes
}

func writeApprovalFields(body *strings.Builder, fields map[string]any, indent string) {
	order := []string{"order_id", "exchange", "tradingsymbol", "transaction_type", "quantity", "order_type", "product", "price", "trigger_price", "validity", "validity_ttl", "variety", "current_order", "parameters", "changes"}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	slices.SortStableFunc(keys, func(a, b string) int {
		x, y := slices.Index(order, a), slices.Index(order, b)
		if x < 0 {
			x = len(order)
		}
		if y < 0 {
			y = len(order)
		}
		return x - y
	})
	labels := map[string]string{"order_id": "Order ID", "tradingsymbol": "Symbol", "transaction_type": "Side", "order_type": "Order type", "validity_ttl": "Validity TTL (minutes)", "parameters": "Order details", "changes": "Requested changes", "current_order": "Current order"}
	for _, key := range keys {
		label := labels[key]
		if label == "" {
			label = strings.ReplaceAll(key, "_", " ")
			if label != "" {
				label = strings.ToUpper(label[:1]) + label[1:]
			}
		}
		value := fields[key]
		if nested, ok := value.(map[string]any); ok {
			if key == "current_order" {
				// Omit API bookkeeping from the display, while retaining identity,
				// order terms and fill state. Integrity still covers the entire entry.
				selected := make(map[string]any)
				for _, field := range []string{"order_id", "exchange", "tradingsymbol", "transaction_type", "quantity", "order_type", "product", "price", "trigger_price", "validity", "validity_ttl", "variety", "status", "filled_quantity", "pending_quantity", "cancelled_quantity", "average_price", "disclosed_quantity", "market_protection"} {
					if v, exists := nested[field]; exists {
						if text, ok := v.(string); ok && text == "" {
							continue
						}
						selected[field] = v
					}
				}
				nested = selected
			}
			fmt.Fprintf(body, "%s%s:\n", indent, label)
			writeApprovalFields(body, nested, indent+"  ")
			continue
		}
		text := fmt.Sprint(value)
		if s, ok := value.(string); ok {
			text = s
			if strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) }) {
				text = strconv.Quote(s)
			}
		} else if _, ok := value.([]any); ok {
			encoded, _ := json.Marshal(value)
			text = string(encoded)
		}
		fmt.Fprintf(body, "%s%s: %s\n", indent, label, text)
	}
}

func validateTradeApproval(result *mcp.ElicitResult) *mcp.CallToolResult {
	if result == nil {
		return approvalFailure("missing_response", "No approval response was received; no transaction was executed. "+approvalClientGuidance())
	}
	switch result.Action {
	case "decline":
		return approvalFailure("not_accepted", "Order approval was declined; no transaction was executed. Stop this request. Do not retry or start browser review unless the user requests it.")
	case "cancel":
		return approvalFailure("not_accepted", "Order approval was cancelled; no transaction was executed. Stop this request. Do not retry or start browser review unless the user requests it.")
	case "accept":
	default:
		return approvalFailure("not_accepted", "The client did not return a valid approval response; no transaction was executed. "+approvalClientGuidance())
	}
	confirmed, ok := result.Content["confirm"].(bool)
	if !ok {
		return approvalFailure("not_confirmed", "The client did not return an explicit confirmation; no transaction was executed. "+approvalClientGuidance())
	}
	if !confirmed {
		return approvalFailure("not_confirmed", "The order was not confirmed; no transaction was executed.")
	}
	return nil
}
