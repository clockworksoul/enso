package memstore_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/memstore"
)

// newEntry is a test helper that builds a valid core.Entry.
func newEntry(t *testing.T, id core.ID, nt core.NodeType, content string) core.Entry {
	t.Helper()
	e, err := core.NewEntry(core.NewEntryParams{
		ID:          id,
		Type:        nt,
		Content:     content,
		EncodedTime: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
		Confidence:  core.ConfHigh,
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

func TestMemStore_AppendAndLoad(t *testing.T) {
	s := memstore.New()
	ctx := context.Background()

	e1 := newEntry(t, "mem:2026-06-20-a", core.TypeFact, "first memory")
	e2 := newEntry(t, "mem:2026-06-20-b", core.TypeTask, "second memory")
	edge := core.Edge{From: "mem:2026-06-20-b", Type: core.EdgeRelatesTo, To: "mem:2026-06-20-a", Extra: map[string]string{}}

	if err := s.Append(ctx, []core.Entry{e1, e2}, []core.Edge{edge}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, edges, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(entries))
	}
	if len(edges) != 1 {
		t.Errorf("want 1 edge, got %d", len(edges))
	}
	if entries[0].ID != "mem:2026-06-20-a" || entries[1].ID != "mem:2026-06-20-b" {
		t.Errorf("unexpected entry order: %q %q", entries[0].ID, entries[1].ID)
	}
}

func TestMemStore_AppendOnly(t *testing.T) {
	// INV-2: two separate Appends must accumulate; never lose the first batch.
	s := memstore.New()
	ctx := context.Background()

	e1 := newEntry(t, "mem:2026-06-20-first", core.TypeFact, "first")
	if err := s.Append(ctx, []core.Entry{e1}, nil); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	e2 := newEntry(t, "mem:2026-06-20-second", core.TypeInsight, "second")
	if err := s.Append(ctx, []core.Entry{e2}, nil); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	entries, _, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 entries after two appends, got %d", len(entries))
	}
}

func TestMemStore_EmptyLoad(t *testing.T) {
	s := memstore.New()
	entries, edges, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load on empty: %v", err)
	}
	if entries != nil || edges != nil {
		t.Errorf("empty corpus should return nil slices, got entries=%v edges=%v", entries, edges)
	}
}

func TestMemStore_RefusesInvalidEntry(t *testing.T) {
	s := memstore.New()
	bad := core.Entry{ID: "not-an-id", Type: core.TypeFact, Content: "x",
		EncodedTime: time.Now(), Confidence: core.ConfHigh, Tags: []string{}}
	if err := s.Append(context.Background(), []core.Entry{bad}, nil); err == nil {
		t.Fatal("Append should refuse an invalid entry")
	}
	// Store must be unchanged.
	n, _ := s.Len()
	if n != 0 {
		t.Errorf("store should be unchanged after failed append, got %d entries", n)
	}
}

func TestMemStore_RefusesInvalidEdge(t *testing.T) {
	s := memstore.New()
	e := newEntry(t, "mem:2026-06-20-good", core.TypeFact, "good entry")
	badEdge := core.Edge{From: "not-an-id", Type: core.EdgeRelatesTo, To: "mem:2026-06-20-good"}
	if err := s.Append(context.Background(), []core.Entry{e}, []core.Edge{badEdge}); err == nil {
		t.Fatal("Append should refuse an invalid edge")
	}
	n, _ := s.Len()
	if n != 0 {
		t.Errorf("store should be unchanged after failed append, got %d entries", n)
	}
}

func TestMemStore_LoadReturnsCopy(t *testing.T) {
	// Mutating the returned slice must not affect the store's internal state.
	s := memstore.New()
	ctx := context.Background()
	e := newEntry(t, "mem:2026-06-20-copy", core.TypeFact, "immutable")
	if err := s.Append(ctx, []core.Entry{e}, nil); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, _, _ := s.Load(ctx)
	entries[0].Content = "mutated by caller"

	// Load again; store should still have the original content.
	entries2, _, _ := s.Load(ctx)
	if entries2[0].Content == "mutated by caller" {
		t.Error("Load returned an alias into internal state; mutation visible on re-load")
	}
}

func TestMemStore_SupersessionAdditive(t *testing.T) {
	// The supersession workflow (append new + closed old + SUPERSEDES edge)
	// must preserve both entries in history.
	s := memstore.New()
	ctx := context.Background()

	old := newEntry(t, "mem:2026-06-20-fact-v1", core.TypeFact, "old version of fact")
	if err := s.Append(ctx, []core.Entry{old}, nil); err != nil {
		t.Fatalf("Append old: %v", err)
	}

	newer := newEntry(t, "mem:2026-06-21-fact-v2", core.TypeFact, "corrected version of fact")
	closed, edge := old.Supersede(newer.ID, time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC))
	if err := s.Append(ctx, []core.Entry{newer, closed}, []core.Edge{edge}); err != nil {
		t.Fatalf("Append supersession: %v", err)
	}

	entries, edges, _ := s.Load(ctx)
	if len(edges) != 1 || edges[0].Type != core.EdgeSupersedes {
		t.Fatalf("want 1 SUPERSEDES edge, got %+v", edges)
	}
	// old entry (with valid_until set) must still be in history.
	var sawClosed bool
	for _, e := range entries {
		if e.ID == "mem:2026-06-20-fact-v1" && e.ValidUntil != nil {
			sawClosed = true
		}
	}
	if !sawClosed {
		t.Error("closed copy of superseded entry missing from history (INV-2 violation)")
	}
	_ = entries
}

func TestMemStore_ConcurrentAppend(t *testing.T) {
	// Parallel Appends must not race and the final count must be exact.
	s := memstore.New()
	ctx := context.Background()
	const goroutines = 20

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Each goroutine writes a uniquely-IDed entry.
			id := core.ID(fmt.Sprintf("mem:2026-06-20-concurrent-%02d", n))
			e := newEntry(t, id, core.TypeFact, "concurrent")
			// Ignore errors (IDs must be unique; reuse would cause validate to pass
			// but collisions to silently stack — fine for a concurrency test).
			_ = s.Append(ctx, []core.Entry{e}, nil)
		}(i)
	}
	wg.Wait()

	n, _ := s.Len()
	if n != goroutines {
		t.Errorf("concurrent appends: want %d entries, got %d", goroutines, n)
	}
}

func TestMemStore_Reset(t *testing.T) {
	s := memstore.New()
	ctx := context.Background()
	e := newEntry(t, "mem:2026-06-20-reset", core.TypeFact, "will be reset")
	_ = s.Append(ctx, []core.Entry{e}, nil)

	s.Reset()

	n, _ := s.Len()
	if n != 0 {
		t.Errorf("after Reset, want 0 entries, got %d", n)
	}
	entries, edges, err := s.Load(ctx)
	if err != nil || entries != nil || edges != nil {
		t.Errorf("after Reset, Load should return nil/nil/nil, got %v/%v/%v", entries, edges, err)
	}
}
