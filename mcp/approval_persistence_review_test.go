package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/internal/audit/audittest"
	"github.com/zerodha/kite-mcp-server/internal/storage"
)

// Reject the transaction that saves an accepted broker outcome and its broad
// permission. The earlier single-use claim must remain the committed state.
type terminalResultFailureState struct {
	mu               sync.Mutex
	failed           error
	committedOrder   json.RawMessage
	permissionWrites int
}

func (s *terminalResultFailureState) Load(string) (map[string]json.RawMessage, error) {
	return nil, s.Check()
}

func (s *terminalResultFailureState) Apply(changes ...storage.Change) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed != nil {
		return s.failed
	}
	for _, change := range changes {
		if change.Bucket == "browser_orders" {
			var value storedBrowserOrder
			if err := json.Unmarshal(change.Value, &value); err != nil {
				return err
			}
			if value.Status == "submitted" {
				s.failed = errors.New("injected terminal transaction failure")
				return s.failed
			}
		}
	}
	for _, change := range changes {
		switch change.Bucket {
		case "browser_orders":
			s.committedOrder = append(json.RawMessage(nil), change.Value...)
		case "order_permissions":
			s.permissionWrites++
		}
	}
	return nil
}

func (s *terminalResultFailureState) Check() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

func (*terminalResultFailureState) Close() error { return nil }

func TestBrowserAcceptedOutcomeIsAuditedWhenResultPersistenceFails(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	var logs audittest.Recorder
	f.manager.Logger = logs.Logger()
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	state := &terminalResultFailureState{}
	f.b.state = state
	form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}

	response := postBrowserDecision(f.b, id, identity, form, "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), "Check orders and trades")
	require.EqualValues(t, 1, f.calls.Load())
	ends := logs.Events("order_submission_result")
	require.Len(t, ends, 1)
	require.Equal(t, "accepted", ends[0]["outcome"])

	events := logs.Events("browser_approval_result")
	require.Len(t, events, 1)
	require.Equal(t, audit.Ref(id), events[0]["approval_ref"])
	require.Equal(t, "submitted", events[0]["status"])
	require.Equal(t, "approved", events[0]["approval_outcome"])
	require.Equal(t, true, events[0]["submission_attempted"])
	require.Equal(t, "accepted", events[0]["submission_outcome"])
	require.Equal(t, false, events[0]["result_persisted"])
	require.Empty(t, logs.Events("order_permission_granted"))
	require.Error(t, state.Check())
	var committed storedBrowserOrder
	require.NoError(t, json.Unmarshal(state.committedOrder, &committed))
	require.Equal(t, "submitting", committed.Status)
	require.Zero(t, state.permissionWrites)

	// Once persistence fails, replaying the form cannot submit again or turn
	// the uncommitted broad permission into a successful authorization.
	require.Equal(t, http.StatusServiceUnavailable, postBrowserDecision(f.b, id, identity, form, "").Code)
	require.EqualValues(t, 1, f.calls.Load())
	require.Len(t, logs.Events("browser_approval_result"), 1)
	require.Empty(t, logs.Events("order_permission_granted"))
}
