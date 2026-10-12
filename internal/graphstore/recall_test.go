package graphstore

import (
	"context"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

func openWithCorpus(t *testing.T) (*GraphStore, []core.Entry, []core.Edge) {
	t.Helper()
	entries, edges := testCorpus(t)
	g, err := OpenRebuilt(context.Background(), "", entries, edges)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	t.Cleanup(g.Close)
	return g, entries, edges
}

// TestRecallSupersessionFilter pins DoD box 4 at the adapter level: a
// superseded entry is never returned as current, on the exact query where
// specificity alone provably cannot break the tie (same tags, same subject —
// only the SUPERSEDES edge distinguishes stale from current).
func TestRecallSupersessionFilter(t *testing.T) {
	g, _, _ := openWithCorpus(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	rr, err := g.Recall(context.Background(), "what happened with granola?", now)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(rr.Ranked) == 0 {
		t.Fatalf("no results")
	}
	if got := rr.Ranked[0].Entry.ID; got != "mem:2026-07-04-granola-uninstalled" {
		t.Fatalf("top result is %s; want the current granola entry", got)
	}
	for _, r := range rr.Ranked {
		if r.Entry.ID == "mem:2026-07-03-granola-installed" {
			t.Fatalf("superseded entry surfaced as current")
		}
	}
}

// TestRecallTraversalReachesChild is the vocabulary-drift/traversal DoD box,
// exercised in the shape of the real 2026-06-23 enso-repo-path miss (the n ≥ 1
// real case, per Matt's 2026-07-18 sign-off; the full real-case replay runs in
// internal/bench). The query's terms lexically match ONLY the parent record —
// the child never mentions "repos" and carries no matching tag token for the
// query — so a lexical-only pipeline cannot see it. It must surface anyway,
// via the parent's OWNS edge.
func TestRecallTraversalReachesChild(t *testing.T) {
	g, _, _ := openWithCorpus(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	// Terms: "repos" matches the parent's tags/content only.
	const query = "repos"
	childID := core.ID("mem:2026-07-02-enso-path")

	// Honesty check on the fixture itself: the child must NOT match lexically,
	// or this test stops proving traversal. (Tokenize("repos") vs the child's
	// surfaces.)
	terms := core.Tokenize(query)
	entries, _, err := g.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, e := range entries {
		if e.ID == childID && core.Specificity(e, terms) > 0 {
			t.Fatalf("fixture broken: child matches the query lexically; traversal not exercised")
		}
	}

	rr, err := g.Recall(context.Background(), query, now)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	childPos, noisePos := -1, -1
	for i, r := range rr.Ranked {
		switch r.Entry.ID {
		case childID:
			childPos = i
		case "mem:2026-07-05-coffee": // unconnected, lexically non-matching noise
			noisePos = i
		}
	}
	if childPos < 0 {
		t.Fatalf("child entry not surfaced: traversal over OWNS failed to reach it")
	}
	// The tier boundary is the observable value of the edge: the connected
	// child must outrank the unconnected noise entry even though BOTH score
	// specificity 0 and the noise entry is FRESHER (encoded later). Without
	// traversal they would sort by decay and the noise would win.
	if noisePos >= 0 && noisePos < childPos {
		t.Fatalf("edge-connected child (pos %d) ranked below unconnected noise (pos %d): traversal not load-bearing", childPos, noisePos)
	}
}

// TestRecallEmptyQueryIsRecentMode pins the degradation invariant: with no
// query terms, recall returns all current entries ranked by decay strength
// alone (identical semantics to core.Rank over the current set).
func TestRecallEmptyQueryIsRecentMode(t *testing.T) {
	g, entries, edges := openWithCorpus(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	rr, err := g.Recall(context.Background(), "", now)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	// Expected: filter superseded + expired in corpus order, then pure decay rank.
	superseded := map[core.ID]bool{}
	for _, ed := range edges {
		if ed.Type == core.EdgeSupersedes {
			superseded[core.ID(ed.To)] = true
		}
	}
	var kept []core.Entry
	for _, e := range entries {
		if superseded[e.ID] || !e.IsCurrent(now) {
			continue
		}
		kept = append(kept, e)
	}
	want := core.Rank(kept, now)

	if len(rr.Ranked) != len(want) {
		t.Fatalf("got %d results, want %d", len(rr.Ranked), len(want))
	}
	for i := range want {
		if rr.Ranked[i].Entry.ID != want[i].Entry.ID {
			t.Fatalf("recent-mode order diverges from core.Rank at %d: got %s, want %s",
				i, rr.Ranked[i].Entry.ID, want[i].Entry.ID)
		}
	}
}

// TestRecallRankingModes pins the production regression and both explicit
// fallback modes. Blended is the default; legacy specificity preserves the
// old hard tier; decay ignores query relevance.
func TestRecallRankingModes(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	mk := func(label, content string, at time.Time) core.Entry {
		t.Helper()
		id, err := core.NewID(at, label)
		if err != nil {
			t.Fatal(err)
		}
		e, err := core.NewEntry(core.NewEntryParams{
			ID: id, Type: core.TypeFact, Content: content, EncodedTime: at,
			Confidence: core.ConfHigh, Tags: []string{}, About: []string{},
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	oldMatch := mk("old-generic", "database benchmark notes", now.Add(-90*24*time.Hour))
	fresh := mk("fresh-answer", "the release blocker was resolved by rotating the signing key", now)
	g, err := OpenRebuilt(context.Background(), "", []core.Entry{oldMatch, fresh}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	checkTop := func(name string, opts RecallOptions, want core.ID) {
		t.Helper()
		rr, err := g.RecallWithOptions(context.Background(), "database architecture", now, opts)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(rr.Ranked) == 0 || rr.Ranked[0].Entry.ID != want {
			t.Fatalf("%s top = %v, want %s", name, rankedIDs(rr), want)
		}
	}

	checkTop("blended default", RecallOptions{}, fresh.ID)
	checkTop("legacy specificity", RecallOptions{Ranking: RankingSpecificity}, oldMatch.ID)
	checkTop("decay", RecallOptions{Ranking: RankingDecay}, fresh.ID)
}
