package bench

import (
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// WP-3 seam: decay vs recency DIVERGENCE.
//
// The Jul-15 edge-independence probe (docs/2026-07-15-...) ended on an honest
// admission: on the git-history replay corpus, decay-only and recency both
// score 0.63 and recover the SAME distinct-date cases. They coincide because
// the replay is static — every entry's LastRefTime is frozen at EncodedTime, so
// "highest decay strength" and "newest write" are the same ordering. The doc
// named the frontier and stopped:
//
//	"Decay and recency diverge the moment LastRefTime is bumped by a material
//	 recall (RECALL-DEF) rather than tracking EncodedTime; on this replay corpus
//	 no recall bumps fire, so they coincide. The value of decay over recency
//	 shows up only once the recall bump is live (P3) ... Build the case before
//	 the layer."
//
// This file builds that case. It is the FALSIFIABLE measurement that decay is
// not merely a fancy recency proxy: a scenario where an OLDER but materially
// recalled entry SHOULD outrank a NEWER but never-recalled entry, and where
// recency (write-order) provably gets it wrong while decay-with-recall gets it
// right.
//
// Discipline note (validate before build): this does NOT wire BumpOnRecall into
// any production recall path. It measures — on a hand-built but semantically
// real scenario — whether the P3 recall bump would earn its cost by beating
// recency on a case recency cannot get. The bump math (core.BumpOnRecall)
// already exists and is unit-tested; what was missing was a benchmark case that
// distinguishes it from recency at all. Without such a case, "decay > recency"
// is an untested claim. This makes it testable.

// recallEvent records that an entry was materially recalled (RECALL-DEF:
// surfaced AND used in a reply) at a specific time. It is the offline stand-in
// for the live recall signal P3 will consume.
type recallEvent struct {
	id core.ID
	at time.Time
}

// RecallBumpModel ranks by leaky-integrator decay strength AFTER applying a
// set of material-recall bumps to the candidates. It is the ONLY model in the
// harness whose ranking can differ from write-order, because BumpOnRecall moves
// an entry's LastRefTime forward past its EncodedTime. Without any recall events
// it is byte-identical to DecayBlindModel (and therefore, on this replay,
// identical to recency) — the divergence is entirely carried by the bumps.
type RecallBumpModel struct {
	recalls []recallEvent
}

func (RecallBumpModel) Name() string { return "decay+recall-bump (RECALL-DEF)" }

func (m RecallBumpModel) Rank(candidates []core.Entry, _ []core.Edge, now time.Time) []core.Entry {
	// Apply material-recall bumps. BumpOnRecall is non-destructive; we build a
	// bumped copy of the candidate slice, applying every recall event whose time
	// is <= now (a recall that has not happened yet cannot affect this query).
	bumped := make([]core.Entry, len(candidates))
	copy(bumped, candidates)
	for _, ev := range m.recalls {
		if ev.at.After(now) {
			continue
		}
		for i := range bumped {
			if bumped[i].ID == ev.id {
				params := core.DefaultRecallParams(bumped[i].Type)
				bumped[i] = core.BumpOnRecall(bumped[i], ev.at, params)
			}
		}
	}
	ranked := core.Rank(bumped, now)
	out := make([]core.Entry, len(ranked))
	for i, r := range ranked {
		out[i] = r.Entry
	}
	return out
}

// TestWP3RecallBumpDivergesFromRecency is the seam the Jul-15 probe named. It
// constructs the scenario recency CANNOT get right and decay-with-recall CAN,
// proving the two signals are genuinely distinct — not the coincidence the
// static replay makes them look like.
//
// Scenario (a real recall shape, not a supersession):
//
//   - ENTRY A ("load-bearing", older): a durable, frequently-used fact written
//     long ago and materially recalled many times since — e.g. a core
//     architectural invariant Matt asks about repeatedly ("Markdown is
//     canonical, the graph is a derived cache"). It is OLD by write time but
//     HOT by usage.
//   - ENTRY B ("write-only", newer): a fact written more recently but never
//     materially recalled — e.g. a one-off note captured during a consolidation
//     pass and never referenced again ("the Jul-2 architecture-sync had 6
//     attendees"). It is NEW by write time but COLD by usage.
//
// Both plausibly match a query about "the enso architecture." Recency ranks B
// first (newer write). But the RIGHT answer for a relevance-ranked recall is A:
// it is the durable, repeatedly-used memory, exactly the thing a human's
// spacing-strengthened memory surfaces first. Decay-with-recall-bump ranks A
// first because its many spaced recalls have raised S_floor and refreshed
// LastRefTime past B's untouched EncodedTime.
//
// This is NOT a staleness/supersession case — neither entry is wrong or stale.
// It isolates the pure recency-vs-strength distinction the corpus otherwise
// cannot exercise.
func TestWP3RecallBumpDivergesFromRecency(t *testing.T) {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	// A: written Apr 1, materially recalled ~weekly through late June.
	entryA := mustEntry("wp3-loadbearing-invariant", core.TypeFact,
		"Ensō invariant: Markdown is canonical; the graph index is a derived cache",
		base, nil, []string{"enso", "architecture", "invariant"},
		[]string{"project:enso"})

	// B: written Jun 25 (newer than A's WRITE), never materially recalled.
	writeB := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	entryB := mustEntry("wp3-writeonly-note", core.TypeFact,
		"The Jul-2 enso architecture sync had six attendees",
		writeB, nil, []string{"enso", "architecture", "meeting"},
		[]string{"project:enso"})

	// A is recalled weekly from Apr 8 through Jun 24 — spaced, material recalls.
	var recalls []recallEvent
	for wk := base.AddDate(0, 0, 7); wk.Before(writeB); wk = wk.AddDate(0, 0, 7) {
		recalls = append(recalls, recallEvent{id: entryA.ID, at: wk})
	}

	// Query time: Jun 26, one day after B was written and two days after A's
	// last recall. This is the instant recency and decay disagree.
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	candidates := []core.Entry{entryA, entryB}

	// --- Recency gets it WRONG: B is the newest write. --------------------------
	recencyRanked := BaselineModel{}.Rank(candidates, nil, now)
	if recencyRanked[0].ID != entryB.ID {
		t.Fatalf("scenario invalid: recency should rank the newer write (B) first, got %v; "+
			"if this fails the test is not exercising the divergence it claims", recencyRanked[0].ID)
	}

	// --- Decay WITHOUT the recall bump ALSO gets it wrong (== recency here). -----
	// This is the crucial control: it proves the corpus-static claim from Jul-15
	// (decay coincides with recency when no bump fires). If decay-blind already
	// picked A, the divergence would be an artifact of decay params, not recall.
	decayBlindRanked := DecayBlindModel{}.Rank(candidates, nil, now)
	if decayBlindRanked[0].ID != entryB.ID {
		t.Fatalf("control failed: decay-WITHOUT-recall should coincide with recency (pick B), "+
			"got %v; the Jul-15 coincidence claim would be false and this whole seam "+
			"mis-stated", decayBlindRanked[0].ID)
	}

	// --- Decay WITH the recall bump gets it RIGHT: A wins. ----------------------
	// This is the entire point: the material recalls raised A's strength above
	// B's untouched write, so the durable-and-used memory outranks the
	// fresh-but-cold one. Recency provably cannot produce this ordering.
	bumped := RecallBumpModel{recalls: recalls}.Rank(candidates, nil, now)
	if bumped[0].ID != entryA.ID {
		sa := core.StrengthAt(applyRecalls(entryA, recalls, now), now)
		sb := core.StrengthAt(entryB, now)
		t.Errorf("decay+recall-bump ranked %v first; expected the recalled-durable entry A. "+
			"strength A=%.4f B=%.4f — the recall bump did not lift A above B; "+
			"check DefaultRecallParams(TypeFact) Alpha/Tau or the bump math",
			bumped[0].ID, sa, sb)
	}

	// --- The load-bearing numeric assertion: A's strength EXCEEDS B's. ----------
	// Pin the mechanism, not just the ordering, so a future params change that
	// silently collapses the divergence fails loudly here.
	sa := core.StrengthAt(applyRecalls(entryA, recalls, now), now)
	sb := core.StrengthAt(entryB, now)
	if !(sa > sb) {
		t.Errorf("expected recalled-durable A strength (%.4f) > cold-fresh B strength (%.4f)", sa, sb)
	}
	t.Logf("WP-3 recall-bump divergence (n=1 constructed scenario):")
	t.Logf("  recency          → B first (newer write)   [WRONG for relevance recall]")
	t.Logf("  decay (no bump)   → B first (== recency)    [control: coincides, as Jul-15 claims]")
	t.Logf("  decay + %d recalls → A first (strength %.4f > B %.4f)   [RIGHT: durable+used]",
		len(recalls), sa, sb)
}

// TestWP3RecallBumpNoOpWithoutEvents pins the safety property that makes the
// divergence attributable to the recall signal alone: with ZERO recall events,
// RecallBumpModel is identical to DecayBlindModel on the same candidates. If
// this ever fails, RecallBumpModel is introducing an ordering effect that is
// NOT the recall bump, and every divergence result above is contaminated.
func TestWP3RecallBumpNoOpWithoutEvents(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	e1 := mustEntry("wp3-noop-old", core.TypeFact, "old fact",
		base, nil, []string{"enso"}, nil)
	e2 := mustEntry("wp3-noop-new", core.TypeFact, "new fact",
		base.AddDate(0, 0, 5), nil, []string{"enso"}, nil)
	now := base.AddDate(0, 0, 6)
	cands := []core.Entry{e1, e2}

	noBump := RecallBumpModel{}.Rank(cands, nil, now) // no recall events
	decayBlind := DecayBlindModel{}.Rank(cands, nil, now)
	if len(noBump) != len(decayBlind) {
		t.Fatalf("length mismatch: %d vs %d", len(noBump), len(decayBlind))
	}
	for i := range noBump {
		if noBump[i].ID != decayBlind[i].ID {
			t.Errorf("position %d: RecallBumpModel(no events)=%v but DecayBlindModel=%v — "+
				"RecallBumpModel is not a no-op without recall events; divergence results "+
				"are contaminated by a non-recall effect", i, noBump[i].ID, decayBlind[i].ID)
		}
	}
}

// TestWP3RecallBumpSpacingMatters pins the SECOND thing decay buys over recency:
// the spacing effect (Bjork's desirable difficulty). Two entries recalled the
// SAME number of times but with different spacing must NOT end up equal —
// spaced recalls consolidate more durably than massed ones. Recency is blind to
// this entirely (it sees only the last write). This proves decay+recall carries
// information recency structurally cannot represent.
func TestWP3RecallBumpSpacingMatters(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// spacedEntry: 3 recalls, well spaced (7 days apart).
	spacedEntry := mustEntry("wp3-spaced", core.TypeFact, "spaced fact",
		base, nil, []string{"enso"}, nil)
	spaced := []recallEvent{
		{spacedEntry.ID, base.AddDate(0, 0, 7)},
		{spacedEntry.ID, base.AddDate(0, 0, 14)},
		{spacedEntry.ID, base.AddDate(0, 0, 21)},
	}

	// massedEntry: 3 recalls, all bunched at the start (1 day apart).
	massedEntry := mustEntry("wp3-massed", core.TypeFact, "massed fact",
		base, nil, []string{"enso"}, nil)
	massed := []recallEvent{
		{massedEntry.ID, base.AddDate(0, 0, 1)},
		{massedEntry.ID, base.AddDate(0, 0, 2)},
		{massedEntry.ID, base.AddDate(0, 0, 3)},
	}

	// Evaluate both at a far-out time so decay from their LAST recall dominates
	// and S_floor (the consolidation creep) carries the spacing difference.
	now := base.AddDate(0, 0, 60)

	spacedStrength := core.StrengthAt(applyRecalls(spacedEntry, spaced, now), now)
	massedStrength := core.StrengthAt(applyRecalls(massedEntry, massed, now), now)

	// Spaced recalls should consolidate S_floor higher → higher long-run
	// strength. (The last spaced recall is also later, but the mechanism under
	// test is the floor creep; both are decay effects recency cannot see.)
	if !(spacedStrength > massedStrength) {
		t.Errorf("spaced recalls (%.4f) should consolidate higher long-run strength than "+
			"massed recalls (%.4f) — the spacing multiplier (1-e^(-Δt/τ)) is not producing "+
			"desirable-difficulty consolidation", spacedStrength, massedStrength)
	}
	t.Logf("spacing effect: spaced S=%.4f > massed S=%.4f (recency sees neither)",
		spacedStrength, massedStrength)
}

// applyRecalls is a test helper: apply a sequence of material recalls to an
// entry in event order and return the bumped result, for direct strength
// inspection. Mirrors RecallBumpModel's inner loop.
func applyRecalls(e core.Entry, events []recallEvent, now time.Time) core.Entry {
	out := e
	for _, ev := range events {
		if ev.at.After(now) || ev.id != e.ID {
			continue
		}
		params := core.DefaultRecallParams(out.Type)
		out = core.BumpOnRecall(out, ev.at, params)
	}
	return out
}
