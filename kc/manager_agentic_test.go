package kc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/agentic"
)

func TestKiteSessionCleanupHookClearsAgenticAccount(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	require.NoError(t, err)

	manager.Agentic = agentic.NewStore()
	sessionID := manager.sessionManager.GenerateWithData(&KiteSessionData{Kite: NewKiteConnect("test_key")})
	manager.Agentic.Setup(sessionID, 7500)

	session, err := manager.sessionManager.GetSession(sessionID)
	require.NoError(t, err)

	manager.kiteSessionCleanupHook(session)

	acct := manager.Agentic.Get(sessionID)
	assert.False(t, acct.Active)
}

func TestClearSessionDataClearsAgenticBudget(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	require.NoError(t, err)

	manager.Agentic = agentic.NewStore()
	sessionID := manager.sessionManager.GenerateWithData(&KiteSessionData{Kite: NewKiteConnect("test_key")})
	manager.Agentic.Setup(sessionID, 3000)

	require.NoError(t, manager.ClearSessionData(sessionID))

	acct := manager.Agentic.Get(sessionID)
	assert.False(t, acct.Active)
}
