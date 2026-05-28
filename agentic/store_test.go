package agentic

import (
	"sync"
	"testing"
)

func TestStoreBudgetLifecycle(t *testing.T) {
	store := NewStore()
	sessionID := "sess-1"

	acct := store.Setup(sessionID, 10000)
	if !acct.Active || acct.BudgetINR != 10000 || acct.RemainingINR != 10000 {
		t.Fatalf("unexpected setup account: %+v", acct)
	}

	ok, _, reason := store.CanSpend(sessionID, 6000)
	if !ok || reason != "" {
		t.Fatalf("expected spend allowed, got ok=%v reason=%q", ok, reason)
	}

	store.RecordSpend(sessionID, 6000)

	ok, acct, reason = store.CanSpend(sessionID, 5000)
	if ok || reason == "" {
		t.Fatalf("expected spend blocked, got ok=%v reason=%q acct=%+v", ok, reason, acct)
	}
	if acct.RemainingINR != 4000 {
		t.Fatalf("expected 4000 remaining, got %v", acct.RemainingINR)
	}

	store.Remove(sessionID)
	acct = store.Get(sessionID)
	if acct.Active {
		t.Fatal("expected inactive account after remove")
	}
}

func TestStoreInactiveSessionAllowsSpend(t *testing.T) {
	store := NewStore()
	ok, _, reason := store.CanSpend("unknown", 999999)
	if !ok || reason != "" {
		t.Fatalf("inactive session should not block: ok=%v reason=%q", ok, reason)
	}
}

func TestStoreSetupReplacesExistingBudget(t *testing.T) {
	store := NewStore()
	sessionID := "sess-2"

	store.Setup(sessionID, 5000)
	store.RecordSpend(sessionID, 4000)

	acct := store.Setup(sessionID, 20000)
	if acct.BudgetINR != 20000 || acct.SpentINR != 0 || acct.RemainingINR != 20000 {
		t.Fatalf("setup should reset spent: %+v", acct)
	}
}

func TestStoreRecordSpendAccumulates(t *testing.T) {
	store := NewStore()
	sessionID := "sess-3"
	store.Setup(sessionID, 1000)

	store.RecordSpend(sessionID, 200)
	store.RecordSpend(sessionID, 300)

	acct := store.Get(sessionID)
	if acct.SpentINR != 500 || acct.RemainingINR != 500 {
		t.Fatalf("unexpected totals: %+v", acct)
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	store := NewStore()
	sessionID := "sess-4"
	store.Setup(sessionID, 100000)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.CanSpend(sessionID, 10)
			store.RecordSpend(sessionID, 1)
			store.Get(sessionID)
		}()
	}
	wg.Wait()

	acct := store.Get(sessionID)
	if !acct.Active || acct.SpentINR <= 0 {
		t.Fatalf("expected active account with spend recorded: %+v", acct)
	}
}
