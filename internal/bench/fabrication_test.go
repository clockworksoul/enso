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
