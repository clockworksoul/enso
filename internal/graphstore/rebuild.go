package graphstore

import (
	"context"
	"fmt"
	"os"

	"github.com/clockworksoul/enso/internal/core"
)

// OpenRebuilt discards any existing graph database at dbPath and rebuilds it
// from the given corpus — the WP-3 "rebuild is a pure function" policy:
// markdown → graph, deterministic (records are inserted in corpus order, so
// identical corpora yield identical graphs), idempotent (rebuilding twice
// yields the same graph as rebuilding once).
//
// Deleting dbPath is NOT a history deletion: the graph is a derived,
// rebuildable cache (INV-1); the Markdown corpus the caller loaded entries
// and edges from remains the canonical record. This is exactly the
// kill-the-graph drill made routine.
//
// Incremental sync is deliberately absent: no documented latency case demands
// it yet (RH-2), and a full rebuild of a real corpus is measured in
// milliseconds at current scale.
//
// dbPath must be a path this store owns (typically <root>/index.kuzu); an
// empty dbPath rebuilds into a fresh in-memory graph.
func OpenRebuilt(ctx context.Context, dbPath string, entries []core.Entry, edges []core.Edge) (*GraphStore, error) {
	return OpenRebuiltWith(ctx, dbPath, nil, entries, edges)
}

// OpenRebuiltWith is OpenRebuilt with an embedder attached BEFORE the corpus
// loads, so every rebuilt record gets its derived vector (WP-4). A nil
// embedder yields the pure WP-3 lexical+traversal store. Rebuild determinism
// holds whenever the embedder is deterministic for a given text (the map-
// backed and provider embedders both are).
func OpenRebuiltWith(ctx context.Context, dbPath string, emb Embedder, entries []core.Entry, edges []core.Edge) (*GraphStore, error) {
	if dbPath != "" {
		if err := os.RemoveAll(dbPath); err != nil {
			return nil, fmt.Errorf("graphstore: remove stale index %q: %w", dbPath, err)
		}
		// KùzuDB also keeps a write-ahead log beside the database file.
		if err := os.RemoveAll(dbPath + ".wal"); err != nil {
			return nil, fmt.Errorf("graphstore: remove stale wal %q: %w", dbPath, err)
		}
	}
	g, err := Open(dbPath)
	if err != nil {
		return nil, err
	}
	if emb != nil {
		g.SetEmbedder(emb)
	}
	// Entries first, then edges, so every in-corpus edge endpoint binds to its
	// real node record rather than an Entity placeholder.
	if err := g.Append(ctx, entries, nil); err != nil {
		g.Close()
		return nil, fmt.Errorf("graphstore: rebuild entries: %w", err)
	}
	if err := g.Append(ctx, nil, edges); err != nil {
		g.Close()
		return nil, fmt.Errorf("graphstore: rebuild edges: %w", err)
	}
	return g, nil
}

// OpenRebuiltFrom is the common composition: load the canonical corpus from
// src (the Markdown store in production) and rebuild the graph index from it.
func OpenRebuiltFrom(ctx context.Context, dbPath string, src core.Store) (*GraphStore, error) {
	entries, edges, err := src.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("graphstore: load canonical corpus: %w", err)
	}
	return OpenRebuilt(ctx, dbPath, entries, edges)
}
