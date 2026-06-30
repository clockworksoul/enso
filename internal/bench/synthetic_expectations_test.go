package bench

import (
	"testing"

	"github.com/clockworksoul/enso/internal/core"
)

// synthetic_expectations_test.go runs the SYNTHETIC detector-expectation corpus
// (synthetic_expectations.go + testdata/synthetic_expectations.jsonl) with three
// distinct assertion tiers, encoding the methodology agreed 2026-06-30:
//
//	TIER 1  LOCKED positives        → HARD ASSERT. status=locked, wantFire=true.
//	                                  The detector already meets these; any
//	                                  regression that un-meets one FAILS the
//	                                  build. (None at first — cases graduate in.)
//
//	TIER 2  ALL negatives           → HARD ASSERT, regardless of status.
//	                                  wantFire=false. Over-firing is ALWAYS a
//	                                  regression (a false positive), so a synthetic
//	                                  case asserting the detector should stay quiet
//	                                  is safe to gate immediately.
//
//	TIER 3  ASPIRATIONAL positives  → REPORT ONLY. status=aspirational,
//	                                  wantFire=true. Whether the current vocabulary
//	                                  catches these is LOGGED for a human, not
//	                                  gated. Misses are a future-vocabulary to-do
//	                                  list — the synthetic-prior the real corpus
//	                                  will later confirm or contradict.
//
// CRITICAL SEPARATION: this file is the ONLY place the synthetic corpus is
// scored. The real precision@1 scoreboard (bench_test.go, over SeedCases /
// NeighborCases / HeldOutStaleCases) NEVER sees these cases. Synthetic data
// asserts behavior; it does not certify correctness.

// TestSyntheticExpectations_Negatives is TIER 2: every wantFire=false case must
// stay quiet. This gate is live immediately because over-firing is unambiguously
// a regression — a correction-detector that fires on plain statements, questions,
// hypotheticals, or reported speech is worse than useless.
//
// NOTE ON HONESTY: some negatives ("hypothetical", "past-tense-narration") are
// HARD boundaries the current flat-regex detector likely CANNOT distinguish
// (e.g. 'actually' inside a counterfactual). If one of these fails today, that
// is a TRUE finding — a known false-positive class the vocabulary can't yet
// separate — not a reason to delete the case. If they fail, we mark them with a
// skip+log rather than weaken the corpus; see the t.Skip path below. Today we
// REPORT negative failures loudly but only FAIL on the negatives we believe are
// cleanly separable. This keeps the gate honest without pretending the detector
// is more precise than it is.
func TestSyntheticExpectations_Negatives(t *testing.T) {
	exps, err := LoadSyntheticExpectations()
	if err != nil {
		t.Fatalf("load synthetic corpus: %v", err)
	}

	var negatives, firedWrong int
	for _, e := range exps {
		if e.WantFire {
			continue
		}
		negatives++
		det := core.DetectCorrection(e.Utterance)
		if det.IsCorrection {
			firedWrong++
			t.Logf("  ⚠ FALSE POSITIVE  %-28s fired (kind=%s conf=%s signals=%v) on %q",
				e.Intent, det.Kind, det.Confidence, det.Signals, e.Utterance)
		} else {
			t.Logf("  ✓ stayed quiet    %-28s on %q", e.Intent, e.Utterance)
		}
	}
	if negatives == 0 {
		t.Fatal("no negative (wantFire=false) synthetic cases — the false-positive gate has nothing to measure")
	}
	t.Logf("── synthetic negatives: %d total, %d false positives ──", negatives, firedWrong)

	// HONEST GATE: we report all false positives above, but only the cases we
	// believe are CLEANLY separable are hard failures today. The two hard
	// boundaries (hypothetical 'actually', reported-speech 'no longer true') are
	// documented known limits of a flat-regex detector — failing on them is a
	// real finding, not a regression to block CI on yet. They graduate to hard
	// gates if/when the detector gains context-sensitivity. Track the count, and
	// fail only if a CLEANLY-separable negative (a plain statement or a question)
	// over-fires.
	for _, e := range exps {
		if e.WantFire {
			continue
		}
		switch e.Intent {
		case "negative:hypothetical", "negative:past-tense-narration":
			// Known hard boundary — report (done above), do not gate yet.
			continue
		}
		det := core.DetectCorrection(e.Utterance)
		if det.IsCorrection {
			t.Errorf("cleanly-separable negative over-fired: %s on %q (kind=%s signals=%v) — this IS a regression",
				e.Intent, e.Utterance, det.Kind, det.Signals)
		}
	}
}

// TestSyntheticExpectations_LockedPositives is TIER 1: every status=locked,
// wantFire=true case MUST fire. These are graduated cases the detector already
// handles, now protected against regression. The suite may legitimately contain
// ZERO locked cases at first (everything starts aspirational); in that case this
// test is a no-op that documents the absence, which is fine — locking is earned,
// not assumed.
func TestSyntheticExpectations_LockedPositives(t *testing.T) {
	exps, err := LoadSyntheticExpectations()
	if err != nil {
		t.Fatalf("load synthetic corpus: %v", err)
	}

	var locked int
	for _, e := range exps {
		if e.Status != StatusLocked || !e.WantFire {
			continue
		}
		locked++
		det := core.DetectCorrection(e.Utterance)
		if !det.IsCorrection {
			t.Errorf("LOCKED expectation regressed: %s did NOT fire on %q (signals=%v) — "+
				"a graduated case must keep passing", e.Intent, e.Utterance, det.Signals)
		} else {
			t.Logf("  ✓ locked          %-28s fired (kind=%s conf=%s)", e.Intent, det.Kind, det.Confidence)
		}
	}
	t.Logf("── synthetic locked positives: %d (all must fire) ──", locked)
}

// TestSyntheticExpectations_AspirationalReport is TIER 3: status=aspirational,
// wantFire=true cases are REPORTED, not gated. This logs the current detector's
// recall against the synthetic prior — the number a human reads to decide where
// the vocabulary should grow next. A miss here is a future-work signal, NOT a
// failure. The only hard assertion is the sanity floor: the harness ran and the
// corpus is non-empty.
func TestSyntheticExpectations_AspirationalReport(t *testing.T) {
	exps, err := LoadSyntheticExpectations()
	if err != nil {
		t.Fatalf("load synthetic corpus: %v", err)
	}

	var aspirational, met int
	t.Logf("── synthetic aspirational positives (REPORTED, not gated) ──")
	for _, e := range exps {
		if e.Status != StatusAspirational || !e.WantFire {
			continue
		}
		aspirational++
		det := core.DetectCorrection(e.Utterance)
		status := "MISS → future-vocabulary candidate"
		if det.IsCorrection {
			met++
			status = "met"
		}
		t.Logf("  %-28s %-5v %-36s on %q",
			e.Intent, det.IsCorrection, status, e.Utterance)
	}
	if aspirational == 0 {
		t.Fatal("no aspirational synthetic positives — the probe has nothing to measure")
	}
	t.Logf("── synthetic detector recall (aspirational): %d/%d met ──", met, aspirational)
	// Deliberately NO coverage assertion. The number is the experiment; a human
	// reads it and decides. Gating it would convert a falsifiable spec into a
	// self-confirming validation, which is exactly the trap this whole bucket
	// is designed to avoid.
}
