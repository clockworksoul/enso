package bench

// wp3_graph_test.go — the WP-3 real-corpus gate for the KùzuDB graph adapter.
//
// The Jul-14 rebaseline proved the SUPERSEDES edge is +0.43 load-bearing over
// specificity on the 79-case git-history corpus (specificity-only 0.57, full
// pipeline 1.00). WP-3's concrete acceptance bar was set there: the graph
// adapter's recall must recover those same cases — i.e. score 1.00 on this
// corpus with the stale entry NEVER surfaced as current — and must demonstrate
// edge traversal on the one real NEIGHBOR miss (2026-06-23 enso-repo-path,
// accepted as the n ≥ 1 vocabulary-drift case per Matt's 2026-07-18 sign-off).

import (
	"context"
	"testing"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/graphstore"
)

// buildCaseGraph loads one benchmark case's candidates+edges into a fresh
// in-memory graph. Each case is its own corpus snapshot at AsOf, exactly like
// the Markdown-pipeline harness treats it. The CALLER must Close the store as
// soon as the case is scored — 79 simultaneously-open embedded databases
// exhaust the process (learned the hard way; t.Cleanup is too late).
func buildCaseGraph(t *testing.T, c Case) *graphstore.GraphStore {
	t.Helper()
	g, err := graphstore.OpenRebuilt(context.Background(), "", c.Candidates, c.Edges)
	if err != nil {
		t.Fatalf("case %s: build graph: %v", c.Name, err)
	}
	return g
}

// TestWP3GraphSupersessionGate replays every real supersession triple in the
// git-history corpus through the graph adapter's recall (DoD: "on every real
// supersession triple in the corpus, the stale entry is never returned as
// current"). Bar: 79/79 with zero stale surfacings — parity with the Jul-14
// full-pipeline result the corpus was rebaselined at.
func TestWP3GraphSupersessionGate(t *testing.T) {
	records, err := LoadGitHistoryRecords(corpusPath)
	if err != nil {
		t.Fatalf("load git-history corpus: %v", err)
	}
	cases, err := GitHistoryCases(records)
	if err != nil {
		t.Fatalf("build cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("empty corpus")
	}

	hits, staleSurfaced := 0, 0
	for _, c := range cases {
		g := buildCaseGraph(t, c)
		res, err := g.Recall(context.Background(), c.Query, c.AsOf)
		g.Close()
		if err != nil {
			t.Fatalf("case %s: recall: %v", c.Name, err)
		}
		if len(res) > 0 && res[0].Entry.ID == c.WantID {
			hits++
		} else if len(res) > 0 {
			t.Logf("MISS %s: top=%s want=%s", c.Name, res[0].Entry.ID, c.WantID)
		} else {
			t.Logf("MISS %s: no results, want=%s", c.Name, c.WantID)
		}
		// The stale entry (any superseded id) must never appear at all.
		superseded := map[core.ID]bool{}
		for _, ed := range c.Edges {
			if ed.Type == core.EdgeSupersedes {
				superseded[core.ID(ed.To)] = true
			}
		}
		for _, r := range res {
			if superseded[r.Entry.ID] {
				staleSurfaced++
				t.Errorf("case %s: superseded entry %s surfaced as current", c.Name, r.Entry.ID)
			}
		}
	}

	score := float64(hits) / float64(len(cases))
	t.Logf("WP-3 graph gate: P@1 = %d/%d = %.2f (bar: 1.00, Jul-14 full-pipeline parity); stale surfacings: %d (bar: 0)",
		hits, len(cases), score, staleSurfaced)
	if hits != len(cases) {
		t.Errorf("graph recall scored %d/%d; the WP-3 bar is full parity with the Markdown pipeline (79/79)", hits, len(cases))
	}
}

// TestWP3GraphNeighborTraversal exercises the real 2026-06-23 enso-repo-path
// NEIGHBOR miss through the graph (the n ≥ 1 vocabulary-drift case, Matt's
// 2026-07-18 sign-off):
//
//  1. Full recall on the real query must rank the specific child first
//     (specificity parity with the Stage-5 Markdown pipeline), and
//  2. the OWNS edge must demonstrably carry traversal from the vague parent —
//     the entry the plugin actually surfaced during the real miss — to the
//     specific child, which is the reach a flat file cannot provide.
func TestWP3GraphNeighborTraversal(t *testing.T) {
	for _, c := range NeighborCases() {
		g := buildCaseGraph(t, c)
		defer g.Close()

		res, err := g.Recall(context.Background(), c.Query, c.AsOf)
		if err != nil {
			t.Fatalf("case %s: recall: %v", c.Name, err)
		}
		if len(res) == 0 || res[0].Entry.ID != c.WantID {
			got := core.ID("(none)")
			if len(res) > 0 {
				got = res[0].Entry.ID
			}
			t.Errorf("case %s: top result %s, want %s", c.Name, got, c.WantID)
		}

		// Edge-reach demonstration: from the parent seed alone, traversal must
		// arrive at the child via the real OWNS edge.
		var parent core.ID
		for _, ed := range c.Edges {
			if ed.Type == core.EdgeOwns && core.ID(ed.To) == c.WantID {
				parent = ed.From
			}
		}
		if parent == "" {
			t.Fatalf("case %s: no OWNS edge to the target in the fixture", c.Name)
		}
		reached, err := g.Neighbors(context.Background(), []core.ID{parent})
		if err != nil {
			t.Fatalf("case %s: traverse: %v", c.Name, err)
		}
		found := false
		for _, id := range reached {
			if id == c.WantID {
				found = true
			}
		}
		if !found {
			t.Errorf("case %s: traversal from parent %s did not reach child %s via OWNS", c.Name, parent, c.WantID)
		}
	}
}
