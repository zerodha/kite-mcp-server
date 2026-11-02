package mcp

import (
	"math"
	"slices"
)

// The SDK's raw tool handlers do not validate input schemas. In particular,
// fields used in SDK URL paths must never be coerced or contain URL syntax.
func validatePathIdentifier(value any, field string) error {
	s, ok := value.(string)
	if !ok || len(s) == 0 || len(s) > 200 {
		return ValidationError{field, "must be a non-empty identifier of at most 200 bytes"}
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return ValidationError{field, "must contain only letters, digits, hyphens, or underscores"}
		}
	}
	return nil
}

func validateNumericArgument(args map[string]any, field string, minimum, maximum float64, integer bool) error {
	raw, exists := args[field]
	if !exists {
		return nil
	}
	var value float64
	switch v := raw.(type) {
	case float64:
		value = v
	case int:
		value = float64(v)
	default:
		return ValidationError{field, "must be a number in the supported range"}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < minimum || value > maximum || (integer && math.Trunc(value) != value) {
		return ValidationError{field, "must be a number in the supported range"}
	}
	return nil
}

func validateStringChoices(args map[string]any, field string, allowed ...string) error {
	raw, exists := args[field]
	if !exists {
		return nil
	}
	s, ok := raw.(string)
	if !ok || s == "" || (len(allowed) > 0 && !slices.Contains(allowed, s)) {
		return ValidationError{field, "must be a supported non-empty string"}
	}
	return nil
}

func validateOrderArguments(args map[string]any, mode string) error {
	if mode == "history" || mode == "trades" || mode == "modify" || mode == "cancel" {
		if err := validatePathIdentifier(args["order_id"], "order_id"); err != nil {
			return err
		}
	}
	if mode != "place" && mode != "modify" && mode != "cancel" {
		return nil
	}
	for _, spec := range []struct {
		field   string
		choices []string
	}{
		{"variety", []string{"regular", "co", "amo", "iceberg", "auction"}},
		{"approval_method", []string{"auto", "browser"}},
		{"exchange", []string{"NSE", "BSE", "MCX", "NFO", "BFO"}},
		{"transaction_type", []string{"BUY", "SELL"}},
		{"order_type", []string{"MARKET", "LIMIT", "SL", "SL-M"}},
		{"product", []string{"CNC", "NRML", "MIS", "MTF"}},
		{"validity", []string{"DAY", "IOC", "TTL"}},
		{"tradingsymbol", nil},
	} {
		if err := validateStringChoices(args, spec.field, spec.choices...); err != nil {
			return err
		}
	}
	for _, field := range []string{"price", "trigger_price"} {
		if err := validateNumericArgument(args, field, 0, math.MaxFloat64, false); err != nil {
			return err
		}
	}
	if err := validateNumericArgument(args, "market_protection", -1, 100, false); err != nil {
		return err
	}
	if value := SafeAssertFloat64(args["market_protection"], 0); value < 0 && value != -1 {
		return ValidationError{"market_protection", "must be -1 for automatic protection, 0 to disable it, or a positive percentage up to 100"}
	}
	if value, exists := args["tradingsymbol"]; exists && len(value.(string)) > 200 {
		return ValidationError{"tradingsymbol", "must be at most 200 bytes"}
	}
	if raw, exists := args["tag"]; exists {
		if tag, ok := raw.(string); !ok || len(tag) > 20 {
			return ValidationError{"tag", "must be a string of at most 20 bytes"}
		}
	}
	if raw, exists := args["autoslice"]; exists {
		if _, ok := raw.(bool); !ok {
			return ValidationError{"autoslice", "must be a boolean"}
		}
	}
	return nil
}
