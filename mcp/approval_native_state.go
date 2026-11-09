package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zerodha/kite-mcp-server/internal/storage"
)

type storedNativeApproval struct {
	Owner, Operation string
	Snapshot         []byte
	Expires          time.Time
}

func (s *tradeApprovals) restoreState() error {
	if s.state == nil {
		return nil
	}
	values, err := s.state.Load("native_approvals")
	if err != nil {
		return err
	}
	for id, data := range values {
		var p storedNativeApproval
		if json.Unmarshal(data, &p) != nil || p.Owner == "" || p.Expires.IsZero() {
			return errors.New("invalid stored native approval")
		}
		if !time.Now().Before(p.Expires) || strings.HasPrefix(p.Owner, "transport:") {
			if err := storage.Delete(s.state, "native_approvals", id); err != nil {
				return err
			}
			continue
		}
		s.pending.Set(id, pendingTradeApproval{p.Owner, p.Operation, p.Snapshot, p.Expires})
	}
	return nil
}
