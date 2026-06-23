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

// TODO(NEIGHBOR): add the 2026-06-23 enso-repo-path miss as the first
// NEIGHBOR-class case. Different mechanism than the STALE seeds: the correct
// specific entry (child path `~/workspace/clockworksoul/enso`) exists and is
// reachable by a one-hop inference from a held parent fact
// (`~/workspace/clockworksoul/` = the OSS-repo root), but a vaguer parent entry
// substitutes for the specific child (centroid-adjacent retrieval). Faithfully
// modeling it needs the harness to exercise RELATES_TO / About-ref resolution
// (parent->child) rather than SUPERSEDES, plus a model that rewards landing the
// specific over the general. Deferred to a Dross Hour session so the
// relationship-resolution design is done properly, not rushed. Logged in
// research/2026-06-17-phase0-benchmark.md NEIGHBOR log.

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
func adamHeadcountStale() Case {
	jun18 := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)
	jun23 := time.Date(2026, 6, 23, 21, 0, 0, 0, time.UTC)
	// The stale TODO line was last re-surfaced/scanned today, just before the
	// query — so by write/touch-recency it looks fresher than the Jun-18 outcome.
	jun23scan := time.Date(2026, 6, 23, 20, 0, 0, 0, time.UTC)

	stale := mustEntry(
		"adam-headcount-todo",
		core.TypeTask,
		"Message Adam re: Axon headcount/investment. Target Jun 16; prep slipped.",
		jun23scan, // re-surfaced on today's TODO scan → looks freshest to recency
		&jun18,    // but it was actually closed when the Jun 18 outcome superseded it
		[]string{"work", "career", "team"},
		[]string{"person:adam", "project:axon"},
	)

	current := mustEntry(
		"adam-headcount-landed",
		core.TypeDecision,
		"Adam headcount ask landed at the Jun 18 1:1. Adam aligned; prefers moving someone internally; no specific person yet.",
		jun18,
		nil, // still current
		[]string{"work", "career", "team"},
		[]string{"person:adam", "project:axon"},
	)

	edge := core.Edge{
		From:  current.ID,
		Type:  core.EdgeSupersedes,
		To:    string(stale.ID),
		Extra: map[string]string{},
	}

	return Case{
		Name:       "adam-headcount-stale",
		MissClass:  "STALE",
		Query:      "what's the status of the Adam headcount item?",
		AsOf:       jun23,
		WantID:     current.ID,
		Candidates: []core.Entry{stale, current},
		Edges:      []core.Edge{edge},
	}
}

// Case 2 — the Ed Sandoval Neo4j-blog timeline STALE miss (2026-06-23).
//
// What was asked (implicitly, while drafting the Ed email): whose court is the
// blog post in / what's the latest state of the Ed thread?
// The drafted email opened with "apologies for the gap on my end," implying the
// ball was on Matt's side. What was true: the May 26 leg (Ed owes guest-post
// terms) meant the open dependency was on ED's side. The model's recall of the
// thread state was stale: it had Apr 28 + May 29 but the May 26 leg recolored
// who-owes-whom.
//
// Shape: an earlier "state of thread" entry (pre-May-26 understanding: Matt to
// follow up) superseded by the corrected timeline entry (Ed owes terms since
// May 26). Query is Jun 23.
func edSandovalTimelineStale() Case {
	jun23 := time.Date(2026, 6, 23, 17, 0, 0, 0, time.UTC)
	// The stale "Matt owes" understanding was the operative recalled state and
	// kept being re-affirmed whenever the thread came up, so by touch-recency it
	// looks current right up to the query. Model its last touch as just before
	// the query (the moment the email draft opened with "apologies for the gap
	// on my end").
	jun23affirm := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	// The stale understanding: the felt state was "Matt needs to move this
	// forward" (the May 26 leg, where Ed owes the guest-post terms, was not
	// captured in memory — so the recalled thread-state defaulted to Matt-owes).
	stale := mustEntry(
		"ed-thread-matt-owes",
		core.TypeFact,
		"Neo4j blog: internal Yext legal cleared. Next action felt to be on Matt to push the thread forward.",
		jun23affirm, // re-affirmed at draft time → looks freshest to recency
		&jun23,      // corrected later on Jun 23 when the May 26 leg surfaced
		[]string{"work", "omega", "career"},
		[]string{"person:ed-sandoval", "project:neo4j-blog"},
	)

	// The correct fact has been TRUE since May 26 (Ed owes the guest-post terms),
	// even though it surfaced into memory only on Jun 23. Encode it at its true
	// origin (May 26) so it is OLDER by write-time than the freshly re-affirmed
	// stale belief — this is what defeats a recency heuristic: the correct answer
	// is the older one, and only supersession/validity (not recency) surfaces it.
	may26 := time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC)
	current := mustEntry(
		"ed-thread-ed-owes-terms",
		core.TypeFact,
		"Neo4j blog: open dependency is on ED's side. Matt asked May 26 for Neo4j guest-post submission terms; Ed punted to DevRel and went silent ~4 weeks.",
		may26,
		nil, // current
		[]string{"work", "omega", "career"},
		[]string{"person:ed-sandoval", "project:neo4j-blog"},
	)

	edge := core.Edge{
		From:  current.ID,
		Type:  core.EdgeSupersedes,
		To:    string(stale.ID),
		Extra: map[string]string{},
	}

	return Case{
		Name:       "ed-sandoval-timeline-stale",
		MissClass:  "STALE",
		Query:      "whose court is the Neo4j blog post in?",
		AsOf:       jun23,
		WantID:     current.ID,
		Candidates: []core.Entry{stale, current},
		Edges:      []core.Edge{edge},
	}
}
