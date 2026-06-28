package bench

import (
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// FabricationCases returns the FABRICATION-class benchmark cases. These come
// from the third real miss species in the live log
// (research/2026-06-17-phase0-benchmark.md): a confidently-asserted precise
// figure the record did not support.
//
// FABRICATION is structurally DIFFERENT from both STALE and NEIGHBOR, and that
// difference is the whole point of this file:
//
//   - STALE: a wrong entry exists and is ranked above the correct one. Fixable
//     by supersession. precision@1 captures it.
//   - NEIGHBOR: a vaguer parent entry is ranked above the specific child. Fixable
//     by specificity-aware ranking. precision@1 captures it.
//   - FABRICATION: the wrong answer was NEVER AN ENTRY. It was invented at
//     reply time by over-extrapolating from a real but vague anchor. There is
//     no distractor entry to rank below the truth, so a ranking model that puts
//     the (only) correct entry first scores a perfect 1/1 — AND THE MISS STILL
//     HAPPENED IN REAL LIFE.
//
// That last point is the finding. precision@1 — the metric this whole benchmark
// is built on — is BLIND to FABRICATION by construction. You cannot rank your
// way out of inventing a fact. The defense is a different axis entirely: a
// retrieval-margin / abstention signal that says "the support for a precise
// answer here is weak; hedge or abstain instead of asserting a number."
//
// So these cases are scored on a SECOND axis (margin), not precision@1. See
// fabrication_test.go.
func FabricationCases() []FabricationCase {
	return []FabricationCase{tipaTenureFabrication()}
}

// FabricationCase is a labeled FABRICATION miss. Unlike Case (which is scored by
// precision@1 ranking), a FabricationCase is scored by whether the retrieval
// MARGIN is low enough that a disciplined system would abstain from asserting a
// precise answer rather than fabricate one.
type FabricationCase struct {
	Name      string
	MissClass string // always "FABRICATION"

	// Query is the question that, in real life, produced a fabricated precise
	// answer (here: "how long has Tipa been at Yext?").
	Query string

	// AsOf is when the question was asked.
	AsOf time.Time

	// Anchor is the real, vague entry the fabrication over-extrapolated FROM.
	// In the Tipa miss this was "6-7 months in April" — a soft, dated, range-y
	// fact. It is the ONLY entry in the corpus that matches the query; there is
	// no precise "13 months" entry, because that number was invented.
	Anchor core.Entry

	// FabricatedAnswer is what was actually said (for the record / readability).
	FabricatedAnswer string

	// TrueAnswer is the ground truth (~9.5 months as of late June, from a
	// Sept-2-2025 hire date), for the record.
	TrueAnswer string

	// PreciseSupportExists records whether ANY entry in the corpus directly
	// supports a precise numeric answer. For a FABRICATION case this is false by
	// definition: the only support is the vague Anchor. A disciplined system
	// should detect "no precise support" and abstain.
	PreciseSupportExists bool
}

// tipaTenureFabrication — the Tipa-tenure FABRICATION miss (2026-06-23).
//
// What was asked (while reasoning about manage-out runway): how long has Tipa
// been at Yext?
// What was said, repeatedly and with confidence: "13 months in."
// What was true: hired Sept 2, 2025 → ~9.5 months as of late June 2026. The
// record at the time said only "6-7 months in April," which extrapolates to
// ~9-10 months by June, NOT 13.
// Why FABRICATION: a precise figure was asserted that the record did not
// support, in a career-stakes context (runway). Not stale, not a near-neighbor
// retrieval — an invented precise number anchored to a vague real fact.
//
// The corpus models this honestly: there is exactly ONE matching entry, the
// vague April anchor. There is NO "13 months" entry to rank, because that
// number never existed as a memory. A precision@1 ranker therefore "passes"
// (it ranks the only entry first) while the real-world miss still occurred.
// The defense has to come from MARGIN, not ranking.
func tipaTenureFabrication() FabricationCase {
	apr := time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC)
	jun23 := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)

	// The real anchor: vague, dated, range-y. This is genuinely what the record
	// held. It supports a SOFT answer ("~6-7mo as of April, so ~9-10 by now"),
	// never a hard "13 months."
	anchor := mustEntry(
		"tipa-tenure-april-soft",
		core.TypeFact,
		"Tipa is roughly 6-7 months into his tenure as of April 2026 (soft estimate, not a hire date).",
		apr,
		nil,
		[]string{"work", "team", "career"},
		[]string{"person:tipa"},
	)
	// Soft estimate → modeled as lower confidence. The fabrication ignored this
	// softness and reported a hard number anyway.
	anchor.Confidence = core.ConfMedium

	return FabricationCase{
		Name:                 "tipa-tenure-fabrication",
		MissClass:            "FABRICATION",
		Query:                "how long has Tipa been at Yext?",
		AsOf:                 jun23,
		Anchor:               anchor,
		FabricatedAnswer:     "13 months",
		TrueAnswer:           "~9.5 months (hired Sept 2, 2025)",
		PreciseSupportExists: false, // only the vague April anchor exists
	}
}
