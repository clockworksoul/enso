package bench

// wp5_activation_test.go — WP-5: Phase-3 activation of the RECALL-DEF bump.
//
// Matt explicitly overrode the WP-5 evidence lock on 2026-07-18 (recorded in
// ENSO-STATUS.md). The Jul-16 work proved the divergence at MODEL level
// (RecallBumpModel); WP-5 wires the same mechanism through the SHIPPED path —
// core.MarkRecalled events appended via the Store port, resolved by the graph
// adapter's latest-record-per-id recall — and re-proves every claim there:
//
//   1. Ranking quality vs the P2 baseline: on the recency-vs-relevance
//      scenario, the P2 pipeline (no bumps) picks the fresh-but-cold entry;
//      the SAME pipeline over the SAME corpus after real MarkRecalled events
//      picks the durable-and-used one. The delta is attributable to the
//      wiring alone.
//   2. INV-2: every bump is an APPENDED temporal-update record; prior state
//      stays in the corpus; nothing is rewritten.
//   3. INV-1: bump records live in Markdown — kill the graph, rebuild, same
//      answer.
//   4. Runaway Hebbian brakes: S_cap bounds unlimited consolidation, and a
//      cold-but-relevant entry still breaks in over a hot one (specificity
//      primacy is the shipped novelty brake).
//
// Honest caveat, carried over from Jul-16 and still true: the ranking-quality
// scenario is n=1 constructed. Corpus-scale gains are unmeasurable until live
// material-recall telemetry exists — WP-5 ships the wiring and its proofs,
// not a corpus-scale number.

import (
	"context"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/graphstore"
	"github.com/clockworksoul/enso/internal/mdstore"
)

// wp5Scenario seeds a Markdown corpus with the Jul-16 divergence pair:
// A durable-and-recalled (old write, weekly material recalls), B fresh-but-cold
// (new write, never recalled). Returns the corpus store and the two entries.
func wp5Scenario(t *testing.T, withRecalls bool) (*mdstore.FSStore, core.Entry, core.Entry, time.Time) {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	writeB := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	entryA := mustEntry("wp5-loadbearing-invariant", core.TypeFact,
		"Ensō invariant: Markdown is canonical; the graph index is a derived cache",
		base, nil, []string{"enso", "architecture", "invariant"}, []string{"project:enso"})
	entryB := mustEntry("wp5-writeonly-note", core.TypeFact,
		"The Jul-2 enso architecture sync had six attendees",
		writeB, nil, []string{"enso", "architecture", "meeting"}, []string{"project:enso"})

	store := mdstore.NewFSStore(t.TempDir())
	if err := store.Append(ctx, []core.Entry{entryA, entryB}, nil); err != nil {
		t.Fatalf("seed corpus: %v", err)
	}
	if withRecalls {
		// Weekly material recalls of A from Apr 8 until B's write — each one a
		// real RECALL-DEF event through the shipped primitive.
		for wk := base.AddDate(0, 0, 7); wk.Before(writeB); wk = wk.AddDate(0, 0, 7) {
			if _, err := core.MarkRecalled(ctx, store, entryA.ID, wk); err != nil {
				t.Fatalf("mark recalled at %s: %v", wk, err)
			}
		}
	}
	return store, entryA, entryB, now
}

// recallTop runs graph recall (recent mode — pure strength order, the mode
// where the bump is the only differentiator) and returns the top id.
func recallTop(t *testing.T, store *mdstore.FSStore, now time.Time) core.ID {
	t.Helper()
	g, err := graphstore.OpenRebuiltFrom(context.Background(), "", store)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	defer g.Close()
	rr, err := g.Recall(context.Background(), "", now)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(rr.Ranked) == 0 {
		t.Fatalf("no results")
	}
	return rr.Ranked[0].Entry.ID
}

// TestWP5WiredBumpBeatsP2Baseline is the WP-5 ranking-quality measurement.
func TestWP5WiredBumpBeatsP2Baseline(t *testing.T) {
	// P2 baseline (no recall events): the fresh-but-cold B wins — the control
	// that pins "decay without bumps == recency" (Jul-15/16) on the shipped path.
	baseStore, _, entryB, now := wp5Scenario(t, false)
	if top := recallTop(t, baseStore, now); top != entryB.ID {
		t.Fatalf("P2 baseline control broken: want cold-fresh %s on top, got %s", entryB.ID, top)
	}

	// WP-5 wired: same corpus + 12 weekly RECALL-DEF events on A → A wins.
	bumpStore, entryA, _, now := wp5Scenario(t, true)
	if top := recallTop(t, bumpStore, now); top != entryA.ID {
		t.Fatalf("WP-5 regression: after spaced material recalls, want durable %s on top, got %s", entryA.ID, top)
	}
	t.Logf("WP-5 ranking quality (n=1 constructed relevance-recall case, shipped path end-to-end):")
	t.Logf("  P2 baseline (no bump):  %s first  [fresh-but-cold — recency proxy]", entryB.ID)
	t.Logf("  WP-5 wired (bumped):    %s first  [durable-and-used — RIGHT]", entryA.ID)
}

// TestWP5BumpIsAppendOnly pins INV-2 + RH-5 for the wiring: each MarkRecalled
// appends a temporal-update record; nothing is rewritten; the latest record
// carries the consolidation.
func TestWP5BumpIsAppendOnly(t *testing.T) {
	store, entryA, _, _ := wp5Scenario(t, true)
	entries, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var records []core.Entry
	for _, e := range entries {
		if e.ID == entryA.ID {
			records = append(records, e)
		}
	}
	// 1 original + 12 weekly bump records (Apr 8 .. Jun 24).
	if len(records) != 13 {
		t.Fatalf("want 13 records for %s (original + 12 bumps), got %d", entryA.ID, len(records))
	}
	if !records[0].Temporal.LastRefTime.Equal(entryA.EncodedTime) {
		t.Fatalf("original record was rewritten: LastRefTime %v", records[0].Temporal.LastRefTime)
	}
	// Consolidation must be monotone across the record history: each bump
	// raises S_floor and advances LastRefTime.
	for i := 1; i < len(records); i++ {
		if records[i].Temporal.SFloor <= records[i-1].Temporal.SFloor {
			t.Fatalf("S_floor not raised at bump %d: %g -> %g", i,
				records[i-1].Temporal.SFloor, records[i].Temporal.SFloor)
		}
		if !records[i].Temporal.LastRefTime.After(records[i-1].Temporal.LastRefTime) {
			t.Fatalf("LastRefTime not advanced at bump %d", i)
		}
	}
}

// TestWP5KillTheGraphKeepsBumps pins INV-1 for the wiring: temporal-update
// records are Markdown-canonical, so the rebuilt graph ranks identically.
func TestWP5KillTheGraphKeepsBumps(t *testing.T) {
	store, entryA, _, now := wp5Scenario(t, true)
	first := recallTop(t, store, now)
	second := recallTop(t, store, now) // fresh rebuild from Markdown alone
	if first != second || first != entryA.ID {
		t.Fatalf("bump state not corpus-canonical: first=%s second=%s want=%s", first, second, entryA.ID)
	}
}

// TestWP5RunawayBrakes pins the two shipped brakes on Hebbian runaway
// (tech spec §5.5): the S_cap ceiling, and cold-but-relevant break-in.
func TestWP5RunawayBrakes(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	hot := mustEntry("wp5-hot-monopolist", core.TypeFact,
		"The build system uses make with a check target",
		base, nil, []string{"build", "make"}, []string{"project:enso"})
	cold := mustEntry("wp5-cold-specific", core.TypeFact,
		"The staging deploy credentials rotate on the first Monday of each month",
		base.AddDate(0, 0, 1), nil, []string{"staging", "credentials", "rotation"}, []string{})

	store := mdstore.NewFSStore(t.TempDir())
	if err := store.Append(ctx, []core.Entry{hot, cold}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Recall the hot entry weekly for a year — sustained, spaced, maximal
	// consolidation pressure.
	var last core.Entry
	when := base
	for i := 0; i < 52; i++ {
		when = when.AddDate(0, 0, 7)
		var err error
		if last, err = core.MarkRecalled(ctx, store, hot.ID, when); err != nil {
			t.Fatalf("bump %d: %v", i, err)
		}
	}

	// Brake 1 — S_cap ceiling: consolidation asymptotes; strength is bounded
	// the instant after the 52nd recall, when it is as high as it will ever be.
	if s := core.StrengthAt(last, when); s > last.Temporal.SCap {
		t.Fatalf("S_cap ceiling violated: strength %.4f > cap %.4f", s, last.Temporal.SCap)
	}
	if last.Temporal.SFloor >= last.Temporal.SCap {
		t.Fatalf("S_floor reached S_cap after 52 recalls; consolidation must asymptote, not saturate")
	}

	// Brake 2 — break-in (the shipped novelty mechanism): a query specifically
	// about the COLD entry ranks it above the year-long monopolist, because
	// specificity is the primary sort key and strength only tie-breaks.
	g, err := graphstore.OpenRebuiltFrom(ctx, "", store)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	defer g.Close()
	rr, err := g.Recall(ctx, "when do the staging credentials rotate?", when.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(rr.Ranked) == 0 || rr.Ranked[0].Entry.ID != cold.ID {
		got := core.ID("(none)")
		if len(rr.Ranked) > 0 {
			got = rr.Ranked[0].Entry.ID
		}
		t.Fatalf("rich-get-richer: cold-but-relevant entry lost to the hot monopolist (top=%s)", got)
	}
}
