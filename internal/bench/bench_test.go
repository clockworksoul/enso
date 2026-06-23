package bench

import (
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// TestBenchmark_EnsoBeatsBaseline is the headline assertion and the success
// metric: over the labeled corpus of real STALE misses, the Ensō model
// (staleness + supersession + decay) must rank the correct, current entry first
// strictly more often than the naive recency baseline.
//
// This is the number Stage 4+ work is measured against. A change "helps" iff it
// raises the Ensō score here without regressing it.
func TestBenchmark_EnsoBeatsBaseline(t *testing.T) {
	cases := SeedCases()
	if len(cases) == 0 {
		t.Fatal("no seed cases")
	}

	baseline := Run(BaselineModel{}, cases)
	enso := Run(EnsoModel{}, cases)

	t.Logf("corpus size: %d", len(cases))
	t.Logf("%-24s precision@1 = %.2f (%d/%d) failures=%v",
		baseline.Model, baseline.Score(), baseline.TopHits, baseline.Total, baseline.Failures)
	t.Logf("%-24s precision@1 = %.2f (%d/%d) failures=%v",
		enso.Model, enso.Score(), enso.TopHits, enso.Total, enso.Failures)

	if enso.Score() <= baseline.Score() {
		t.Errorf("Ensō model did not beat baseline: enso=%.2f baseline=%.2f",
			enso.Score(), baseline.Score())
	}

	// The seed corpus is entirely STALE pairs that supersession resolves, so the
	// Ensō model should get them ALL right.
	if enso.Score() != 1.0 {
		t.Errorf("expected Ensō to score 1.0 on the all-STALE seed corpus, got %.2f (failures: %v)",
			enso.Score(), enso.Failures)
	}
}

// TestBenchmark_BaselineFailsStale documents WHY the baseline loses: it never
// applies supersession, so on a STALE pair it cannot reliably exclude the stale
// entry. We assert it gets at least one seed case wrong, which is the whole
// reason these cases are in the corpus. (If the baseline ever started passing
// these, the cases would no longer be discriminating and we'd need harder ones.)
func TestBenchmark_BaselineFailsStale(t *testing.T) {
	baseline := Run(BaselineModel{}, SeedCases())
	if len(baseline.Failures) == 0 {
		t.Errorf("baseline unexpectedly passed all STALE cases; corpus is no longer discriminating")
	}
}

// TestBenchmark_CorrectionCaptureIsLoadBearing is the most important diagnostic
// in the file. It proves, in code, the conclusion from today's miss log: the
// Ensō model only fixes a STALE miss IF the correction was captured (the
// SUPERSEDES edge + the closed ValidUntil exist). Strip the supersession out —
// simulating "the correction was never written down" — and the Ensō model
// degrades to the baseline failure.
//
// This is the falsifiable form of "§5 #3 (no reconsolidation / correction-
// capture is load-bearing)" from the neurological-grounding doc: the math
// cannot save you from a miss you never recorded. It tells us the highest-
// leverage intervention is making correction-capture reflexive, not tuning the
// decay curve.
func TestBenchmark_CorrectionCaptureIsLoadBearing(t *testing.T) {
	cases := SeedCases()

	// Build a "correction never captured" variant of each case: drop the edges
	// AND reopen the stale entry (clear ValidUntil), so nothing marks it stale.
	uncaptured := make([]Case, 0, len(cases))
	for _, c := range cases {
		stripped := c
		stripped.Edges = nil
		newCands := make([]core.Entry, len(c.Candidates))
		for i, e := range c.Candidates {
			if e.ValidUntil != nil {
				e.ValidUntil = nil // un-close it: the correction was never recorded
			}
			newCands[i] = e
		}
		stripped.Candidates = newCands
		uncaptured = append(uncaptured, stripped)
	}

	enso := Run(EnsoModel{}, uncaptured)

	// With no captured corrections, the Ensō model can no longer guarantee the
	// current entry wins. It should NOT score a perfect 1.0 — the capture is
	// what was doing the work.
	if enso.Score() == 1.0 {
		t.Errorf("Ensō scored 1.0 with corrections stripped; capture was supposed to be load-bearing")
	}
	t.Logf("with corrections UNCAPTURED, enso precision@1 = %.2f (%d/%d) — capture is load-bearing",
		enso.Score(), enso.TopHits, enso.Total)
}

// TestBenchmark_DecayTiebreakWithinCurrent sanity-checks that when two entries
// are BOTH current (no supersession), the Ensō model falls back to decay-
// strength ranking via core.Rank, and a more-recently-referenced entry ranks
// above a long-untouched one. This guards the non-STALE path so future cases
// that exercise pure decay (not just supersession) have a tested foundation.
func TestBenchmark_DecayTiebreakWithinCurrent(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	stale := mustEntry("decay-old", core.TypeFact, "old untouched fact", old, nil, nil, nil)
	fresh := mustEntry("decay-fresh", core.TypeFact, "recently referenced fact", recent, nil, nil, nil)

	out := EnsoModel{}.Rank([]core.Entry{stale, fresh}, nil, now)
	if len(out) != 2 {
		t.Fatalf("expected 2 ranked, got %d", len(out))
	}
	if out[0].ID != fresh.ID {
		t.Errorf("expected recently-referenced entry to rank first by decay, got %s", out[0].ID)
	}
}
