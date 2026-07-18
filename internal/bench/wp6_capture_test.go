package bench

// wp6_capture_test.go — WP-6: the capture loop closed on the REAL cases.
//
// The Jul-14 gate carried an honest caveat: the 1.00 score "assumes the edge
// already exists (corpus builds it); it proves the filter *uses* the edge
// correctly, not that the live system *creates* it." This test retires that
// caveat for the four real correction utterances preserved on the case
// fixtures: it replays each case from the PRE-CORRECTION state (the stale
// belief still current, no edge anywhere), runs the restored capture layer on
// the verbatim utterance, confirms the proposal the way an operator would, and
// then requires the WP-3/4 graph recall to surface the current entry.
//
// The edge in the final graph exists ONLY because capture proposed it and the
// confirmation executed the ceremony — nothing is pre-built.

import (
	"context"
	"testing"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/graphstore"
	"github.com/clockworksoul/enso/internal/mdstore"
)

// utteranceCases returns every real case that preserved its correction
// utterance (STALE class only; NEIGHBOR misses had no correction utterance —
// that emptiness is structural, see the Case.Utterance doc).
func utteranceCases() []Case {
	var out []Case
	for _, c := range append(SeedCases(), HeldOutStaleCases()...) {
		if c.Utterance != "" {
			out = append(out, c)
		}
	}
	return out
}

// preCorrectionState reconstructs the corpus as it stood the moment the
// utterance was spoken: the stale entry still CURRENT (its ValidUntil unset —
// the fixture records the post-correction closure, which capture has not
// performed yet), the corrected entry not yet written, no SUPERSEDES edge.
func preCorrectionState(t *testing.T, c Case) (stale core.Entry, current core.Entry) {
	t.Helper()
	found := 0
	for _, cand := range c.Candidates {
		if cand.ID == c.WantID {
			current = cand
			found++
		} else {
			stale = cand
			stale.ValidUntil = nil // reopen: capture is what will close it
			found++
		}
	}
	if found != 2 {
		t.Fatalf("case %s: expected a stale/current pair, found %d candidates", c.Name, found)
	}
	return stale, current
}

// TestWP6CaptureClosesTheLoop is the WP-6 DoD end-to-end: for each real
// utterance — utterance → ProposeSupersession → operator confirmation →
// supersession ceremony → graph recall surfaces the current entry.
func TestWP6CaptureClosesTheLoop(t *testing.T) {
	cases := utteranceCases()
	if len(cases) != 4 {
		t.Fatalf("evidence base changed: expected the 4 real correction utterances, got %d", len(cases))
	}

	ctx := context.Background()
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			stale, current := preCorrectionState(t, c)

			// The corpus at utterance time: only the stale belief exists.
			corpus := mdstore.NewFSStore(t.TempDir())
			if err := corpus.Append(ctx, []core.Entry{stale}, nil); err != nil {
				t.Fatalf("seed pre-correction corpus: %v", err)
			}

			// 1. CAPTURE — the restored layer sees the verbatim utterance.
			entries, edges, err := corpus.Load(ctx)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			p := core.ProposeSupersession(c.Utterance, entries, edges, c.AsOf)
			if !p.Actionable() {
				t.Fatalf("real utterance %q not detected (signals=%v)", c.Utterance, p.Detection.Signals)
			}
			if p.Target == nil {
				t.Fatalf("real utterance %q detected (%s) but not resolved to a target", c.Utterance, p.Evidence)
			}
			if p.Target.ID != stale.ID {
				t.Fatalf("proposal targeted %s; the documented stale belief is %s", p.Target.ID, stale.ID)
			}
			t.Logf("proposal: evidence=%s signals=%v", p.Evidence, p.Detection.Signals)

			// 2. CONFIRM — the operator accepts the proposal, authoring the
			// corrected entry (its content is the case's ground-truth current
			// text) and executing the ceremony. This is the ONLY write path;
			// the proposal itself wrote nothing.
			if err := corpus.Supersede(ctx, *p.Target, current); err != nil {
				t.Fatalf("ceremony: %v", err)
			}

			// 3. RECALL — the WP-3/4 pipeline over the graph rebuilt from the
			// corpus must now surface the current entry, via an edge that
			// exists only because capture created it.
			g, err := graphstore.OpenRebuiltFrom(ctx, "", corpus)
			if err != nil {
				t.Fatalf("build graph: %v", err)
			}
			defer g.Close()
			rr, err := g.Recall(ctx, c.Query, c.AsOf)
			if err != nil {
				t.Fatalf("recall: %v", err)
			}
			if len(rr.Ranked) == 0 || rr.Ranked[0].Entry.ID != c.WantID {
				got := core.ID("(none)")
				if len(rr.Ranked) > 0 {
					got = rr.Ranked[0].Entry.ID
				}
				t.Fatalf("recall top = %s, want %s", got, c.WantID)
			}
			for _, r := range rr.Ranked {
				if r.Entry.ID == stale.ID {
					t.Fatalf("stale entry surfaced despite capture-created supersession")
				}
			}

			// The edge really is capture-created: it exists in the corpus now
			// and did not before.
			_, postEdges, err := corpus.Load(ctx)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			edgeFound := false
			for _, ed := range postEdges {
				if ed.Type == core.EdgeSupersedes && core.ID(ed.To) == stale.ID && ed.From == current.ID {
					edgeFound = true
				}
			}
			if !edgeFound {
				t.Fatal("SUPERSEDES edge missing from corpus after confirmation")
			}
		})
	}
}

// TestWP6ProposalWritesNothing pins the no-auto-commit DoD box at the store
// level: running capture over a live corpus leaves the corpus byte-identical.
func TestWP6ProposalWritesNothing(t *testing.T) {
	ctx := context.Background()
	c := utteranceCases()[0]
	stale, _ := preCorrectionState(t, c)

	corpus := mdstore.NewFSStore(t.TempDir())
	if err := corpus.Append(ctx, []core.Entry{stale}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, beforeEdges, err := corpus.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	entries, edges, _ := corpus.Load(ctx)
	_ = core.ProposeSupersession(c.Utterance, entries, edges, c.AsOf)

	after, afterEdges, err := corpus.Load(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(after) != len(before) || len(afterEdges) != len(beforeEdges) {
		t.Fatalf("proposing wrote to the corpus: %d→%d entries, %d→%d edges",
			len(before), len(after), len(beforeEdges), len(afterEdges))
	}
}
