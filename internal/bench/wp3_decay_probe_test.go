package bench

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// TestWP3DecayEdgeIndependentContribution answers the frontier question the
// Jul-14 gate doc explicitly left open in its caveat:
//
//	"The full pipeline scoring 1.00 is partly a corpus-construction property:
//	 GitHistoryCases builds each pair with an explicit SUPERSEDES edge and a
//	 ValidUntil on the stale entry, so the filter always has the edge it needs.
//	 The corpus proves the filter uses the edge correctly; it does NOT prove
//	 the harder upstream problem — that the live system will reliably create
//	 that edge from an uncorrected, in-conversation status change. That capture
//	 problem is deliberately out of P1/WP-3 scope (ADR-001)."
//
// The gate separated recency (baseline, 0.??) from the edge (full, 1.00) and
// from specificity (0.57). It did NOT isolate DECAY as an independent signal.
// Decay is special: it is EDGE-INDEPENDENT. StrengthAt reads only LastRefTime
// (init = EncodedTime), so decay-based staleness suppression needs no capture
// layer, no detection, no correction — just the write timestamps every entry
// already has.
//
// The question: of the 34 same-subject cases specificity-only provably cannot
// break (both entries about the same subject → equal specificity → tie), how
// many can DECAY ALONE recover by simply letting the older entry age below the
// fresher one? That slice is staleness suppression the live system gets for
// FREE, without solving capture.
func TestWP3DecayEdgeIndependentContribution(t *testing.T) {
	if _, err := os.Stat(corpusPath); os.IsNotExist(err) {
		t.Skipf("corpus not found at %s — run cmd/corpus-builder first", corpusPath)
	}
	records, err := LoadGitHistoryRecords(corpusPath)
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	cases, err := GitHistoryCases(records)
	if err != nil {
		t.Fatalf("build cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("corpus is empty")
	}

	// Three edge-INDEPENDENT models (none consult SUPERSEDES/ValidUntil):
	//   - recency: newest EncodedTime wins (the current flat-file behavior)
	//   - specificity-only: query→content match, decay tiebreak, no filter
	//   - decay-only: leaky-integrator strength, no query, no filter
	// And the full pipeline (edge-DEPENDENT) as the ceiling.
	recency := Run(BaselineModel{}, cases)
	decayOnly := Run(DecayBlindModel{}, cases)
	specOnly := RunQueryAware(SpecificityBlindModel{}, cases)
	full := RunQueryAware(EnsoSpecificityModel{}, cases)

	t.Logf("\nWP-3 edge-independence probe (n=%d real supersession pairs):", len(cases))
	t.Logf("%-42s  %5s  %5s  %s", "Model", "P@1", "Score", "edge?")
	t.Logf("%-42s  %5s  %5s  %s", strings.Repeat("-", 42), "-----", "-----", "-----")
	t.Logf("%-42s  %d/%d  %.2f  %s", recency.Model, recency.TopHits, recency.Total, recency.Score(), "no")
	t.Logf("%-42s  %d/%d  %.2f  %s", specOnly.Model, specOnly.TopHits, specOnly.Total, specOnly.Score(), "no")
	t.Logf("%-42s  %d/%d  %.2f  %s", decayOnly.Model, decayOnly.TopHits, decayOnly.Total, decayOnly.Score(), "no")
	t.Logf("%-42s  %d/%d  %.2f  %s", full.Model, full.TopHits, full.Total, full.Score(), "YES")

	// --- The load-bearing breakdown: decay's contribution over specificity ----
	// Which cases does specificity-only fail? Of those, how many does decay-only
	// recover WITHOUT the edge? That is the free staleness suppression.
	specFail := failureSet(specOnly)
	decayFail := failureSet(decayOnly)

	var decayRescuesSpecMiss []string // spec fails, decay wins (edge-free rescue)
	var bothFail []string             // neither edge-free model wins → edge-only
	for name := range specFail {
		if decayFail[name] {
			bothFail = append(bothFail, name)
		} else {
			decayRescuesSpecMiss = append(decayRescuesSpecMiss, name)
		}
	}

	t.Logf("\n--- Decay's edge-INDEPENDENT contribution over specificity-only ---")
	t.Logf("specificity-only misses:                 %d", len(specFail))
	t.Logf("  ...of those, decay-only RECOVERS:       %d  (free staleness suppression)", len(decayRescuesSpecMiss))
	t.Logf("  ...of those, BOTH still miss:           %d  (edge-only — needs capture)", len(bothFail))

	// How many cases require the edge, period (no edge-free model gets them)?
	recencyFail := failureSet(recency)
	edgeOnlyCount := 0
	var edgeOnly []string
	for _, c := range cases {
		if specFail[c.Name] && decayFail[c.Name] && recencyFail[c.Name] {
			edgeOnlyCount++
			edgeOnly = append(edgeOnly, c.Name)
		}
	}
	t.Logf("\ncases NO edge-free model recovers (recency+spec+decay all miss): %d / %d",
		edgeOnlyCount, len(cases))
	t.Logf("  → this is the true, irreducible capture-dependent gap WP-3's edge must close")

	// --- Diagnostic: WHY do the edge-only cases resist decay? ------------------
	// Hypothesis: same-day supersessions (stale_date == current_date) are
	// decay-indistinguishable because both entries have identical LastRefTime,
	// so StrengthAt is equal and the tie can only be broken by the edge.
	sameDayEdgeOnly := 0
	for _, c := range cases {
		if !(specFail[c.Name] && decayFail[c.Name]) {
			continue
		}
		if len(c.Candidates) == 2 {
			a, b := c.Candidates[0], c.Candidates[1]
			if a.EncodedTime.Equal(b.EncodedTime) {
				sameDayEdgeOnly++
			}
		}
	}
	t.Logf("\ndiagnostic: of the edge-only (spec+decay both miss) cases, %d are same-day",
		sameDayEdgeOnly)
	t.Logf("  (stale_date == current_date → identical LastRefTime → decay CANNOT break the tie)")

	if testing.Verbose() && len(decayRescuesSpecMiss) > 0 {
		t.Logf("\ndecay-only rescues (specificity missed, decay recovered edge-free):")
		for _, n := range decayRescuesSpecMiss {
			t.Logf("  RESCUE %s", n)
		}
	}

	// --- Invariants (the pins that make this a regression guard, not a probe) --

	// I1: The full (edge-aware) pipeline is the ceiling. No edge-free model may
	//     beat it — the filter can only drop the stale distractor, never hurt.
	for _, r := range []Result{recency, specOnly, decayOnly} {
		if r.Score() > full.Score() {
			t.Errorf("edge-free model %q (%.2f) beat full pipeline (%.2f) — impossible; "+
				"the supersession filter can only help", r.Model, r.Score(), full.Score())
		}
	}

	// I2: Decay contributes SOMETHING edge-free that specificity does not, OR it
	//     provably cannot (all spec-misses are same-day). Either outcome is a
	//     real finding; a silent zero with no same-day explanation is the only
	//     thing that would mean the probe is measuring nothing.
	if len(decayRescuesSpecMiss) == 0 && sameDayEdgeOnly == 0 && len(specFail) > 0 {
		t.Errorf("decay rescued 0 spec-misses AND none are same-day — "+
			"decay is contributing nothing edge-free and there is no structural reason why; "+
			"the probe or the decay math needs investigation (spec misses=%d)", len(specFail))
	}
}

// failureSet returns the set of case names a Result got wrong, for set algebra.
func failureSet(r Result) map[string]bool {
	s := make(map[string]bool, len(r.Failures))
	for _, n := range r.Failures {
		s[n] = true
	}
	return s
}

// TestWP3DecayMonotoneOnDistinctDates is the mechanism check behind the probe:
// on a synthetic same-subject pair with DISTINCT encode dates and no edge, the
// fresher entry MUST have higher decay strength (so decay-only ranks it first).
// This isolates the claim "decay recovers distinct-date same-subject cases" from
// corpus noise. If this ever fails, the decay math regressed.
func TestWP3DecayMonotoneOnDistinctDates(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// Two same-subject Facts, identical content shape, 10 days apart, NO edge.
	stale := mustEntry("wp3-decay-stale", core.TypeFact, "Omega project is in design phase",
		base, nil, []string{"git-history"}, nil)
	current := mustEntry("wp3-decay-current", core.TypeFact, "Omega project shipped to prod",
		base.AddDate(0, 0, 10), nil, []string{"git-history"}, nil)

	now := base.AddDate(0, 0, 11) // query one day after the fresher write
	ranked := DecayBlindModel{}.Rank([]core.Entry{stale, current}, nil, now)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked, got %d", len(ranked))
	}
	if ranked[0].ID != current.ID {
		t.Errorf("decay-only ranked stale first on distinct dates; expected fresher entry "+
			"to have higher strength (stale=%v current=%v)", stale.ID, current.ID)
	}
}
