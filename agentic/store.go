package agentic

import (
	"sync"
	"time"
)

// Account tracks a per-agent (MCP session) trading budget.
type Account struct {
	BudgetINR   float64   `json:"budget_inr"`
	SpentINR    float64   `json:"spent_inr"`
	RemainingINR float64  `json:"remaining_inr"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store holds in-memory agentic accounts keyed by MCP session ID.
type Store struct {
	mu       sync.RWMutex
	accounts map[string]*accountState
}

type accountState struct {
	budgetINR float64
	spentINR  float64
	createdAt time.Time
}

// NewStore creates an empty agentic account store.
func NewStore() *Store {
	return &Store{accounts: make(map[string]*accountState)}
}

// Setup creates or replaces the budget for a session.
func (s *Store) Setup(sessionID string, budgetINR float64) Account {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.accounts[sessionID] = &accountState{
		budgetINR: budgetINR,
		spentINR:  0,
		createdAt: time.Now(),
	}
	return s.snapshot(sessionID, s.accounts[sessionID])
}

// Get returns the account for a session, or inactive if none exists.
func (s *Store) Get(sessionID string) Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.accounts[sessionID]
	if !ok {
		return Account{Active: false}
	}
	return s.snapshot(sessionID, state)
}

// CanSpend reports whether estimated notional fits the remaining budget.
func (s *Store) CanSpend(sessionID string, estimatedINR float64) (bool, Account, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.accounts[sessionID]
	if !ok {
		return true, Account{Active: false}, ""
	}

	remaining := state.budgetINR - state.spentINR
	if estimatedINR > remaining {
		acct := s.snapshot(sessionID, state)
		return false, acct, "order exceeds agent budget"
	}
	return true, s.snapshot(sessionID, state), ""
}

// RecordSpend adds to spent after a successful order.
func (s *Store) RecordSpend(sessionID string, amountINR float64) Account {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.accounts[sessionID]
	if !ok {
		return Account{Active: false}
	}
	state.spentINR += amountINR
	return s.snapshot(sessionID, state)
}

// Remove clears the agentic account for a session.
func (s *Store) Remove(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accounts, sessionID)
}

func (s *Store) snapshot(_ string, state *accountState) Account {
	remaining := state.budgetINR - state.spentINR
	if remaining < 0 {
		remaining = 0
	}
	return Account{
		BudgetINR:    state.budgetINR,
		SpentINR:     state.spentINR,
		RemainingINR: remaining,
		Active:       true,
		CreatedAt:    state.createdAt,
	}
}
