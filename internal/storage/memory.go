package storage

import "iter"

// NewMemory creates an empty collection. The owner supplies synchronization.
func NewMemory[K comparable, V any]() Records[K, V] {
	return &memoryRecords[K, V]{values: make(map[K]V)}
}

type memoryRecords[K comparable, V any] struct {
	values map[K]V
}

func (m *memoryRecords[K, V]) Get(key K) V { return m.values[key] }

func (m *memoryRecords[K, V]) Lookup(key K) (V, bool) {
	value, ok := m.values[key]
	return value, ok
}

func (m *memoryRecords[K, V]) Set(key K, value V) { m.values[key] = value }

func (m *memoryRecords[K, V]) Delete(key K) { delete(m.values, key) }

func (m *memoryRecords[K, V]) Len() int { return len(m.values) }

func (m *memoryRecords[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for key, value := range m.values {
			if !yield(key, value) {
				return
			}
		}
	}
}
