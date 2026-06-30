package bench

import (
	"context"
	"testing"

	"github.com/clockworksoul/enso/internal/confirm"
	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/memstore"
)

// held_out_test.go is the GENERALIZATION probe promised by DROSS-TODO seam #4:
// replay the two real, never-processed Jun-25 informal STALE misses (Granola
// ban, LeanCTX scope) through the SAME recall model and the SAME detector the
// seed cases use, and measure whether the loop generalizes to correction
// language it was NOT tuned around.
//
// It splits cleanly into the two independent claims the loop makes:
//
//	(A) RECALL  — given the supersession edge, does the Ensō model recover the
//	              current answer where the naive baseline surfaces the stale one?
//	              This tests the consumption half and should PASS (the edge
//	              exists; this is the same math the seeds proved).
//
//	(B) DETECT  — does core.DetectCorrection (via confirm.Propose) fire on the
//	              real Jun-25 correction utterances? This tests the CAPTURE half
//	              on held-out language. The outcome is REPORTED, not gated on a
//	              quality bar we have not earned — a MISS here is a real finding
//	              about detector recall, not a test failure to paper over.

// --- (A) RECALL generalization ------------------------------------------------

// TestHeldOut_RecallGeneralizes confirms the consumption half generalizes: on
// the two held-out STALE misses, the Ensō (staleness+decay) model ranks the
// current entry first while the naive baseline is fooled by the re-scanned
// stale entry. This is the Phase-1 value claim, re-proven on cases the model
// was not built around.
func TestHeldOut_RecallGeneralizes(t *testing.T) {
	cases := HeldOutStaleCases()

	base := Run(BaselineModel{}, cases)
	enso := Run(EnsoModel{}, cases)

	t.Logf("── held-out STALE recall: %d cases ──", len(cases))
	t.Logf("  %-22s %d/%d", base.Model, base.TopHits, base.Total)
	t.Logf("  %-22s %d/%d", enso.Model, enso.TopHits, enso.Total)

	// The discriminating claim: Ensō solves all held-out STALE cases.
	if enso.TopHits != enso.Total {
		t.Errorf("Ensō model regressed on held-out STALE: %d/%d (failures: %v)",
			enso.TopHits, enso.Total, enso.Failures)
	}
	// And the baseline must be genuinely fooled — otherwise the cases are not
	// discriminating and prove nothing about supersession's value. Each stale
	// entry is re-scanned at query-adjacent time, so recency picks it.
	if base.TopHits != 0 {
		t.Errorf("baseline unexpectedly scored %d/%d on held-out STALE — cases are not discriminating "+
			"(the stale entry should look freshest by recency)", base.TopHits, base.Total)
	}
}

// --- (B) DETECT generalization (reported, not gated) --------------------------

// TestHeldOut_DetectorGeneralizes is the experiment. It replays the real Jun-25
// correction utterances through the real detector and REPORTS the catch rate.
// The seed cases were the detector's training set; these were not. The number
// this logs is the first honest signal of detector RECALL on held-out language.
//
// Hard assertions are deliberately minimal: the harness must run and the recall
// precondition (a current head exists to land on) must hold. Whether the
// detector fired is logged for a human to read and decide — per the same
// "report the number, don't fake a gate" stance as TestDetector_ReplayMissLog.
func TestHeldOut_DetectorGeneralizes(t *testing.T) {
	cases := HeldOutStaleCases()

	var fired, strong int
	t.Logf("── held-out detector replay over %d real Jun-25 STALE misses ──", len(cases))
	for _, c := range cases {
		if c.Utterance == "" {
			t.Fatalf("%s: held-out STALE case must carry an utterance", c.Name)
		}

		// Seed a store with the candidates as of the query, then run the REAL
		// detect+resolve path.
		store := memstore.New()
		if err := store.Append(context.Background(), c.Candidates, c.Edges); err != nil {
			t.Fatalf("%s: seed store: %v", c.Name, err)
		}
		_, ok, err := confirm.Propose(
			context.Background(),
			confirm.StoreResolver{Store: store},
			c.Utterance,
			c.AsOf,
		)
		if err != nil {
			t.Fatalf("%s: propose: %v", c.Name, err)
		}

		// Independent of confirm.Propose's surfacing policy, ask the raw detector
		// directly so we report DETECTION recall, not the policy gate.
		det := core.DetectCorrection(c.Utterance)

		if det.IsCorrection {
			fired++
		}
		if det.Confidence == core.DetectStrong {
			strong++
		}

		t.Logf("  %-20s fired=%-5v kind=%-8s conf=%-6s signals=%v | proposed=%v content=%q",
			c.Name, det.IsCorrection, det.Kind, det.Confidence, det.Signals, ok, truncate(det.Content, 48))
	}

	t.Logf("── held-out detector recall: fired %d/%d (strong %d/%d) ──",
		fired, len(cases), strong, len(cases))

	// FINDING-ORIENTED, not pass/fail: we EXPECT some of these held-out
	// utterances to be missed by the Jun-23-grounded vocabulary (they are bare
	// corrective assertions without explicit supersession markers). That is the
	// experiment's value. The only hard floor: the harness must have measured
	// real cases. Detector quality is read from the logged number, then decided.
	if len(cases) == 0 {
		t.Fatal("no held-out cases to measure")
	}
}

// truncate shortens a string for tidy test log output.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
