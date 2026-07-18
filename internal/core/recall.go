package core

import (
	"context"
	"fmt"
	"math"
	"time"
)

// RecallParams holds the tunable constants for the spacing-aware consolidation
// bump (tech spec §5.1). Both parameters are intentionally conservative Phase-1
// priors; empirical Phase-3 tuning will replace them once we have real recall
// data. All values are marked // TUNABLE so they are easy to find and adjust.
type RecallParams struct {
	// Alpha is the base consolidation rate: the maximum fraction of headroom
	// (S_cap − S_floor) that a single, fully-spaced recall can add to S_floor.
	// Constraint: 0 < Alpha ≤ 1. A value of 0.10 means one maximally-spaced
	// recall raises the floor by at most 10% of remaining headroom.
	Alpha float64 // TUNABLE — Phase-3 calibration target

	// Tau is the characteristic spacing timescale in hours.
	// The spacing multiplier f(Δt) = 1 − e^(−Δt/Tau) reaches ≈63% of its
	// maximum at Δt = Tau, and ≈95% at Δt = 3·Tau. A Tau of 24h means that
	// "one day between recalls" counts as one full spacing unit.
	Tau float64 // TUNABLE — Phase-3 calibration target
}

// DefaultRecallParams returns defensible Phase-1 priors for RecallParams by
// NodeType. Values mirror the per-type Lambda rationale from DefaultTemporal:
// volatile types (Task) use tighter spacing and faster consolidation; stable
// semantic types use longer spacing aligned with episodic consolidation timescales.
//
// All numeric literals are Phase-3 calibration targets marked // TUNABLE.
func DefaultRecallParams(nt NodeType) RecallParams {
	switch nt {
	case TypeTask:
		// Tasks are hot information: access daily, consolidate quickly.
		return RecallParams{Alpha: 0.15 /* TUNABLE */, Tau: 12 /* TUNABLE (hours) */}
	case TypePerson, TypeProject:
		// Identity/relationship nodes: moderate spacing, moderate consolidation.
		return RecallParams{Alpha: 0.08 /* TUNABLE */, Tau: 48 /* TUNABLE (hours) */}
	default:
		// Fact, Decision, Insight: stable semantic content, one-day spacing prior.
		return RecallParams{Alpha: 0.10 /* TUNABLE */, Tau: 24 /* TUNABLE (hours) */}
	}
}

// StrengthAt computes the instantaneous retrieval strength of an entry at time
// now using the leaky-integrator decay formula (tech spec §5.1):
//
//	S(t) = S_floor + (S_last − S_floor) · e^(−λ · Δt)
//
// where Δt is the elapsed time in hours since LastRefTime.
//
// Properties:
//   - At Δt = 0: S(t) = S_last (full vividness just after a recall).
//   - As Δt → ∞: S(t) → S_floor (permanent base strength never lost).
//   - Result is clamped to [0, S_cap] to guard against float drift.
//
// StrengthAt does NOT check IsCurrent; staleness filtering (excluding entries
// whose ValidUntil has passed) is the caller's responsibility. The two signals
// are orthogonal: a superseded entry can still carry non-zero strength and a
// valid entry can have decayed to near S_floor.
func StrengthAt(e Entry, now time.Time) float64 {
	Δt := now.Sub(e.Temporal.LastRefTime).Hours()
	if Δt < 0 {
		Δt = 0 // guard: clock skew or backdated test fixtures
	}
	s := e.Temporal.SFloor + (e.Temporal.SLast-e.Temporal.SFloor)*math.Exp(-e.Temporal.Lambda*Δt)
	// Clamp to [0, S_cap] to guard against floating-point drift.
	if s < 0 {
		s = 0
	}
	if s > e.Temporal.SCap {
		s = e.Temporal.SCap
	}
	return s
}

// BumpOnRecall updates the temporal fields of an entry following a material
// recall (RECALL-DEF: surfaced AND materially used in a reply — not merely
// retrieved by search). It implements the spacing-aware consolidation bump
// (tech spec §5.1):
//
//	α_eff  = params.Alpha · (1 − e^(−Δt / params.Tau))
//	S_floor ← S_floor + α_eff · (S_cap − S_floor)   // consolidation creep
//	S_last  ← S_cap                                    // vividness spike
//	LastRefTime ← now
//
// The spacing multiplier (1 − e^(−Δt/τ)) encodes the spacing effect and
// Bjork's desirable-difficulty principle in one term: rapid successive recalls
// contribute almost nothing to long-term consolidation, while spaced recalls
// consolidate durably.
//
// BumpOnRecall returns a modified copy; it never mutates e (non-destructive,
// consistent with Supersede). The caller is responsible for persisting the
// returned entry via Store.Append (INV-2: append-only; the original is not
// overwritten, a new temporal-update record is appended).
//
// Note: S_last is reset to S_cap, not to a floor-modulated spike. The
// floor-modulated spike (Bjork: high-SS → bigger RS spike) would double-count
// with the spacing multiplier. Deferred to Phase-3 empirical tuning if
// high-floor memories show insufficient brightening.
func BumpOnRecall(e Entry, now time.Time, params RecallParams) Entry {
	Δt := now.Sub(e.Temporal.LastRefTime).Hours()
	if Δt < 0 {
		Δt = 0 // guard: clock skew
	}
	αEff := params.Alpha * (1 - math.Exp(-Δt/params.Tau))

	// Consolidation creep: floor rises toward S_cap, modulated by spacing.
	e.Temporal.SFloor = e.Temporal.SFloor + αEff*(e.Temporal.SCap-e.Temporal.SFloor)
	// Vividness spike: S_last resets to S_cap.
	e.Temporal.SLast = e.Temporal.SCap
	// Timestamp the recall.
	e.Temporal.LastRefTime = now
	return e
}

// MarkRecalled is the Phase-3 (WP-5) RECALL-DEF event primitive: it records
// that entry id was surfaced AND MATERIALLY USED in a reply.
//
// The definitional gate is the CALLER's responsibility and it is strict: a
// search hit, a retrieval, or an entry merely included in context is a
// non-event (RECALL-DEF, ratified 2026-06-17). Call this only when material
// use is established — over-calling it re-creates the rich-get-richer feedback
// the Phase-3 brakes exist to contain. Detecting material use is host-side
// work (tech spec S-3); the substrate deliberately ships only the primitive.
//
// Mechanics (all through the Store port — works identically over Markdown,
// memory, graph, or the log-first composition):
//
//  1. Load the corpus and resolve the LATEST record for id (an id may have
//     multiple records: supersession closes and prior recalls update).
//  2. Apply BumpOnRecall with the entry type's default params.
//  3. APPEND the updated record. History is never rewritten (INV-2): the
//     prior temporal state remains in the corpus; readers resolve the latest
//     record per id. This is also the ONLY write a read may ever trigger
//     (dev spec RH-5).
//
// The bumped entry is returned. An unknown id is a loud error.
func MarkRecalled(ctx context.Context, s Store, id ID, now time.Time) (Entry, error) {
	entries, _, err := s.Load(ctx)
	if err != nil {
		return Entry{}, fmt.Errorf("core: mark recalled %q: %w", id, err)
	}
	found := false
	var latest Entry
	for _, e := range entries {
		if e.ID == id {
			latest, found = e, true // later records supersede earlier temporal state
		}
	}
	if !found {
		return Entry{}, fmt.Errorf("core: mark recalled %q: no record with that id in the corpus", id)
	}
	bumped := BumpOnRecall(latest, now, DefaultRecallParams(latest.Type))
	if err := s.Append(ctx, []Entry{bumped}, nil); err != nil {
		return Entry{}, fmt.Errorf("core: mark recalled %q: append temporal update: %w", id, err)
	}
	return bumped, nil
}
