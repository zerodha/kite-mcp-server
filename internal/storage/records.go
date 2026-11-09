// Package storage provides typed collections for process-local application state.
package storage

import "iter"

// Records preserves the operations used by the existing in-memory stores.
// Synchronization belongs to the owning manager: callers must hold its lock
// across an entire read/check/update sequence, including iteration. Keeping that
// lock outside the collection also protects related collections and mutations
// through stored pointers.
//
// Values retain their identity, including pointers and slices. All has the same
// iteration semantics as a Go map: order is unspecified and deleting entries
// during iteration is supported. An absent key returns the zero value.
//
// This is an in-process collection boundary, not a durable transaction API.
// External backends will need explicit error handling and atomic domain
// operations before replacing these collections.
type Records[K comparable, V any] interface {
	Get(K) V
	Lookup(K) (V, bool)
	Set(K, V)
	Delete(K)
	Len() int
	All() iter.Seq2[K, V]
}
