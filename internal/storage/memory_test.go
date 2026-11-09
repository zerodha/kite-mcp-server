package storage_test

import (
	"testing"

	"github.com/zerodha/kite-mcp-server/internal/storage"
)

// Session and approval cleanup both delete during iteration and update live
// records through pointers. These are part of the collection contract.
func TestCleanupPreservesLiveRecords(t *testing.T) {
	type record struct {
		expired bool
		visits  int
	}
	records := storage.NewMemory[int, *record]()
	for i := range 20 {
		records.Set(i, &record{expired: i%2 == 0})
	}
	live := records.Get(1)
	for key, value := range records.All() {
		if value.expired {
			records.Delete(key)
		} else {
			value.visits++
		}
	}
	if records.Len() != 10 {
		t.Fatalf("cleanup retained %d records, want 10", records.Len())
	}
	for i := range 20 {
		value, exists := records.Lookup(i)
		if i%2 == 0 {
			if exists || value != nil {
				t.Fatalf("expired record %d retained", i)
			}
		} else if !exists || value.visits != 1 {
			t.Fatalf("live record %d was removed or skipped", i)
		}
	}
	if records.Get(1) != live || live.visits != 1 {
		t.Fatal("cleanup replaced a live record instead of updating it in place")
	}
}

// Saved approval selection stops at the first matching record.
func TestIterationCanStopEarly(t *testing.T) {
	records := storage.NewMemory[int, int]()
	for i := range 20 {
		records.Set(i, i)
	}
	visits := 0
	for range records.All() {
		visits++
		break
	}
	if visits != 1 || records.Len() != 20 {
		t.Fatal("stopping iteration changed the collection")
	}
}
