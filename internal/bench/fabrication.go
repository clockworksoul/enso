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

// StagedFabricationCases returns FABRICATION cases that have been fully
// modeled and validated but are not yet wired into FabricationCases().
// These are candidates for the Jul 13 go-live review; promoting one here means
// adding it to the FabricationCases() return slice above — a one-line change.
//
// Current staged cases: neoBlogOutlineFabrication (2026-07-03).
func StagedFabricationCases() []FabricationCase {
	return []FabricationCase{neoBlogOutlineFabrication()}
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

// neoBlogOutlineFabrication — the Neo4j blog-outline FABRICATION miss (2026-07-03).
// STAGED: not yet in FabricationCases(). Candidate for Jul 13 go-live review.
//
// Context: Matt's laptop went offline ~2h while relocating to a vacation cabin
// (Sylvan Beach, NY). Session context fragmented across 6 resets. When context
// was re-established from a stale Jun 22 message, I elaborated it into a full
// confident scenario: the Neo4j blog still had 6 open decisions awaiting Matt's
// call. I produced detailed recommendations for all 6 (§2 schema, §4 numbers,
// §6 division of labor, title, length, code artifacts). In reality, those
// decisions had been finalized, the outline shared with Ed on Jun 23, Ed replied
// Jun 30, and the first Medium draft shipped Jul 2 — the project was 11 days
// past that stage.
//
// Matt's correction: "I already made those changes and shared with Ed yesterday."
//
// WHY THIS IS A DIFFERENT FABRICATION SUBTYPE than Tipa-tenure:
//
//								Tipa-tenure (Jun 23)				Neo4j outline (Jul 3)
//								—————————————————				————————————————————
//	Unit fabricated			precise figure ("13 months")		entire scenario / state-of-world
//	Seed						vague anchor ("6-7mo in April")  	specific-but-stale entry (Jun 22)
//	PreciseSupportExists	false (only a soft estimate)		true (6 named decisions, specific)
//	Anchor confidence		ConfMedium (soft estimate)			ConfHigh (specific as-of-Jun-22)
//	shouldAbstain trigger	branch 1 (no precise support)		branch 3 (temporal decay < floor)
//								AND branch 2 (medium confidence)
//
// This is the key finding: the first two shouldAbstain branches do NOT fire
// here. The anchor IS specific and IS high-confidence — it was correct and
// precise as of Jun 22. The ONLY discriminating signal is that 11 days have
// elapsed and the entry’s temporal strength has decayed to ~SFloor (0.1),
// which is well below the staleFloor of 0.35. Asserting a confident current-
// state scenario from a stale, decayed entry is the fabrication trap; the
// abstention layer catches it via temporal decay alone.
//
// Decay math (TypeDecision, Lambda=0.05, SFloor=0.1, SLast=SCap=1.0):
//
//	Δt = Jun 22 → Jul 3 = 264 hours
//	StrengthAt = 0.1 + (1.0−0.1)*e^(−0.05×264) ≈ 0.1 + 0.9*e^(−13.2) ≈ 0.100
//	0.100 < staleFloor(0.35) → branch 3 fires.
//
// This case is the first real validation of branch 3. Tipa validated branches
// 1+2; this one validates 3. Together they exercise the full abstention surface.
func neoBlogOutlineFabrication() FabricationCase {
	jun22 := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC) // when the Jun 22 outline-decisions message was encoded
	jul3 := time.Date(2026, 7, 3, 1, 30, 0, 0, time.UTC)   // when the fabrication occurred (~21:30 ET = 01:30 UTC Jul 4)

	// The real anchor: the Jun 22 chat message that documented 6 open outline
	// decisions. It was SPECIFIC and HIGH-CONFIDENCE as of Jun 22 — those
	// decisions genuinely were open then. The failure is that this entry was used
	// 11 days later as if it were current, seeding an elaborate fabricated
	// scenario. ValidUntil is nil because no explicit supersession entry was ever
	// written; the decisions were finalized in conversation, not recaptured as a
	// memory entry. The only signal of staleness is the temporal decay.
	anchor := mustEntry(
		"neo4j-blog-outline-decisions-jun22",
		core.TypeDecision,
		"Neo4j blog post has 6 open outline decisions awaiting Matt’s call: \u00a72 schema (generic vs Omega vs both), \u00a74 numbers (Legal threshold), \u00a76 division of labor (Matt/Ed split), title (leading candidate: ‘Your Engineering Org Is Already a Graph’), length (3k–3.4k vs tighter), code artifacts (3 Cypher snippets).",
		jun22,
		nil, // no explicit supersession written; staleness only visible via decay
		[]string{"work", "writing", "neo4j"},
		[]string{"project:neo4j-blog", "person:ed-ceballos"},
	)
	// High confidence as-of-Jun-22: the decisions really were open then.
	// The fabrication trap is treating this high-confidence-as-of-then entry as
	// high-confidence-right-now after 11 days of decay.
	// NOTE: do NOT lower confidence here — that would misrepresent the case.
	// anchor.Confidence stays ConfHigh, which is what makes this case distinct:
	// the first two shouldAbstain branches DON’T fire, only the staleness branch.

	return FabricationCase{
		Name:      "neo4j-blog-outline-fabrication",
		MissClass: "FABRICATION",
		Query:     "what’s the current state of the Neo4j blog outline decisions?",
		AsOf:      jul3,
		Anchor:    anchor,
		FabricatedAnswer: "[full scenario] The outline still has 6 open decisions. " +
			"\u00a72 schema: generic first, scale-reveal in \u00a74. " +
			"\u00a74 numbers: put real figures in, let Legal cut. " +
			"\u00a76 division of labor: Matt scaffolds, Ed fills roadmap. " +
			"Title: ‘Your Engineering Org Is Already a Graph’ / ‘It’s 3am’ subtitle. " +
			"Length: ~3,200 words, ask Ed to confirm. Code: exactly 3 Cypher snippets.",
		TrueAnswer: "All 6 decisions were finalized on/before Jun 23. " +
			"Outline shared with Ed Jun 23; Ed replied Jun 30 (all green). " +
			"First Medium draft shipped to Ed Jul 2. Project is 11 days past the outline-decisions stage.",
		PreciseSupportExists: true, // the anchor IS specific — staleness is the only signal
	}
}
