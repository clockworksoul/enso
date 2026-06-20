package core

import "context"

// Store is the driven (outbound) port the domain owns for persistence. Per the
// hexagon (unified spec §3), the core defines this interface on its own terms;
// adapters implement it. The Markdown filesystem store is the first
// implementation; a graph store (KùzuDB) becomes a second implementation behind
// this same port at Phase 2, which is what makes "graph later" an addition
// rather than a migration.
//
// AMEND-1: whatever a Store persists to, its on-disk format is a PUBLIC,
// documented contract — not an opaque implementation detail. The Markdown
// adapter's format is specified in the tech spec §3.1 and pinned by golden-file
// tests.
//
// INV-2 (append-only): Append never destroys. Supersession is expressed by
// appending a new entry, appending a SUPERSEDES edge, and re-appending the old
// entry with valid_until set (the store keeps the full history; readers resolve
// "current" via valid_until + edges). A Store implementation MUST NOT delete or
// in-place-rewrite prior history.
type Store interface {
	// Append durably records new entries and edges. It is additive: calling
	// Append never removes or mutates previously stored history. Implementations
	// should make the durable write before any derived index update (write
	// path: log-first, per tech spec §3.6 / unified spec §5).
	Append(ctx context.Context, entries []Entry, edges []Edge) error

	// Load reads back the full corpus (all entries and edges, including
	// superseded ones — nothing is hidden at the storage layer; staleness is a
	// read-time concern). Parse failures are loud (returned as an error), never
	// silently skipped (failure mode #2).
	Load(ctx context.Context) (entries []Entry, edges []Edge, err error)
}
