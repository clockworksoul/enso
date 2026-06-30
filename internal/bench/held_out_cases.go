package bench

import (
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// HeldOutStaleCases are the GENERALIZATION probe for the correction loop.
//
// WHY THIS SET EXISTS — separate from SeedCases.
//
// The two SeedCases (adam-headcount, ed-sandoval) are the misses the detector
// vocabulary in core/detect.go was WRITTEN AGAINST. Scoring the detector on the
// same cases it was tuned to catch proves nothing about generalization — it is
// train==test. The "genuinely-open next seams" note (DROSS-TODO, Jun 29) names
// this explicitly: "n=1 validates the axis, not the layer; need a 2nd/3rd real
// case before tuning." Seam #4: "replay the rest of the live [FLAGGED-MISS] log
// (informal Jun-25 STALEs — Granola ban, LeanCTX — still uncaptured)."
//
// These are exactly those two held-out, real, never-processed STALE misses from
// the Day-7 Phase-0 benchmark log (research/2026-06-17-phase0-benchmark.md,
// "2026-06-25 (Thu) — Day 7", "two proactive STALE self-corrections today").
// They were logged informally and never run through the detector or the recall
// loop. Replaying them measures the one thing the seed cases cannot: does the
// loop fire on real correction language it was NOT built around?
//
// DISCIPLINE — faithful, not tuned. The Utterance fields below are
// reconstructed from how each correction actually entered the conversation, NOT
// reverse-engineered to trip a signal. If a real correction does not contain a
// lexical marker the detector knows, the honest outcome is fired=false — and
// that is a finding about detector RECALL, the precondition for any later
// vocabulary work. Do not season these with "actually" / "that's stale" markers
// the real utterance lacked just to make the number look good. (The same
// fixture-honesty lesson the ed-sandoval reframe case learned the hard way.)
//
// SHAPE. Like the seed cases, each is built PRE-correction (the stale belief is
// an open entry) and the correction is captured THROUGH core.Entry.Correct, so
// the resulting triple is what the recall model then scores. Capture and
// consumption cannot drift without breaking the build.
func HeldOutStaleCases() []Case {
	return []Case{
		granolaBanStale(),
		leanCtxScopeStale(),
	}
}

// Case H1 — the Granola-ban STALE miss (2026-06-25).
//
// What happened: on 2026-06-22 Yext announced a Granola ban policy, and the
// stale belief "Granola is banned; switch to the Zoom->yext/transcripts
// replacement workflow" was written into MEMORY.md / TOOLS.md. On 2026-06-25
// Matt clarified that Granola STILL WORKS and remains the transcript source of
// record despite the announced policy. The belief was correct-as-of-the-Jun-22
// announcement and never reconciled against the Jun-25 reality.
//
// Why STALE (not NEIGHBOR/FABRICATION): the stored fact was true once (the ban
// WAS announced) and simply never updated when the operative reality diverged.
// Classic reconsolidation gap.
//
// DETECTOR PREDICTION (recorded before running): the natural correction
// utterance is a bare corrective assertion ("Granola still works and is the
// source of record") with NO explicit supersession marker — no "actually it's
// now", no "that's stale", no "scratch that". The Jun-23-grounded vocabulary
// may well MISS it. That prediction is the point of the probe.
func granolaBanStale() Case {
	jun22 := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)  // ban announced
	jun25 := time.Date(2026, 6, 25, 14, 37, 0, 0, time.UTC) // Matt's clarification (Day-7 log timestamp)

	// The stale belief, encoded when the ban was announced. Kept being
	// re-referenced (it is a standing workflow note), so by touch-recency it
	// looks perpetually fresh right up to the query.
	original := mustEntry(
		"granola-banned-switch-zoom",
		core.TypeFact,
		"Granola is banned per Yext policy (Jun 22). Default to the Zoom -> yext/transcripts replacement workflow.",
		jun22,
		nil, // open at encode time
		[]string{"work", "tools", "granola", "transcripts"},
		[]string{"tool:granola", "policy:yext"},
	)

	stale, current, edge, err := original.Correct(core.Correction{
		Kind:       core.CorrectRestate,
		Content:    "Granola still works and is the transcript source of record despite the Jun-22 ban policy. Keep using scripts/granola.py; do NOT default to the Zoom replacement workflow.",
		NewLabel:   "granola still source of record",
		AsOf:       jun25,
		Type:       core.TypeFact,
		Confidence: core.ConfHigh,
	})
	if err != nil {
		panic(err)
	}

	// Reproduce the live failure dynamic: the stale workflow note keeps being
	// re-scanned (standing TOOLS.md/MEMORY.md content) right UP TO AND PAST the
	// query, so by touch-recency it looks fresher than the buried correction.
	// This is the whole reason supersession is needed: the correction lands once
	// (EncodedTime = AsOf) but the stale line is re-surfaced on every subsequent
	// scan, so a recency heuristic sees the stale entry as freshest. We model the
	// last re-scan as just after the query instant. Correct leaves the stale
	// entry's content untouched (INV-2); we only bump the recency signal the
	// baseline keys on, matching how the note was actually re-surfaced.
	jun25scan := jun25.Add(1 * time.Minute)
	stale.EncodedTime = jun25scan
	stale.Temporal.LastRefTime = jun25scan

	return Case{
		Name:      "granola-ban-stale",
		MissClass: "STALE",
		Query:     "is Granola banned — what's the transcript workflow?",
		// Faithful reconstruction. This is how the correction actually entered the
		// conversation: a direct corrective assertion, NO explicit marker. Not
		// tuned to fire.
		Utterance:  "Granola still works and is the transcript source of record",
		AsOf:       jun25,
		WantID:     current.ID,
		Candidates: []core.Entry{stale, current},
		Edges:      []core.Edge{edge},
	}
}

// Case H2 — the LeanCTX scope STALE miss (2026-06-25).
//
// What happened: an existing memory note on the LeanCTX tool described a narrow
// scope. On 2026-06-25 (19:09 UTC, Day-7 log) the assistant flagged that the
// note UNDERSOLD the tool's current scope and updated it. The note was
// correct-as-of-when-written and never reconciled as the tool's scope grew.
//
// Why STALE: project/tool evolution outran the stored description. Same
// reconsolidation-gap mechanism, different surface than a TODO-status flip.
//
// DETECTOR PREDICTION (recorded before running): the natural correction here is
// a scope-expansion assertion ("LeanCTX does more than that now — it also
// ..."). It contains "now", which the restate:actually-now signal keys on — but
// ONLY in combination with "actually"/"it's now". A bare "does more than that
// now" may NOT match the compiled markers. Predicted: MISS or, at best, a weak
// hit. Again, the prediction is the point.
func leanCtxScopeStale() Case {
	apr := time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC)    // note written, narrow scope
	jun25 := time.Date(2026, 6, 25, 19, 9, 0, 0, time.UTC) // scope correction (Day-7 log timestamp)

	original := mustEntry(
		"leanctx-narrow-scope",
		core.TypeFact,
		"LeanCTX is a narrow context-trimming helper; limited scope.",
		apr,
		nil,
		[]string{"work", "tools", "leanctx", "context"},
		[]string{"tool:leanctx"},
	)

	stale, current, edge, err := original.Correct(core.Correction{
		Kind:       core.CorrectRestate,
		Content:    "LeanCTX's current scope is broader than the old note: it does more than narrow context-trimming. Update the stored description to the fuller current capability set.",
		NewLabel:   "leanctx broader scope",
		AsOf:       jun25,
		Type:       core.TypeFact,
		Confidence: core.ConfHigh,
	})
	if err != nil {
		panic(err)
	}

	// The narrow-scope note kept being referenced as the canonical LeanCTX
	// description right up to and past the query, so by touch-recency it looks
	// freshest at query time even though the correction landed once (at AsOf).
	// This re-scan dynamic is exactly what makes recency insufficient and
	// supersession load-bearing.
	jun25scan := jun25.Add(1 * time.Minute)
	stale.EncodedTime = jun25scan
	stale.Temporal.LastRefTime = jun25scan

	return Case{
		Name:      "leanctx-scope-stale",
		MissClass: "STALE",
		Query:     "what is LeanCTX's scope?",
		// Faithful reconstruction: a scope-expansion correction. Contains "now"
		// but NOT the "actually it's now" bigram the strong restate signal keys
		// on. Not tuned to fire.
		Utterance:  "LeanCTX does more than that now, the note undersells its current scope",
		AsOf:       jun25,
		WantID:     current.ID,
		Candidates: []core.Entry{stale, current},
		Edges:      []core.Edge{edge},
	}
}
