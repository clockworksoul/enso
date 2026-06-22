// Package memstore provides an in-memory implementation of core.Store. It is
// intended for:
//
//  1. Unit tests that need a Store without filesystem I/O.
//  2. Early integration tests and ephemeral workloads where durability is not
//     required (e.g. bulk-query, ephemeral sessions, or snapshot recall).
//
// MemStore satisfies the same INV-2 (append-only) contract as FSStore: Append
// only adds records; it never overwrites or deletes. Like FSStore, it validates
// all inputs before mutating state.
package memstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/clockworksoul/enso/internal/core"
)

// MemStore is a thread-safe, in-memory implementation of core.Store.
// The zero value is not usable; use New.
type MemStore struct {
	mu      sync.RWMutex
	entries []core.Entry
	edges   []core.Edge
}

// New returns an empty, ready-to-use MemStore.
func New() *MemStore {
	return &MemStore{}
}

// Append adds entries and edges to the in-memory corpus without removing any
// prior records (INV-2: append-only). All entries and edges are validated
// before any mutation; a validation failure leaves the store unchanged.
func (s *MemStore) Append(_ context.Context, entries []core.Entry, edges []core.Edge) error {
	// Validate everything up front (loud, before any mutation) so a bad batch
	// doesn't partially land — same discipline as FSStore.
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("memstore: refusing to append invalid entry %q: %w", e.ID, err)
		}
	}
	for _, ed := range edges {
		if err := ed.Validate(); err != nil {
			return fmt.Errorf("memstore: refusing to append invalid edge from %q: %w", ed.From, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entries...)
	s.edges = append(s.edges, edges...)
	return nil
}

// Load returns copies of all stored entries and edges. The returned slices are
// independent of the store's internal state and safe to mutate.
//
// Load returns nil slices (not empty slices) when the corpus is empty, matching
// FSStore's behavior and making "empty corpus" checks simple.
func (s *MemStore) Load(_ context.Context) ([]core.Entry, []core.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.entries) == 0 && len(s.edges) == 0 {
		return nil, nil, nil
	}
	entries := make([]core.Entry, len(s.entries))
	copy(entries, s.entries)
	edges := make([]core.Edge, len(s.edges))
	copy(edges, s.edges)
	return entries, edges, nil
}

// Len returns the number of entries and edges currently stored.
// Useful for test assertions without going through Load.
func (s *MemStore) Len() (entries, edges int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries), len(s.edges)
}

// Reset clears all stored state. Provided for test setup/teardown; not part of
// the core.Store port (MemStore is explicitly a test helper).
func (s *MemStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = nil
	s.edges = nil
}

// compile-time assertion: MemStore must satisfy core.Store.
var _ core.Store = (*MemStore)(nil)
