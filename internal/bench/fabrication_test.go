package bench

import (
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// TestFabrication_PrecisionAtOneIsBlind is the central, deliberately
// uncomfortable finding: the metric this entire benchmark is built on —
// precision@1 ranking — CANNOT detect a FABRICATION miss.
//
// We prove it by running both models over a corpus where the only matching
// entry is the correct (vague) anchor. Both models rank it first and "pass"
// with precision@1 = 1.0 — yet in real life the assistant fabricated "13
// months" and the miss happened. A green ranking score coexists with a real
// miss. That is the proof that ranking is the wrong axis for this class.
func TestFabrication_PrecisionAtOneIsBlind(t *testing.T) {
	fc := FabricationCases()
	if len(fc) == 0 {
		t.Fatal("no fabrication cases")
	}

	for _, c := range fc {
		// Build a precision@1 Case from the FabricationCase: the ONLY candidate
		// is the correct anchor (no fabricated distractor exists, because the
		// fabricated number was never an entry). WantID is the anchor itself.
		rankCase := Case{
			Name:       c.Name,
			MissClass:  c.MissClass,
			Query:      c.Query,
			AsOf:       c.AsOf,
			WantID:     c.Anchor.ID,
			Candidates: []core.Entry{c.Anchor},
		}

		baseline := Run(BaselineModel{}, []Case{rankCase})
		enso := Run(EnsoModel{}, []Case{rankCase})

		// Both models "pass" precision@1 — they rank the only entry first.
		if baseline.Score() != 1.0 || enso.Score() != 1.0 {
			t.Errorf("%s: expected both models to score 1.0 (only one candidate), got baseline=%.2f enso=%.2f",
				c.Name, baseline.Score(), enso.Score())
		}

		t.Logf("%s: precision@1 = 1.00 for BOTH models, yet the real miss (fabricated %q, truth %q) still occurred. Ranking is blind to FABRICATION.",
			c.Name, c.FabricatedAnswer, c.TrueAnswer)
	}
}

// shouldAbstain is a minimal, honest abstention heuristic for the FABRICATION
// axis. It does NOT try to rank; it asks a different question: "is the support
// for a PRECISE answer strong enough to assert one, or should the system hedge
// or abstain?"
//
// It abstains when ANY of these hold for the best matching entry:
//   - no entry directly supports a precise answer (preciseSupport == false), or
//   - the only support is low-confidence (a soft estimate), or
//   - the only support is temporally stale (decay strength below a floor),
//     because an old soft estimate extrapolated to "now" is exactly the
//     fabrication trap.
//
// This is deliberately simple. The point is not a polished abstention model;
// it is to show that a SECOND signal — orthogonal to ranking — catches the case
// that precision@1 cannot. Validation before construction: we measure whether
// such a signal even discriminates before building anything elaborate.
func shouldAbstain(anchor core.Entry, preciseSupport bool, now time.Time) (bool, string) {
	if !preciseSupport {
		return true, "no entry directly supports a precise answer (only a vague anchor)"
	}
	if anchor.Confidence == core.ConfLow || anchor.Confidence == core.ConfMedium {
		return true, "only support is a low/medium-confidence soft estimate"
	}
	const staleFloor = 0.35
	if s := core.StrengthAt(anchor, now); s < staleFloor {
		return true, "only support is temporally stale (decayed below floor)"
	}
	return false, "precise, high-confidence, fresh support exists"
}

// TestFabrication_MarginSignalCatchesIt is the constructive half: it shows a
// margin/abstention signal — orthogonal to precision@1 — DOES discriminate the
// FABRICATION case. On the Tipa case the correct behavior is to ABSTAIN from a
// precise number (and hedge: "~9-10 months, soft"), which the heuristic does
// because no precise support exists and the anchor is a medium-confidence soft
// estimate.
//
// This is the Stage-6 (abstention) target made concrete: a number the benchmark
// can move. A future "fabrication defense" change is judged by whether it raises
// the abstain-correctly rate on this corpus without abstaining on cases that DO
// have precise support.
func TestFabrication_MarginSignalCatchesIt(t *testing.T) {
	for _, c := range FabricationCases() {
		abstain, why := shouldAbstain(c.Anchor, c.PreciseSupportExists, c.AsOf)
		if !abstain {
			t.Errorf("%s: abstention signal FAILED to fire; it should have abstained from the fabricated precise answer. reason=%q", c.Name, why)
		}
		t.Logf("%s: ABSTAIN=%v (%s) — the disciplined answer is to hedge to %q, not assert %q",
			c.Name, abstain, why, c.TrueAnswer, c.FabricatedAnswer)
	}
}

// TestFabrication_AbstentionDoesNotOverfire guards the obvious failure mode of
// any abstention layer: refusing to answer when good support DOES exist. A
// precise, high-confidence, fresh anchor must NOT trigger abstention, or the
// signal is useless (it would just suppress every answer). This is the
// false-positive guard that keeps the margin axis honest.
func TestFabrication_AbstentionDoesNotOverfire(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)

	// A precise, high-confidence, freshly-referenced fact: an exact hire date.
	// This is the kind of support that SHOULD license a precise answer.
	hire := mustEntry(
		"tipa-hire-date-exact",
		core.TypeFact,
		"Tipa was hired Sept 2, 2025 (exact start date from offer record).",
		now.Add(-2*time.Hour), // freshly referenced
		nil,
		[]string{"work", "team"},
		[]string{"person:tipa"},
	)
	hire.Confidence = core.ConfHigh

	abstain, why := shouldAbstain(hire, true /* precise support exists */, now)
	if abstain {
		t.Errorf("abstention OVER-fired on a precise/high-confidence/fresh anchor (reason=%q); it would suppress legitimate answers", why)
	}
	t.Logf("precise+high-confidence+fresh anchor → ABSTAIN=%v (%s) — correctly answers instead of suppressing", abstain, why)
}

// --- Staged cases (not yet in FabricationCases; candidates for Jul 13 review) ---

// TestStagedFabrication_NeoBlogOutline_PrecisionAtOneIsBlind mirrors
// TestFabrication_PrecisionAtOneIsBlind for the staged Jul 3 case. The corpus
// has exactly one matching entry (the Jun 22 anchor, specific and high-confidence
// as-of-then) and a precision@1 ranker scores 1.0 — yet the fabrication still
// happened in real life. Ranking cannot detect this case because the anchor IS
// the correct, specific entry; it's just 11 days stale.
func TestStagedFabrication_NeoBlogOutline_PrecisionAtOneIsBlind(t *testing.T) {
	for _, c := range StagedFabricationCases() {
		rankCase := Case{
			Name:       c.Name,
			MissClass:  c.MissClass,
			Query:      c.Query,
			AsOf:       c.AsOf,
			WantID:     c.Anchor.ID,
			Candidates: []core.Entry{c.Anchor},
		}
		baseline := Run(BaselineModel{}, []Case{rankCase})
		enso := Run(EnsoModel{}, []Case{rankCase})
		if baseline.Score() != 1.0 || enso.Score() != 1.0 {
			t.Errorf("%s: expected both models to score 1.0, got baseline=%.2f enso=%.2f",
				c.Name, baseline.Score(), enso.Score())
		}
		t.Logf("%s: precision@1 = 1.00 for BOTH models — the anchor is correct and specific; staleness is the only signal. Fabricated: %q. Truth: %q.",
			c.Name, c.FabricatedAnswer[:60], c.TrueAnswer[:60])
	}
}

// TestStagedFabrication_NeoBlogOutline_OnlyBranch3Fires is the key structural
// finding for this case: the shouldAbstain heuristic fires via branch 3
// (temporal decay) ONLY. Branches 1 and 2 do NOT fire because the anchor IS
// specific (PreciseSupportExists=true) and IS high-confidence (ConfHigh) — it
// was a real, precise, reliable entry as of Jun 22. The staleness after 11 days
// (StrengthAt ≈ 0.1 < staleFloor 0.35) is the sole discriminating signal.
// This validates branch 3 for the first time; Tipa validated branches 1+2.
func TestStagedFabrication_NeoBlogOutline_OnlyBranch3Fires(t *testing.T) {
	for _, c := range StagedFabricationCases() {
		// Branch 1 should NOT fire: precise support exists.
		if !c.PreciseSupportExists {
			t.Errorf("%s: expected PreciseSupportExists=true (branch 1 should not fire), got false", c.Name)
		}

		// Branch 2 should NOT fire: anchor is high-confidence.
		if c.Anchor.Confidence != core.ConfHigh {
			t.Errorf("%s: expected ConfHigh anchor (branch 2 should not fire), got %q", c.Name, c.Anchor.Confidence)
		}

		// Branch 3 SHOULD fire: temporal decay below staleFloor.
		strength := core.StrengthAt(c.Anchor, c.AsOf)
		const staleFloor = 0.35
		if strength >= staleFloor {
			t.Errorf("%s: expected StrengthAt < %.2f (branch 3 should fire), got %.4f", c.Name, staleFloor, strength)
		}

		// Full abstention check: should abstain for the right reason.
		abstain, why := shouldAbstain(c.Anchor, c.PreciseSupportExists, c.AsOf)
		if !abstain {
			t.Errorf("%s: abstention signal failed to fire; should have caught the stale narrative confabulation. reason=%q", c.Name, why)
		}
		t.Logf("%s: branch1(no-precise-support)=false branch2(low-conf)=false branch3(decay)=true strength=%.4f → ABSTAIN=%v (%s)",
			c.Name, strength, abstain, why)
	}
}

// TestStagedFabrication_NeoBlogOutline_SubtypeContrast documents the structural
// contrast between the two FABRICATION subtypes. Both should abstain; they just
// get there via different branches. This is a documentation test more than a
// correctness check — it makes the contrast visible in the test output.
func TestStagedFabrication_NeoBlogOutline_SubtypeContrast(t *testing.T) {
	live := FabricationCases()         // Tipa-tenure: branches 1+2
	staged := StagedFabricationCases() // Neo4j outline: branch 3 only

	for _, c := range live {
		abstain, why := shouldAbstain(c.Anchor, c.PreciseSupportExists, c.AsOf)
		t.Logf("LIVE   %s: PreciseSupport=%v Confidence=%q Strength=%.4f → ABSTAIN=%v (%s)",
			c.Name, c.PreciseSupportExists, c.Anchor.Confidence, core.StrengthAt(c.Anchor, c.AsOf), abstain, why)
	}
	for _, c := range staged {
		abstain, why := shouldAbstain(c.Anchor, c.PreciseSupportExists, c.AsOf)
		t.Logf("STAGED %s: PreciseSupport=%v Confidence=%q Strength=%.4f → ABSTAIN=%v (%s)",
			c.Name, c.PreciseSupportExists, c.Anchor.Confidence, core.StrengthAt(c.Anchor, c.AsOf), abstain, why)
	}
}
