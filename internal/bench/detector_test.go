package bench

import (
	"context"
	"testing"

	"github.com/clockworksoul/enso/internal/confirm"
	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/memstore"
)

// The DETECTOR replay answers a different question than the recall benchmark.
//
// The recall benchmark (bench_test.go) assumes the SUPERSEDES edge already
// exists and asks: given the edge, does the Ensō model rank the current entry
// first? It proved YES (3/3). But that whole result is conditional on the edge
// existing — i.e. on the correction having been CAPTURED from real language in
// the first place. Nothing tested that link until now.
//
// This is the validation step the "complexity kills" conversation kept pointing
// at: replay the REAL correction utterances through the REAL detector
// (core.DetectCorrection, via confirm.Propose) and measure two things per case:
//
//	(1) FIRE:    did DetectCorrection recognize a correction at all?
//	(2) RESOLVE: did target resolution return any candidate to act on?
//
// It writes nothing and decides nothing. It is a measurement.
//
// KNOWN LIMITATION (diagnosed 2026-06-26): the current seed fixtures encode the
// world AFTER the correction already closed the stale entry, so RESOLVE here can
// only ever see the already-current head. A TRUE end-to-end capture test needs
// PRE-correction fixtures (stale entry still open, no SUPERSEDES edge yet) so
// the detector+resolver have a real stale target to find and supersede. Building
// those fixtures is the documented next step; this harness deliberately stops
// short of faking it.
//
// HONEST SCOPE: only STALE/reframe misses carry an Utterance (someone stated a
// correction). NEIGHBOR misses had no correction utterance — the failure was
// confabulation, not a corrected belief — so there is nothing for a detector to
// detect. Those cases are reported as "no utterance," NOT as detector misses.
// That separation is itself a finding: a correction-detector is the wrong tool
// for the NEIGHBOR class by construction.

// detectorOutcome is one case's measured result.
type detectorOutcome struct {
	name       string
	missClass  string
	hasUtter   bool
	fired      bool // DetectCorrection said IsCorrection
	kind       core.CorrectionKind
	confidence core.DetectionConfidence
	resolved   bool // the stale target appeared among resolved candidates
}

// runDetector replays one case's utterance through the real detector + resolver.
func runDetector(t *testing.T, c Case) detectorOutcome {
	t.Helper()
	out := detectorOutcome{name: c.Name, missClass: c.MissClass, hasUtter: c.Utterance != ""}
	if !out.hasUtter {
		return out
	}

	// Seed a store with exactly the candidates that existed as of the query.
	store := memstore.New()
	if err := store.Append(context.Background(), c.Candidates, c.Edges); err != nil {
		t.Fatalf("%s: seed store: %v", c.Name, err)
	}

	prop, ok, err := confirm.Propose(
		context.Background(),
		confirm.StoreResolver{Store: store},
		c.Utterance,
		c.AsOf,
	)
	if err != nil {
		t.Fatalf("%s: propose: %v", c.Name, err)
	}
	out.fired = ok && prop.Detection.IsCorrection
	out.kind = prop.Detection.Kind
	out.confidence = prop.Detection.Confidence

	// RESOLVE: did target resolution surface ANY candidate the correction could
	// act on at all?
	//
	// IMPORTANT MEASUREMENT CAVEAT (diagnosed 2026-06-26): these fixtures encode
	// the world AFTER the correction already happened — adamHeadcountStale() and
	// edSandovalTimelineStale() call core.Entry.Correct(), which CLOSES the stale
	// entry (sets ValidUntil). So at AsOf the only CURRENT candidate is the
	// already-correct head, and StoreResolver (correctly) only returns current
	// entries — you cannot supersede something already closed. "Did a STALE
	// supersedable target appear" is therefore the wrong question for these
	// fixtures: the supersession is already history. We measure the weaker, true
	// thing — "did resolution return any candidate" — and leave end-to-end
	// capture (detect a correction against an OPEN stale entry, then supersede it)
	// to PRE-correction fixtures that don't exist yet. That fixture-shape decision
	// is the documented next step, not something to fake here.
	out.resolved = len(prop.Candidates) > 0
	return out
}

// TestDetector_ReplayMissLog is the experiment. It is intentionally NOT a
// pass/fail gate on detection quality (we do not yet know the right bar) — it
// is a reporting harness that prints the catch rate so a human reads the
// number and decides. The only hard assertions are sanity checks: the corpus
// has utterance-bearing cases, and the harness ran them.
func TestDetector_ReplayMissLog(t *testing.T) {
	cases := append(SeedCases(), NeighborCases()...)

	var withUtter, fired, resolved int
	t.Logf("── detector replay over %d cases ──", len(cases))
	for _, c := range cases {
		o := runDetector(t, c)
		if !o.hasUtter {
			t.Logf("  %-26s [%s] no utterance (not a detector case)", o.name, o.missClass)
			continue
		}
		withUtter++
		if o.fired {
			fired++
		}
		if o.resolved {
			resolved++
		}
		t.Logf("  %-26s [%s] fired=%-5v kind=%-8s conf=%-6s resolved=%v",
			o.name, o.missClass, o.fired, o.kind, o.confidence, o.resolved)
	}

	if withUtter == 0 {
		t.Fatal("no utterance-bearing cases — the detector replay has nothing to measure")
	}
	t.Logf("── detector catch rate: fired %d/%d, resolution-returned-candidate %d/%d ──",
		fired, withUtter, resolved, withUtter)

	// Sanity floor only: at least one real correction must trip the detector,
	// otherwise the sensor is non-functional and the whole capture path is dead.
	// This is deliberately a weak assertion — the EXPERIMENT is the logged
	// numbers above, read by a human, not this gate.
	if fired == 0 {
		t.Errorf("detector fired on 0/%d real correction utterances — sensor appears non-functional", withUtter)
	}
}
