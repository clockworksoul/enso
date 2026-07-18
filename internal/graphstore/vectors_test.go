package graphstore

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// failingEmbedder simulates a provider outage: every call errors.
type failingEmbedder struct{}

func (failingEmbedder) Name() string { return "failing-embedder" }
func (failingEmbedder) Embed(context.Context, string) ([]float64, error) {
	return nil, fmt.Errorf("provider outage (simulated)")
}

// fixtureVectors is a hand-built semantic space for the testCorpus fixtures:
// the query "note taking software" is semantically near the granola entries
// (a meeting-notes app) and far from everything else — while sharing ZERO
// lexical tokens with them, which is exactly the doorfinding gap vectors
// exist to close.
func fixtureVectors() map[string][]float64 {
	return map[string][]float64{
		"note taking software":                                            {1, 0},
		"granola stays installed for meeting notes":                       {0.95, 0.05},
		"granola was uninstalled; meeting notes move to plain markdown":   {0.9, 0.1},
		"clockworksoul OSS repos live under the workspace root":           {0, 1},
		"the enso repo is cloned inside the clockworksoul workspace root": {0, 1},
		"the good espresso beans are the dark roast":                      {0, 1},
	}
}

// TestVectorDoorfinderFindsSemanticMatch: recall v2 surfaces an entry the
// query cannot reach lexically, ranked above unconnected noise, and still
// never surfaces the superseded version (structure keeps judging what vectors
// merely find).
func TestVectorDoorfinderFindsSemanticMatch(t *testing.T) {
	entries, edges := testCorpus(t)
	emb := MapEmbedder{Vectors: fixtureVectors()}
	g, err := OpenRebuiltWith(context.Background(), "", emb, entries, edges)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	defer g.Close()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	rr, err := g.Recall(context.Background(), "note taking software", now)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if rr.Mode != ModeVector {
		t.Fatalf("mode = %s, want %s", rr.Mode, ModeVector)
	}
	if len(rr.Ranked) == 0 {
		t.Fatalf("no results")
	}
	if got := rr.Ranked[0].Entry.ID; got != "mem:2026-07-04-granola-uninstalled" {
		t.Fatalf("top result %s; want the current granola entry via vector seeding", got)
	}
	for _, r := range rr.Ranked {
		if r.Entry.ID == "mem:2026-07-03-granola-installed" {
			t.Fatalf("superseded entry surfaced: vector seeding must not bypass the supersession filter")
		}
	}
}

// TestVectorOutageDegradesToLexical is the WP-4 DoD provider-outage test:
// with embeddings unavailable, recall returns exactly the WP-3 results —
// degrade, don't fail — and reports the degradation loudly in the result.
func TestVectorOutageDegradesToLexical(t *testing.T) {
	entries, edges := testCorpus(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	queries := []string{"what happened with granola?", "repos", "espresso beans"}

	lexical, err := OpenRebuilt(context.Background(), "", entries, edges)
	if err != nil {
		t.Fatalf("build lexical store: %v", err)
	}
	defer lexical.Close()

	outage, err := OpenRebuilt(context.Background(), "", entries, edges)
	if err != nil {
		t.Fatalf("build outage store: %v", err)
	}
	defer outage.Close()
	outage.SetEmbedder(failingEmbedder{})

	for _, q := range queries {
		want, err := lexical.Recall(context.Background(), q, now)
		if err != nil {
			t.Fatalf("lexical recall %q: %v", q, err)
		}
		got, err := outage.Recall(context.Background(), q, now)
		if err != nil {
			t.Fatalf("outage recall %q must not error (degrade, don't fail): %v", q, err)
		}
		if got.Mode != ModeDegraded || got.Degraded == nil {
			t.Fatalf("outage recall %q: mode=%s degraded=%v; want loud degradation", q, got.Mode, got.Degraded)
		}
		if !reflect.DeepEqual(rankedIDs(want), rankedIDs(got)) {
			t.Fatalf("outage recall %q diverged from WP-3 lexical results:\n want %v\n  got %v",
				q, rankedIDs(want), rankedIDs(got))
		}
	}
}

// TestVectorRebuildDeterministic extends the kill-the-graph guarantee to
// vectors: they are derived data, recomputed on rebuild, and two rebuilds
// with the same (deterministic) embedder answer identically (INV-1).
func TestVectorRebuildDeterministic(t *testing.T) {
	entries, edges := testCorpus(t)
	emb := MapEmbedder{Vectors: fixtureVectors()}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	run := func() []string {
		g, err := OpenRebuiltWith(context.Background(), "", emb, entries, edges)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		defer g.Close()
		rr, err := g.Recall(context.Background(), "note taking software", now)
		if err != nil {
			t.Fatalf("recall: %v", err)
		}
		return rankedIDs(rr)
	}
	if a, b := run(), run(); !reflect.DeepEqual(a, b) {
		t.Fatalf("vector recall not deterministic across rebuilds:\n %v\n %v", a, b)
	}
}

func rankedIDs(rr RecallResult) []string {
	out := make([]string, len(rr.Ranked))
	for i, r := range rr.Ranked {
		out[i] = string(r.Entry.ID)
	}
	return out
}
