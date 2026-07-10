package bench

import (
	"testing"
)

// held_out_test.go is the generalization probe: replay two real, never-
// processed Jun-25 STALE misses (Granola ban, LeanCTX scope) through the
// recall model to confirm that supersession-aware ranking generalizes beyond
// the seed cases it was built around.
//
// Given the pre-built supersession triples (the detection/capture layer is
// deferred to WP-5), does the Ensō model recover the current answer where the
// naive baseline surfaces the stale one? Both cases exercise the same math as
// the seed corpus on held-out data — this is the generalization gate for the
// consumption half of the recall loop.

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
