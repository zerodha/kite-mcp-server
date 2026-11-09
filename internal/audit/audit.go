// Package audit emits structured security and trading lifecycle records.
// It does not provide durable storage; the deployment must collect stderr.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type loggerKey struct{}

type Actor struct{ UserID, SessionID, ClientID string }

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func Logger(ctx context.Context) *slog.Logger {
	logger, _ := ctx.Value(loggerKey{}).(*slog.Logger)
	return logger
}

// Ref correlates server-held identifiers without exposing bearer-like state.
func Ref(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Digest identifies the exact reviewed snapshot without logging arbitrary
// arguments, free-text tags, credentials, or returned account data.
func Digest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return Ref(string(encoded))
}

// Record accepts only server-selected fields. Never pass raw requests, errors,
// response bodies, URLs, credentials, or caller-supplied field names here.
func Record(logger *slog.Logger, event string, actor Actor, fields ...any) {
	if logger == nil {
		return
	}
	attrs := []any{"audit", true, "audit_version", 1, "event", event, "event_id", uuid.NewString(), "occurred_at", time.Now().UTC().Format(time.RFC3339Nano)}
	if actor.UserID != "" {
		attrs = append(attrs, "user_id", actor.UserID)
	}
	if actor.SessionID != "" {
		attrs = append(attrs, "session_ref", Ref(actor.SessionID))
	}
	if actor.ClientID != "" {
		attrs = append(attrs, "client_ref", Ref(actor.ClientID))
	}
	attrs = append(attrs, fields...)
	logger.Info("Audit event", attrs...)
}
