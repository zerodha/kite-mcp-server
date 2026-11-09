package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/audit"
)

func TestConfiguredLoggerSupportsStructuredAuditJSON(t *testing.T) {
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("LOG_LEVEL", "info")
	var output bytes.Buffer
	audit.Record(configuredLogger(&output), "user_login_success", audit.Actor{UserID: "AB1234"}, "flow", "oauth")
	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	require.Equal(t, true, record["audit"])
	require.Equal(t, "user_login_success", record["event"])
	require.Equal(t, "AB1234", record["user_id"])
}

func TestConfiguredLoggerPreservesTextDefault(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("LOG_LEVEL", "info")
	var output bytes.Buffer
	configuredLogger(&output).Info("Ready", "version", "test")
	require.Contains(t, output.String(), "version=test")
	require.False(t, strings.HasPrefix(output.String(), "{"))
}
