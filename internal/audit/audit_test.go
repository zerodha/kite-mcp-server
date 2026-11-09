package audit_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/internal/audit/audittest"
)

func TestRecordUsesStableEventsAndSafeReferences(t *testing.T) {
	var logs audittest.Recorder
	audit.Record(logs.Logger(), "user_login_success", audit.Actor{UserID: "AB1234", SessionID: "private-grant", ClientID: "private-client"}, "flow", "oauth")
	records := logs.Events("user_login_success")
	require.Len(t, records, 1)
	record := records[0]
	require.Equal(t, float64(1), record["audit_version"])
	require.Equal(t, "AB1234", record["user_id"])
	require.Equal(t, audit.Ref("private-grant"), record["session_ref"])
	require.Equal(t, audit.Ref("private-client"), record["client_ref"])
	require.NotEmpty(t, record["event_id"])
	_, err := time.Parse(time.RFC3339Nano, record["occurred_at"].(string))
	require.NoError(t, err)
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-grant")
	require.NotContains(t, string(encoded), "private-client")
	require.Equal(t, audit.Digest(map[string]any{"quantity": "1"}), audit.Digest(json.RawMessage("{\n  \"quantity\": \"1\"\n}")))
}
