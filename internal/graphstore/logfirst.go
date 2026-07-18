package graphstore

import (
	"context"
	"fmt"

	"github.com/clockworksoul/enso/internal/core"
)

// LogFirst is the WP-3 production write path: Markdown first, graph second
// (unified spec §5). The Markdown corpus is canonical (INV-1); the graph is a
// derived index that may lag behind it but never lead it.
//
//   - Append writes to the corpus first. If that fails, nothing happened.
//   - Only after the corpus write succeeds is the graph updated. A graph
//     failure is reported as *GraphLagError — the memory IS durably recorded;
//     the index is behind and the next OpenRebuilt repairs it. Callers degrade
//     (fall back to corpus search), they do not brick (fail-safe invariant).
//   - Load reads from the corpus, never the graph: the authoritative answer
//     to "what do I remember?" must not depend on cache freshness.
type LogFirst struct {
	Corpus core.Store  // canonical Markdown store (mdstore.FSStore in production)
	Graph  *GraphStore // derived index
}

// GraphLagError reports that the canonical corpus write SUCCEEDED but the
// derived graph update failed, leaving the index stale until the next rebuild.
// It is deliberately a distinct type: callers must be able to tell "memory
// lost" (plain error) from "memory safe, index behind" (this).
type GraphLagError struct{ Err error }

func (e *GraphLagError) Error() string {
	return fmt.Sprintf("graphstore: corpus write succeeded but graph index update failed (index is stale until next rebuild): %v", e.Err)
}

func (e *GraphLagError) Unwrap() error { return e.Err }

// Append implements core.Store with log-first ordering.
func (s *LogFirst) Append(ctx context.Context, entries []core.Entry, edges []core.Edge) error {
	if err := s.Corpus.Append(ctx, entries, edges); err != nil {
		return err
	}
	if err := s.Graph.Append(ctx, entries, edges); err != nil {
		return &GraphLagError{Err: err}
	}
	return nil
}

// Load implements core.Store, reading the canonical corpus.
func (s *LogFirst) Load(ctx context.Context) ([]core.Entry, []core.Edge, error) {
	return s.Corpus.Load(ctx)
}

var _ core.Store = (*LogFirst)(nil)
