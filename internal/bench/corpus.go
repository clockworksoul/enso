// Package bench is the offline replay benchmark for Ensō's recall model. It is
// a test-support package (no production code depends on it) that answers one
// question with a number: does the decay/staleness/supersession model recover
// the *correct, current* answer on queries where the naive flat model gets it
// wrong?
//
// Why this exists
//
// The live active-memory plugin is a black box (persistTranscripts:false, logs
// frozen) — we cannot measure per-turn recall quality from deployment alone.
// But the recall MATH (core.StrengthAt, core.Rank, core.Entry.IsCurrent,
// core.Entry.Supersede) is pure and deterministic. So we measure the model
// offline: build a labeled corpus of real recall misses, replay each query
// "as of" its date through both a naive baseline and the Ensō model, and score
// which one ranks the ground-truth entry on top.
//
// The seed cases are REAL misses that actually happened (see cases.go). Every
// future logged miss (research/2026-06-17-phase0-benchmark.md) becomes another
// case. The benchmark is the success metric for Stage 4+ work: a change "helps"
// iff it raises the Ensō score on this corpus without regressing baseline-safe
// cases.
package bench

import (
	"sort"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// Case is one labeled benchmark item: a query asked at a specific time, a set
// of candidate memory entries that exist in the corpus as of that time, and the
// ID of the single entry that is the correct, current answer.
//
// The classic shape (and the one the seed cases use) is a STALE pair: an older
// entry that was true once, and a newer entry that supersedes it. A naive model
// that ignores supersession/recency can rank the stale one first; the Ensō
// model should rank the current one first.
type Case struct {
	Name string // short identifier, e.g. "adam-headcount-stale"

	// MissClass mirrors the Phase-0 benchmark taxonomy (STALE / NEIGHBOR /
	// NOISE) so the corpus stays aligned with the live miss log.
	MissClass string

	Query  string    // what was asked, for human readability
	AsOf   time.Time // the query time; decay + staleness evaluated at this instant
	WantID core.ID   // the ground-truth correct entry's ID

	// Utterance is the verbatim (faithfully reconstructed) correction sentence
	// that, in the real conversation, would have triggered capture of the
	// SUPERSEDES edge this case relies on. It is OPTIONAL and exists ONLY for the
	// detector replay (TestDetector_*): it measures whether core.DetectCorrection
	// would actually fire on the real language and resolve the stale target.
	//
	// Empty Utterance is meaningful, not missing: NEIGHBOR-class misses had NO
	// correction utterance at all (the failure was confabulation, not a stale
	// belief someone corrected), so there is nothing for a detector to detect.
	// The detector replay skips empty-Utterance cases and reports them as
	// "no utterance" rather than as detector misses — that distinction is itself
	// a finding about which misses a correction-detector can and cannot address.
	Utterance string

	// Candidates are all entries that plausibly match the query and exist as of
	// AsOf. Includes the correct entry AND the distractor(s) (e.g. the stale
	// version). The benchmark asks: does the model rank WantID first?
	Candidates []core.Entry

	// Edges carries supersession (and any other) relationships among the
	// candidates. The Ensō model consults SUPERSEDES edges to know which entry
	// is the current head of a supersession chain.
	Edges []core.Edge
}

// Model is anything that, given a case's candidates+edges and a query time,
// returns the candidates ranked best-first. Both the naive baseline and the
// Ensō model implement this, so the harness scores them identically.
type Model interface {
	Name() string
	Rank(candidates []core.Entry, edges []core.Edge, now time.Time) []core.Entry
}

// --- Baseline model: naive recency-by-EncodedTime, supersession-blind ---------

// BaselineModel approximates the *current* flat-file behavior: it has no concept
// of supersession or decay. It ranks purely by EncodedTime descending (most
// recently written first), which is the charitable version of "grep the latest
// note." It will get STALE cases wrong whenever the stale entry was written
// more recently than its correction, OR whenever a correction lives somewhere
// the recency heuristic doesn't privilege. Crucially, it never consults
// ValidUntil or SUPERSEDES edges.
type BaselineModel struct{}

func (BaselineModel) Name() string { return "baseline-recency" }

func (BaselineModel) Rank(candidates []core.Entry, _ []core.Edge, _ time.Time) []core.Entry {
	out := make([]core.Entry, len(candidates))
	copy(out, candidates)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].EncodedTime.After(out[j].EncodedTime)
	})
	return out
}

// --- Ensō model: staleness-aware, supersession-aware, decay-ranked ------------

// EnsoModel implements the intended retrieval pipeline: resolve supersession
// (drop entries that a SUPERSEDES edge has closed / that are not current as of
// now), then rank what remains by core.Rank (decay strength). This is the
// "filter-then-rank" pattern the core package documents.
//
// It deliberately reuses the real core functions (Rank/StrengthAt/IsCurrent) so
// the benchmark measures the actual shipped math, not a reimplementation.
type EnsoModel struct{}

func (EnsoModel) Name() string { return "enso-staleness+decay" }

func (EnsoModel) Rank(candidates []core.Entry, edges []core.Edge, now time.Time) []core.Entry {
	// 1. Identify entries that have been superseded by a SUPERSEDES edge whose
	//    target is this entry. Those are stale heads-of-chain and must lose.
	superseded := map[core.ID]bool{}
	for _, e := range edges {
		if e.Type == core.EdgeSupersedes {
			superseded[core.ID(e.To)] = true
		}
	}

	// 2. Filter: keep entries that are current as of now AND not superseded.
	kept := make([]core.Entry, 0, len(candidates))
	for _, c := range candidates {
		if !c.IsCurrent(now) {
			continue
		}
		if superseded[c.ID] {
			continue
		}
		kept = append(kept, c)
	}

	// 3. Rank survivors by decay strength (real core.Rank).
	ranked := core.Rank(kept, now)
	out := make([]core.Entry, len(ranked))
	for i, r := range ranked {
		out[i] = r.Entry
	}
	return out
}

// --- Scoring ------------------------------------------------------------------

// Result is the outcome of running one Model over the whole corpus.
type Result struct {
	Model     string
	Total     int
	TopHits   int        // cases where WantID ranked #1
	Failures  []string   // names of cases the model got wrong
}

// Score returns TopHits/Total as a fraction in [0,1].
func (r Result) Score() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.TopHits) / float64(r.Total)
}

// Run evaluates a Model over every Case and reports precision@1 (how often the
// correct entry ranks first). precision@1 is the right metric here because each
// case has exactly one correct answer and the live system surfaces a tiny
// top-k; ranking the truth first is the thing that actually prevents a miss.
func Run(m Model, cases []Case) Result {
	res := Result{Model: m.Name(), Total: len(cases)}
	for _, c := range cases {
		ranked := m.Rank(c.Candidates, c.Edges, c.AsOf)
		if len(ranked) > 0 && ranked[0].ID == c.WantID {
			res.TopHits++
		} else {
			res.Failures = append(res.Failures, c.Name)
		}
	}
	return res
}
