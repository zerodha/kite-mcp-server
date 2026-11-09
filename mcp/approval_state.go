package mcp

import (
	"encoding/json"
	"errors"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/internal/storage"
	"github.com/zerodha/kite-mcp-server/kc"
)

type storedBrowserOrder struct {
	ID, Session, User, Client, ClientName, Operation string
	Snapshot, Arguments                              []byte
	Expires, RetainUntil                             time.Time
	Status                                           string
	Result                                           *sdk.CallToolResult
	Trace                                            approvalTrace
	RememberRequested                                bool
}

type storedOrderPermission struct {
	Client, User, ClientName string
	Expires                  time.Time
	Allowed                  bool
	Generation               uint64
}

func (b *BrowserApprovals) persistLocked(orders []*browserOrder, sessions ...string) error {
	if b.state == nil {
		return nil
	}
	changes := make([]storage.Change, 0, len(orders)+len(sessions))
	for _, p := range orders {
		value := storedBrowserOrder{p.id, p.session, p.user, p.client, p.clientName, p.operation, p.snapshot, p.arguments, p.expires, p.retainUntil, p.status, p.result, p.trace, p.rememberRequested}
		change, err := storage.Encode("browser_orders", p.id, value)
		if err != nil {
			return err
		}
		changes = append(changes, change)
	}
	for _, session := range sessions {
		p, ok := b.permissions.Lookup(session)
		if !ok {
			changes = append(changes, storage.Change{Bucket: "order_permissions", Key: session})
			continue
		}
		change, err := storage.Encode("order_permissions", session, storedOrderPermission{p.client, p.user, p.clientName, p.expires, p.allowed, p.generation})
		if err != nil {
			return err
		}
		changes = append(changes, change)
	}
	return b.state.Apply(changes...)
}

// RestoreState runs before serving requests. A previously claimed submission
// has an uncertain outcome after restart and must never be offered for reuse.
func (b *BrowserApprovals) RestoreState() error {
	if b.state != nil {
		orders, err := b.state.Load("browser_orders")
		if err != nil {
			return err
		}
		for id, data := range orders {
			var p storedBrowserOrder
			if json.Unmarshal(data, &p) != nil || p.ID != id || len(id) != 64 || p.Expires.IsZero() {
				return errors.New("invalid stored browser approval")
			}
			switch p.Status {
			case "approval_required", "approved_for_later", "submitting", "submitted", "failed", "declined", "expired":
			default:
				return errors.New("invalid stored approval status")
			}
			value := &browserOrder{id: id, session: p.Session, user: p.User, client: p.Client, clientName: p.ClientName, operation: p.Operation, snapshot: p.Snapshot, arguments: p.Arguments, expires: p.Expires, retainUntil: p.RetainUntil, status: p.Status, result: p.Result, trace: p.Trace, rememberRequested: p.RememberRequested}
			if value.status == "submitting" {
				value.status = "failed"
				value.result = NewToolResultError("Submission was interrupted. Check orders and trades before requesting another submission.")
				value.retainUntil = time.Now().Add(15 * time.Minute)
				if err := b.persistLocked([]*browserOrder{value}); err != nil {
					return err
				}
			}
			b.pending.Set(id, value)
		}
		permissions, err := b.state.Load("order_permissions")
		if err != nil {
			return err
		}
		for session, data := range permissions {
			var p storedOrderPermission
			if json.Unmarshal(data, &p) != nil || p.Expires.IsZero() {
				return errors.New("invalid stored order permission")
			}
			b.permissions.Set(session, clientOrderPermission{p.Client, p.User, p.ClientName, p.Expires, p.Allowed, p.Generation})
		}
		if err := b.cleanupLocked(time.Now()); err != nil {
			return err
		}
	}
	return b.manager.SessionManager().RegisterSessionData("order_review", func(id string, data json.RawMessage) (any, error) {
		var p reviewLoginState
		if json.Unmarshal(data, &p) != nil || p.Session == "" || p.Secret == "" || p.Expires.IsZero() {
			return nil, errors.New("invalid stored order review login")
		}
		return &reviewLogin{broker: b, id: id, target: p.Target, session: p.Session, user: p.User, state: p.State, secret: p.Secret, previous: p.Previous.Restore(), expires: p.Expires, consumed: p.Consumed, verified: p.Verified}, nil
	})
}

type reviewLoginState struct {
	Target, Session, User, State, Secret string
	Previous                             kc.CredentialSnapshot
	Expires                              time.Time
	Consumed, Verified                   bool
}

func (f *reviewLogin) SessionData() (string, any) {
	return "order_review", reviewLoginState{f.target, f.session, f.user, f.state, f.secret, kc.SnapshotCredentials(f.previous), f.expires, f.consumed, f.verified}
}
