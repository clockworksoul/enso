package graphstore

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

// testCorpus builds a small but representative corpus: two plain facts about
// distinct topics, one supersession triple (open original + newer + closed
// copy + SUPERSEDES edge), and a parent-OWNS-child pair sharing an ABOUT
// entity. It exercises every structural shape recall v1 must handle.
func testCorpus(t *testing.T) ([]core.Entry, []core.Edge) {
	t.Helper()
	d := func(day int) time.Time { return time.Date(2026, 7, day, 9, 0, 0, 0, time.UTC) }
	mk := func(day int, label string, nt core.NodeType, content string, tags, about []string) core.Entry {
		id, err := core.NewID(d(day), label)
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		e, err := core.NewEntry(core.NewEntryParams{
			ID: id, Type: nt, Content: content, EncodedTime: d(day),
			Confidence: core.ConfHigh, Tags: tags, About: about,
		})
		if err != nil {
			t.Fatalf("NewEntry: %v", err)
		}
		return e
	}

	parent := mk(1, "oss-root", core.TypeFact,
		"clockworksoul OSS repos live under the workspace root",
		[]string{"workspace", "repos", "clockworksoul"}, []string{"project:clockworksoul"})
	child := mk(2, "enso-path", core.TypeFact,
		"the enso repo is cloned inside the clockworksoul workspace root",
		[]string{"enso"}, []string{"project:enso", "project:clockworksoul"})
	stale := mk(3, "granola-installed", core.TypeFact,
		"granola stays installed for meeting notes",
		[]string{"granola"}, []string{})
	current := mk(4, "granola-uninstalled", core.TypeFact,
		"granola was uninstalled; meeting notes move to plain markdown",
		[]string{"granola"}, []string{})
	unrelated := mk(5, "coffee", core.TypeFact,
		"the good espresso beans are the dark roast",
		[]string{"espresso"}, []string{})

	closed, supEdge := stale.Supersede(current.ID, d(4))
	owns := core.Edge{From: parent.ID, Type: core.EdgeOwns, To: string(child.ID), Extra: map[string]string{}}

	entries := []core.Entry{parent, child, stale, current, closed, unrelated}
	edges := []core.Edge{supEdge, owns}
	return entries, edges
}

// TestReopenContinuesSequence pins that a reopened on-disk graph keeps
// numbering where it left off, so append order stays globally consistent
// across process restarts.
func TestReopenContinuesSequence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.kuzu")
	entries, edges := testCorpus(t)

	g, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := g.Append(ctx, entries[:3], nil); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	g.Close()

	g2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer g2.Close()
	if err := g2.Append(ctx, entries[3:], edges); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	gotE, gotEd, err := g2.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(gotE) != len(entries) || len(gotEd) != len(edges) {
		t.Fatalf("after reopen: got %d entries, %d edges; want %d, %d",
			len(gotE), len(gotEd), len(entries), len(edges))
	}
	// Append order must be intact across the reopen boundary.
	for i := range entries {
		if gotE[i].ID != entries[i].ID {
			t.Fatalf("append order lost at %d: got %s, want %s", i, gotE[i].ID, entries[i].ID)
		}
	}
}

// TestRebuildDeterministic pins the WP-3 policy: rebuild is a pure function of
// the corpus. Two rebuilds from the same corpus yield identical graphs —
// verified by their full Load output, the only observable that matters.
func TestRebuildDeterministic(t *testing.T) {
	ctx := context.Background()
	entries, edges := testCorpus(t)

	load := func(path string) ([]core.Entry, []core.Edge) {
		g, err := OpenRebuilt(ctx, path, entries, edges)
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		defer g.Close()
		e, ed, err := g.Load(ctx)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		return e, ed
	}

	dir := t.TempDir()
	e1, ed1 := load(filepath.Join(dir, "a.kuzu"))
	e2, ed2 := load(filepath.Join(dir, "b.kuzu"))
	if !reflect.DeepEqual(e1, e2) || !reflect.DeepEqual(ed1, ed2) {
		t.Fatalf("two rebuilds from the same corpus differ")
	}

	// Idempotent at the same path too: rebuilding over an existing index
	// yields the same graph, not an accumulation.
	e3, ed3 := load(filepath.Join(dir, "a.kuzu"))
	if !reflect.DeepEqual(e1, e3) || !reflect.DeepEqual(ed1, ed3) {
		t.Fatalf("rebuild over an existing index is not idempotent")
	}
}

// TestKillTheGraphDrill proves INV-1 rather than assuming it: write the corpus
// to the CANONICAL Markdown store, build the graph, record recall results,
// destroy the graph database entirely, rebuild from Markdown alone, and
// require identical recall results. The graph must carry zero unique state.
func TestKillTheGraphDrill(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	entries, edges := testCorpus(t)

	corpus := mdstore.NewFSStore(t.TempDir())
	if err := corpus.Append(ctx, entries, edges); err != nil {
		t.Fatalf("seed markdown corpus: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "index.kuzu")
	queries := []string{
		"what happened with granola?",
		"where do the clockworksoul repos live?",
		"espresso beans",
		"", // recent mode
	}

	recallAll := func(g *GraphStore) [][]core.ID {
		var out [][]core.ID
		for _, q := range queries {
			rr, err := g.Recall(ctx, q, now)
			if err != nil {
				t.Fatalf("recall %q: %v", q, err)
			}
			ids := make([]core.ID, len(rr.Ranked))
			for i, r := range rr.Ranked {
				ids[i] = r.Entry.ID
			}
			out = append(out, ids)
		}
		return out
	}

	g1, err := OpenRebuiltFrom(ctx, dbPath, corpus)
	if err != nil {
		t.Fatalf("initial build: %v", err)
	}
	before := recallAll(g1)
	g1.Close()

	// Kill the graph. This is the drill: the index is gone, Markdown remains.
	g2, err := OpenRebuiltFrom(ctx, dbPath, corpus) // OpenRebuilt removes the old db first
	if err != nil {
		t.Fatalf("rebuild after kill: %v", err)
	}
	defer g2.Close()
	after := recallAll(g2)

	if !reflect.DeepEqual(before, after) {
		t.Fatalf("recall diverged after kill-and-rebuild:\n before %v\n after  %v", before, after)
	}
}

// TestLogFirstWritePath pins the unified-spec §5 ordering: corpus write first;
// a graph failure after a successful corpus write surfaces as *GraphLagError
// (memory safe, index behind) — never as a lost write.
func TestLogFirstWritePath(t *testing.T) {
	ctx := context.Background()
	entries, edges := testCorpus(t)

	corpus := mdstore.NewFSStore(t.TempDir())
	g, err := Open(filepath.Join(t.TempDir(), "index.kuzu"))
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}

	lf := &LogFirst{Corpus: corpus, Graph: g}
	if err := lf.Append(ctx, entries[:2], nil); err != nil {
		t.Fatalf("healthy append: %v", err)
	}

	// Poison the graph (closed database) and append more: the corpus write
	// must land and the failure must be typed as lag, not loss.
	g.Close()
	err = lf.Append(ctx, entries[2:4], nil)
	var lag *GraphLagError
	if !errors.As(err, &lag) {
		t.Fatalf("want *GraphLagError from poisoned graph, got %v", err)
	}
	gotE, _, err := corpus.Load(ctx)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	if len(gotE) != 4 {
		t.Fatalf("corpus must hold all 4 entries despite graph failure, got %d", len(gotE))
	}

	// And the "repair" is exactly a rebuild from the corpus.
	g2, err := OpenRebuiltFrom(ctx, filepath.Join(t.TempDir(), "fresh.kuzu"), corpus)
	if err != nil {
		t.Fatalf("repair rebuild: %v", err)
	}
	defer g2.Close()
	repE, _, err := g2.Load(ctx)
	if err != nil {
		t.Fatalf("load repaired graph: %v", err)
	}
	if len(repE) != 4 {
		t.Fatalf("repaired graph must hold all 4 entries, got %d", len(repE))
	}
	_ = edges
}
