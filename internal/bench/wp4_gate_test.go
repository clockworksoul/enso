package bench

// wp4_gate_test.go — THE WP-4 BENCHMARK GATE (dev spec §8, RH-3: a hard gate).
//
// Recall v2 (vectors → entry nodes → traversal → supersession filter → rank)
// must beat BOTH baselines on the labeled real-miss corpus:
//
//	(i)  naive recency          (BaselineModel — the P0 stand-in), and
//	(ii) flat-file lexical search (SpecificityBlindModel — the
//	     memory_search-equivalent: query-match ranking, no supersession
//	     knowledge, no graph),
//
// specifically on staleness suppression (#4) and WITHOUT inflating the noise
// rate (irrelevant results ranked above the correct answer, per query, must
// not exceed either baseline's). Numbers are logged here and recorded in
// ENSO-STATUS.md; if this test fails, WP-4 does not merge (RH-3).
//
// The vector layer's own marginal contribution is measured honestly as
// DOORFINDER coverage: on how many cases does the correct entry become a seed
// at all (lexically vs +vectors)? P@1 parity alone would hide that the
// no-lexical-overlap cases are carried by decay coincidence rather than by
// actually finding the door.

import (
	"context"
	"testing"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/graphstore"
)

// noiseAbove counts results ranked above the wanted entry (the per-query
// noise the gate must not inflate). If the wanted entry is absent entirely,
// every returned result counts as noise.
func noiseAbove(ranked []core.ID, want core.ID) int {
	for i, id := range ranked {
		if id == want {
			return i
		}
	}
	return len(ranked)
}

func TestWP4VectorGate(t *testing.T) {
	sem, err := LoadSemanticModel(embeddingsPath)
	if err != nil {
		t.Fatalf("load embeddings: %v", err)
	}
	if sem == nil {
		// The gate cannot run without the pre-computed corpus embeddings.
		// Skipping loudly is honest on a checkout without the file; the file
		// is committed, so CI always runs the gate.
		t.Skipf("embeddings file %s absent — WP-4 gate requires it", embeddingsPath)
	}
	records, err := LoadGitHistoryRecords(corpusPath)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	cases, err := GitHistoryCases(records)
	if err != nil {
		t.Fatalf("build cases: %v", err)
	}

	emb := graphstore.MapEmbedder{Vectors: sem.embeddings}

	var (
		v2Hits, recencyHits, lexicalHits       int
		v2Noise, recencyNoise, lexicalNoise    int
		staleSurfaced                          int
		doorLexical, doorVector, doorRecovered int
		recency                                = BaselineModel{}
		lexical                                = SpecificityBlindModel{}
	)

	for _, c := range cases {
		// --- recall v2 through the graph adapter ---
		g, err := graphstore.OpenRebuiltWith(context.Background(), "", emb, c.Candidates, c.Edges)
		if err != nil {
			t.Fatalf("case %s: build: %v", c.Name, err)
		}
		rr, err := g.Recall(context.Background(), c.Query, c.AsOf)
		g.Close()
		if err != nil {
			t.Fatalf("case %s: recall: %v", c.Name, err)
		}
		if rr.Mode != graphstore.ModeVector {
			t.Fatalf("case %s: mode %s — the gate must measure the vector path", c.Name, rr.Mode)
		}
		var v2IDs []core.ID
		superseded := map[core.ID]bool{}
		for _, ed := range c.Edges {
			if ed.Type == core.EdgeSupersedes {
				superseded[core.ID(ed.To)] = true
			}
		}
		for _, r := range rr.Ranked {
			v2IDs = append(v2IDs, r.Entry.ID)
			if superseded[r.Entry.ID] {
				staleSurfaced++
				t.Errorf("case %s: stale entry %s surfaced", c.Name, r.Entry.ID)
			}
		}
		if len(v2IDs) > 0 && v2IDs[0] == c.WantID {
			v2Hits++
		}
		v2Noise += noiseAbove(v2IDs, c.WantID)

		// --- baselines over the same candidates ---
		recencyIDs := idsOf(recency.Rank(c.Candidates, c.Edges, c.AsOf))
		if len(recencyIDs) > 0 && recencyIDs[0] == c.WantID {
			recencyHits++
		}
		recencyNoise += noiseAbove(recencyIDs, c.WantID)

		lexicalIDs := idsOf(lexical.RankQuery(c.Query, c.Candidates, c.Edges, c.AsOf))
		if len(lexicalIDs) > 0 && lexicalIDs[0] == c.WantID {
			lexicalHits++
		}
		lexicalNoise += noiseAbove(lexicalIDs, c.WantID)

		// --- doorfinder coverage: is the correct entry a SEED at all? ---
		terms := core.Tokenize(c.Query)
		var want core.Entry
		for _, cand := range c.Candidates {
			if cand.ID == c.WantID {
				want = cand
			}
		}
		lexicalDoor := core.Specificity(want, terms) > 0
		vectorDoor := false
		if qv, qerr := emb.Embed(context.Background(), c.Query); qerr == nil {
			if wv, werr := emb.Embed(context.Background(), want.Content); werr == nil {
				// Same threshold recall v2 applies (vectorMinSim).
				vectorDoor = graphstore.Cosine(qv, wv) >= 0.60
			}
		}
		if lexicalDoor {
			doorLexical++
		}
		if lexicalDoor || vectorDoor {
			doorVector++
		}
		if !lexicalDoor && vectorDoor {
			doorRecovered++
		}
	}

	n := len(cases)
	score := func(h int) float64 { return float64(h) / float64(n) }
	meanNoise := func(x int) float64 { return float64(x) / float64(n) }

	t.Logf("WP-4 GATE on %d real-miss cases:", n)
	t.Logf("  recall v2 (vector+graph+supersession): P@1 = %d/%d = %.2f   mean noise-above = %.3f   stale surfaced = %d",
		v2Hits, n, score(v2Hits), meanNoise(v2Noise), staleSurfaced)
	t.Logf("  baseline (i) naive recency:            P@1 = %d/%d = %.2f   mean noise-above = %.3f",
		recencyHits, n, score(recencyHits), meanNoise(recencyNoise))
	t.Logf("  baseline (ii) flat lexical search:     P@1 = %d/%d = %.2f   mean noise-above = %.3f",
		lexicalHits, n, score(lexicalHits), meanNoise(lexicalNoise))
	t.Logf("  doorfinder coverage (correct entry seeded): lexical-only %d/%d, +vectors %d/%d (vectors recover %d no-lexical-overlap cases)",
		doorLexical, n, doorVector, n, doorRecovered)

	// THE GATE (RH-3): beat both baselines; do not inflate noise; suppress
	// staleness completely.
	if v2Hits <= recencyHits {
		t.Errorf("GATE FAIL: recall v2 (%d) does not beat naive recency (%d)", v2Hits, recencyHits)
	}
	if v2Hits <= lexicalHits {
		t.Errorf("GATE FAIL: recall v2 (%d) does not beat flat lexical search (%d)", v2Hits, lexicalHits)
	}
	if v2Noise > recencyNoise || v2Noise > lexicalNoise {
		t.Errorf("GATE FAIL: recall v2 noise (%d) exceeds a baseline (recency %d, lexical %d)",
			v2Noise, recencyNoise, lexicalNoise)
	}
	if staleSurfaced != 0 {
		t.Errorf("GATE FAIL: %d stale surfacings (staleness suppression, fix #4)", staleSurfaced)
	}
}

func idsOf(entries []core.Entry) []core.ID {
	out := make([]core.ID, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}
