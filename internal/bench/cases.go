package bench

import (
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// SeedCases are the first labeled benchmark items, reconstructed from REAL
// recall misses that happened on 2026-06-23 (logged in
// research/2026-06-17-phase0-benchmark.md). They are deliberately STALE-class,
// because STALE is the early-dominant failure mode in the live miss log: the
// model knew a fact correct-as-of-an-old-state and never updated it.
//
// Each case encodes the moment of the miss: the query time (AsOf), the stale
// entry that the naive model wrongly surfaced, and the current entry that
// should have won. The supersession is expressed via a SUPERSEDES edge plus a
// ValidUntil on the stale entry, exactly as core.Entry.Supersede produces.
//
// IMPORTANT: these reconstruct the SHAPE of the miss faithfully (which entry
// was correct as of when), not the verbatim wording. The benchmark measures
// ranking, not content fidelity.
func SeedCases() []Case {
	return []Case{
		adamHeadcountStale(),
		edSandovalTimelineStale(),
	}
}

// NeighborCases returns the NEIGHBOR-class benchmark cases: cases where the
// failure is centroid-adjacent retrieval (right neighborhood, wrong specific)
// rather than stale supersession. The current Ensō model does NOT solve these:
// decay-ranked retrieval without query-content matching or specificity-preference
// will pick the vague parent over the specific child whenever the parent looks
// fresher. Both models score 0/N on this set. That is not a test failure; it is
// an honest documentation of a known limitation and the target for Stage 5.
//
// Stage 5 target: specificity-aware retrieval that, given a specific query,
// prefers the more specific matching entry over its vaguer parent even when the
// parent is fresher. Options include semantic/content matching or graph traversal
// from parent to child via OWNS/RELATES_TO edges.
func NeighborCases() []Case {
	return []Case{
		ensōRepoPathNeighbor(),
	}
}

// mustEntry builds a validated entry, setting EncodedTime and the temporal
// LastRefTime to encodedAt. Panics on invalid input (test-support only).
func mustEntry(idLabel string, nt core.NodeType, content string, encodedAt time.Time, validUntil *time.Time, tags, about []string) core.Entry {
	id, err := core.NewID(encodedAt, idLabel)
	if err != nil {
		panic(err)
	}
	e, err := core.NewEntry(core.NewEntryParams{
		ID:          id,
		Type:        nt,
		Content:     content,
		EncodedTime: encodedAt,
		Confidence:  core.ConfHigh,
		ValidUntil:  validUntil,
		Tags:        tags,
		About:       about,
	})
	if err != nil {
		panic(err)
	}
	return e
}

// Case 1 — the Adam headcount STALE miss (2026-06-23).
//
// What was asked: "what's next on the TODO" → I surfaced "Message Adam re:
// headcount, overdue since Jun 16."
// What was true: the headcount ask already landed at the Jun 18 Adam 1:1.
// Why STALE: the Jun 16 TODO line was correct-as-of-old-state and never
// superseded by the Jun 18 outcome.
//
// The REAL failure dynamic (this is what makes recency insufficient): the stale
// TODO line is a standing item that gets re-read and re-surfaced on every TODO
// scan, so to a recency/salience heuristic it looks PERPETUALLY FRESH — its
// effective last-touch is Jun 23 (today, when it was scanned again), NEWER than
// the buried Jun-18 1:1 outcome. A naive "surface the most-recently-touched
// matching entry" model therefore picks the STALE item. Only supersession
// (closing the Jun-16 task when the Jun-18 outcome landed) rescues it. We model
// the stale item's re-surfacing by giving it an EncodedTime of the last scan.
//
// LOOP-CLOSING NOTE: this case is built by exercising the REAL capture path
// (core.Entry.Correct), not by hand-assembling the SUPERSEDES edge. That makes
// the benchmark an end-to-end proof: the same function that captures a live
// correction produces the triple the Ensō model then scores. If capture and
// consumption ever drift, this case breaks. Hand-wiring would have let them
// drift silently.
func adamHeadcountStale() Case {
	jun16 := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	jun18 := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)
	jun23 := time.Date(2026, 6, 23, 21, 0, 0, 0, time.UTC)

	// The original TODO entry, encoded Jun 16 (before the outcome existed).
	original := mustEntry(
		"adam-headcount-todo",
		core.TypeTask,
		"Message Adam re: Axon headcount/investment. Target Jun 16; prep slipped.",
		jun16,
		nil, // not yet closed at encode time
		[]string{"work", "career", "team"},
		[]string{"person:adam", "project:axon"},
	)

	// Capture the correction THROUGH THE REAL PATH when the Jun-18 outcome landed.
	stale, current, edge, err := original.Correct(core.Correction{
		Kind:       core.CorrectRestate,
		Content:    "Adam headcount ask landed at the Jun 18 1:1. Adam aligned; prefers moving someone internally; no specific person yet.",
		NewLabel:   "adam headcount landed",
		AsOf:       jun18,
		Type:       core.TypeDecision,
		Confidence: core.ConfHigh,
	})
	if err != nil {
		panic(err)
	}

	// Reproduce the live failure dynamic: the closed TODO line keeps getting
	// re-scanned, so by touch-recency it looks fresher than the Jun-18 outcome
	// right up to the query. Correct leaves content/EncodedTime untouched (INV-2);
	// we only bump the recency signal the baseline keys on.
	jun23scan := time.Date(2026, 6, 23, 20, 0, 0, 0, time.UTC)
	stale.EncodedTime = jun23scan
	stale.Temporal.LastRefTime = jun23scan

	return Case{
		Name:      "adam-headcount-stale",
		MissClass: "STALE",
		Query:     "what's the status of the Adam headcount item?",
		// Faithful reconstruction of the Jun-18 correction utterance: the moment
		// the outcome landed, the natural way it was stated. "actually" + the
		// restated fact is the real shape of how this correction entered the
		// conversation; it is NOT tuned to guarantee a detector hit.
		Utterance:  "actually the Adam headcount ask already landed at the Jun 18 1:1",
		AsOf:       jun23,
		WantID:     current.ID,
		Candidates: []core.Entry{stale, current},
		Edges:      []core.Edge{edge},
	}
}

// Case 2 — the Ed Sandoval Neo4j-blog timeline miss (2026-06-23).
//
// What was asked (implicitly, while drafting the Ed email): whose court is the
// blog post in? The drafted email opened "apologies for the gap on my end,"
// implying Matt owed the next move. What was true: the May 26 thread leg
// (Ed owes guest-post terms) meant the open dependency was on ED's side.
//
// This is a CorrectReframe: the underlying facts did not change (Matt asked
// May 26; Ed went silent). The *interpretation* of whose-court was wrong.
// That is the defining characteristic of the reframe class: same facts,
// corrected frame. The hazard is exactly what the CorrectionKind docs warn:
// nothing looked obviously outdated, so recency is maximally misleading.
//
// The stale belief was re-affirmed up to draft-time (looks freshest); the
// corrected fact (true since May 26) is OLDER by write-time. Only
// supersession surfaces it.
func edSandovalTimelineStale() Case {
	jun23 := time.Date(2026, 6, 23, 17, 0, 0, 0, time.UTC)
	jun23affirm := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	// The stale reframe: "Matt needs to move this forward."
	// Kept being re-affirmed at every thread reference, so by touch-recency
	// it looks perpetually current right up to the query.
	original := mustEntry(
		"ed-thread-matt-owes",
		core.TypeFact,
		"Neo4j blog: internal Yext legal cleared. Next action felt to be on Matt to push the thread forward.",
		jun23affirm,
		nil, // not yet closed before the correction is applied
		[]string{"work", "omega", "career"},
		[]string{"person:ed-sandoval", "project:neo4j-blog"},
	)

	// The corrected fact has been TRUE since May 26 (Ed owes the guest-post
	// terms, and went silent). EventTime = May 26 (when it became true in the
	// world); AsOf = Jun 23 (when the reframe was captured). The EventTime
	// distinction is the reason CorrectReframe exists: the corrected fact is
	// older by world-time than the stale belief, even though it is captured now.
	may26 := time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC)
	stale, current, edge, err := original.Correct(core.Correction{
		Kind:      core.CorrectReframe,
		Content:   "Neo4j blog: open dependency is on ED's side. Matt asked May 26 for Neo4j guest-post submission terms; Ed punted to DevRel and went silent ~4 weeks. Ball in Ed's court, not Matt's.",
		NewLabel:  "ed thread ed owes terms",
		AsOf:      jun23,
		EventTime: &may26, // the corrected fact became true May 26, not Jun 23
	})
	if err != nil {
		panic(err)
	}

	return Case{
		Name:      "ed-sandoval-timeline-reframe",
		MissClass: "STALE",
		Query:     "whose court is the Neo4j blog post in?",
		// Faithful reconstruction of the reframe utterance. NOTE (2026-06-26): an
		// earlier version of this fixture read "the ball is ACTUALLY in Ed's
		// court" — the spurious "actually" tripped the bare-actually restate signal
		// and mis-tagged the reframe as a restate. That was a FIXTURE bug, not a
		// detector bug: the real correction did not lead with "actually," and the
		// whose-court signal classifies the faithful sentence correctly as reframe.
		// Keep this fixture honest — do not season it with markers the real
		// utterance lacked just to make a signal fire.
		Utterance:  "the ball is in Ed's court, not Matt's",
		AsOf:       jun23,
		WantID:     current.ID,
		Candidates: []core.Entry{stale, current},
		Edges:      []core.Edge{edge},
	}
}

// ensōRepoPathNeighbor — the enso-repo-path NEIGHBOR miss (2026-06-23).
//
// What happened: asked where the enso repo lives locally. I held a parent fact
// ("Matt's clockworksoul OSS repos live under ~/workspace/clockworksoul/") but
// failed the one-hop to the specific child
// ("the enso repo is at ~/workspace/clockworksoul/enso"), confabulating "not
// found" and running two SIGKILL'd find-hunts. The active-memory plugin even
// surfaced the parent fact in the same turn.
//
// This is the DRM/centroid-adjacent signature: the parent entry matches the
// query neighborhood ("where do clockworksoul repos live?") and it is FRESHER
// by touch (it is general background knowledge that keeps being referenced),
// so both recency-baseline and the current Ensō model (decay-ranked) prefer
// it over the specific child. Neither model solves this case.
//
// WHY NEITHER MODEL SOLVES IT: the Ensō model's SUPERSEDES+IsCurrent+decay
// pipeline addresses STALE pairs (wrong entry is closed). Here BOTH entries
// are current; no SUPERSEDES edge exists. The failure is specificity-blindness:
// "repos under ~/workspace/clockworksoul/" is a vaguer match than "the enso
// repo at ~/workspace/clockworksoul/enso," but the current ranker has no
// query-content matching or specificity-preference layer to prefer the specific.
//
// STAGE 5 TARGET: specificity-aware ranking that, for a specific query,
// prefers the more specific matching entry over its vaguer parent even when
// the parent is fresher. Approaches: semantic content-match scoring, graph
// traversal from parent to child via OWNS edges, or a specificity-boost
// proportional to About-ref match depth.
func ensōRepoPathNeighbor() Case {
	// The parent fact: general background knowledge, kept being referenced,
	// so by touch-recency it looks perpetually fresh.
	jun10 := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	jun23query := time.Date(2026, 6, 23, 13, 50, 0, 0, time.UTC) // the miss instant

	parent := mustEntry(
		"clockworksoul-oss-root",
		core.TypeFact,
		"Matt's clockworksoul OSS repos live under ~/workspace/clockworksoul/ on the local machine.",
		jun10,
		nil,
		[]string{"workspace", "repos", "clockworksoul"},
		[]string{"project:clockworksoul"},
	)
	// The parent keeps being referenced (it is general infrastructure knowledge)
	// so it looks freshest at query time. Both models key on this and pick it.
	parent.Temporal.LastRefTime = jun23query.Add(-5 * time.Minute) // touched just before the query

	// The specific child fact: encoded earlier, less-frequently-touched, therefore
	// looks staler by recency even though it is the correct answer.
	may28 := time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC)
	child := mustEntry(
		"enso-repo-local-path",
		core.TypeFact,
		"The Ensō repo (github.com:clockworksoul/enso) is cloned locally at ~/workspace/clockworksoul/enso. Module: github.com/clockworksoul/enso, Go 1.26.",
		may28,
		nil,
		[]string{"workspace", "repos", "clockworksoul", "enso"},
		[]string{"project:enso", "project:clockworksoul"},
	)
	// Child is correctly encoded but less recently touched: it was written once
	// and not re-surfaced frequently, so decay leaves it lower than the parent.
	// No bump since creation.

	// The OWNS edge records the parent->child structural relationship.
	// The current Ensō model does not traverse this edge; it is included
	// here for Stage 5 to key on.
	ownsEdge := core.Edge{
		From:  parent.ID,
		Type:  core.EdgeOwns,
		To:    string(child.ID),
		Extra: map[string]string{},
	}

	return Case{
		Name:       "enso-repo-path-neighbor",
		MissClass:  "NEIGHBOR",
		Query:      "where does the enso repo live locally?",
		AsOf:       jun23query,
		WantID:     child.ID,
		Candidates: []core.Entry{parent, child},
		Edges:      []core.Edge{ownsEdge},
	}
}
