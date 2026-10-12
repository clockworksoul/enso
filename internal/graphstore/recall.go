package graphstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// Recall v1 (WP-3): traversal + staleness only — no vectors.
//
// Pipeline (dev spec §7.4):
//
//  1. SEED — match entry records lexically against the query using the
//     existing core.Tokenize / core.Specificity tokenizer (shared with the
//     Markdown pipeline, so the two cannot drift). No Cypher re-implementation
//     of tokenization: the graph provides reach, core provides judgment.
//  2. EXPAND — traverse 1–2 hops from the seed ids over RELATES_TO / ABOUT /
//     OWNS (undirected) in Cypher. This is the graph earning its keep: a
//     query whose terms match a NEIGHBOR of the answer still reaches the
//     answer via the edge (the vocabulary-drift / connected-fact class).
//     SUPERSEDES is deliberately NOT traversed — it is a staleness marker,
//     not a relevance path.
//  3. FILTER — drop superseded entries (any id targeted by a SUPERSEDES edge)
//     and entries whose ValidUntil has passed. Identical semantics to the
//     Phase-1 pipeline's filter.
//  4. RANK — blend query relevance and decay strength across all current
//     entries. Explicit diagnostic modes retain the old tiered-specificity
//     order and pure-decay order.
//
// An empty query degrades to the no-query "recent" mode: all current entries
// ranked by decay strength alone (the same invariant RankBySpecificity keeps).

// traversalRels are the relevance edges recall may walk. Fixed set; SUPERSEDES
// excluded by design (see package pipeline comment).
var traversalRels = []core.EdgeType{core.EdgeRelatesTo, core.EdgeOwns, core.EdgeAbout}

// maxHops is the traversal radius (dev spec §7.4: "1–2 hops"). Two hops is
// exactly entry → entity-ref ← entry, the connected-fact shape; anything
// deeper is query-language cleverness the WP's non-goals prohibit.
const maxHops = 2

// Doorfinder affinities are minimum relevance values for non-lexical hits.
// Vector similarity is direct query evidence; undirected graph adjacency is a
// deliberately lighter signal because traversal can also walk from a precise
// child back to its vague parent.
const (
	vectorAffinity    = 0.50 // TUNABLE — vector-seed relevance floor
	traversalAffinity = 0.05 // TUNABLE — graph-only relevance floor
)

// RankingMode selects how current entries are ordered after recall filtering.
type RankingMode string

const (
	// RankingBlended is the default query path: one relevance+strength score
	// across every kept entry.
	RankingBlended RankingMode = "blended"
	// RankingSpecificity preserves the legacy hard two-tier order for
	// benchmarks and fallback comparisons.
	RankingSpecificity RankingMode = "specificity"
	// RankingDecay ignores query relevance and orders only by decay strength.
	RankingDecay RankingMode = "decay"
)

// RecallOptions exposes ranking alternatives without changing Recall's
// default happy path. A zero Ranking value means RankingBlended.
type RecallOptions struct {
	Ranking RankingMode
}

// RecallMode reports which doorfinding path a recall actually used — callers
// (and the provider-outage test) must be able to tell degradation from health.
type RecallMode string

const (
	// ModeLexical: no embedder configured; WP-3 lexical+traversal recall.
	ModeLexical RecallMode = "lexical"
	// ModeVector: vector doorfinder active alongside lexical seeding (WP-4).
	ModeVector RecallMode = "vector"
	// ModeDegraded: an embedder is configured but failed; recall fell back to
	// the full WP-3 pipeline (degrade, don't fail — dev spec §8).
	ModeDegraded RecallMode = "degraded"
)

// RecallResult is a recall answer plus its provenance.
type RecallResult struct {
	Ranked []core.ScoredEntry
	Mode   RecallMode
	// Degraded carries the embedder failure when Mode == ModeDegraded. The
	// failure is surfaced, never swallowed — but it does not make the whole
	// recall an error, because lexical+traversal results remain valid.
	Degraded error
}

// Recall returns ALL current entries ranked best-first using the default
// blended query ranking. Use RecallWithOptions for diagnostic/fallback modes.
func (g *GraphStore) Recall(ctx context.Context, query string, now time.Time) (RecallResult, error) {
	return g.RecallWithOptions(ctx, query, now, RecallOptions{})
}

// RecallWithOptions returns ALL current entries ranked best-first, with their
// specificity, relevance, decay strength, and active ranking score attached.
// Like the Phase-1 pipeline, recall never hides a current entry — ranking, not
// omission, expresses relevance (callers take top-k). Blended mode treats
// lexical specificity, vector seeding, and graph traversal as relevance
// evidence in one continuous score with decay strength. RankingSpecificity
// retains the previous tier-1-then-tier-2 order exactly for comparison.
//
// Recall v2 (WP-4): when an embedder is configured, the query is embedded and
// entries whose stored content vectors clear vectorMinSim join the seed set
// (top vectorSeedK by cosine) BEFORE traversal — the doorfinder finds the
// nodes, the graph walks from them, the same filter/rank judges. Any embedder
// failure degrades to exactly the WP-3 result with Mode/Degraded reporting it.
func (g *GraphStore) RecallWithOptions(ctx context.Context, query string, now time.Time, opts RecallOptions) (RecallResult, error) {
	ranking := opts.Ranking
	if ranking == "" {
		ranking = RankingBlended
	}
	if ranking != RankingBlended && ranking != RankingSpecificity && ranking != RankingDecay {
		return RecallResult{}, fmt.Errorf("graphstore: unknown ranking mode %q", ranking)
	}
	entries, edges, err := g.Load(ctx)
	if err != nil {
		return RecallResult{}, err
	}
	mode := ModeLexical
	if g.embedder != nil {
		mode = ModeVector
	}
	if len(entries) == 0 {
		return RecallResult{Mode: mode}, nil
	}
	terms := core.Tokenize(query)

	// RESOLVE — an id may carry several records (supersession re-appends a
	// closed copy; each material recall appends a temporal update, WP-5). The
	// LATEST record per id is that id's current state; earlier records are
	// history, not extra candidates. First-appearance position is kept so
	// stable-sort tie-breaking stays deterministic.
	resolved := latestByID(entries)

	// FILTER — same staleness/supersession semantics as the P1 pipeline.
	superseded := map[core.ID]bool{}
	for _, ed := range edges {
		if ed.Type == core.EdgeSupersedes {
			superseded[core.ID(ed.To)] = true
		}
	}
	kept := make([]core.Entry, 0, len(resolved))
	for _, e := range resolved {
		if superseded[e.ID] || !e.IsCurrent(now) {
			continue
		}
		kept = append(kept, e)
	}

	// Recent mode: no query terms means no seeds and no tiering — pure decay
	// rank, identical to core.Rank regardless of the requested ranking mode.
	if len(terms) == 0 {
		return RecallResult{Ranked: rankByDecay(kept, terms, now), Mode: mode}, nil
	}

	// SEED (lexical) — current entries that match the query lexically at all.
	var seeds []core.ID
	seedSet := map[core.ID]bool{}
	specificityByID := make(map[core.ID]float64, len(kept))
	for _, e := range kept {
		specificityByID[e.ID] = core.Specificity(e, terms)
		if !seedSet[e.ID] && specificityByID[e.ID] > 0 {
			seedSet[e.ID] = true
			seeds = append(seeds, e.ID)
		}
	}

	// SEED (vector, WP-4) — the doorfinder. Failure at any step degrades to
	// the lexical pipeline and reports itself; it never empties the answer.
	var degraded error
	vectorSet := map[core.ID]bool{}
	if g.embedder != nil {
		vecSeeds, err := g.vectorSeeds(ctx, query, kept)
		if err != nil {
			mode, degraded = ModeDegraded, err
		} else {
			for _, id := range vecSeeds {
				vectorSet[id] = true
				if !seedSet[id] {
					seedSet[id] = true
					seeds = append(seeds, id)
				}
			}
		}
	}

	// EXPAND — Cypher traversal from the seeds (1–2 hops over relevance rels).
	tier1 := map[core.ID]bool{}
	reachedSet := map[core.ID]bool{}
	for id := range seedSet {
		tier1[id] = true
	}
	if len(seeds) > 0 {
		reached, err := g.Neighbors(ctx, seeds)
		if err != nil {
			return RecallResult{}, err
		}
		for _, id := range reached {
			tier1[id] = true
			reachedSet[id] = true
		}
	}

	// Partition once for both the legacy tiered ranker and the blended
	// doorfinder affinity. Tier membership is evidence in blended mode, never
	// an ordering gate.
	var t1, t2 []core.Entry
	for _, e := range kept {
		if tier1[e.ID] {
			t1 = append(t1, e)
		} else {
			t2 = append(t2, e)
		}
	}
	var ranked []core.ScoredEntry
	switch ranking {
	case RankingBlended:
		supplemental := make(map[core.ID]float64, len(vectorSet)+len(reachedSet))
		for id := range reachedSet {
			supplemental[id] = traversalAffinity
		}
		for id := range vectorSet {
			supplemental[id] = vectorAffinity
		}
		ranked = core.RankByBlend(kept, terms, supplemental, now)
	case RankingSpecificity:
		ranked = append(core.RankBySpecificity(t1, terms, now), core.RankBySpecificity(t2, terms, now)...)
	case RankingDecay:
		ranked = rankByDecay(kept, terms, now)
	}
	return RecallResult{Ranked: ranked, Mode: mode, Degraded: degraded}, nil
}

func rankByDecay(entries []core.Entry, terms []string, now time.Time) []core.ScoredEntry {
	ranked := core.Rank(entries, now)
	out := make([]core.ScoredEntry, len(ranked))
	for i, r := range ranked {
		specificity := core.Specificity(r.Entry, terms)
		out[i] = core.ScoredEntry{
			Entry: r.Entry, Specificity: specificity, Relevance: specificity,
			Strength: r.Strength, Score: r.Strength,
		}
	}
	return out
}

// latestByID collapses a record stream to one entry per id — the latest
// record's state at the first record's position (position stability keeps
// downstream stable sorts deterministic).
func latestByID(entries []core.Entry) []core.Entry {
	idx := map[core.ID]int{}
	var out []core.Entry
	for _, e := range entries {
		if i, seen := idx[e.ID]; seen {
			out[i] = e
			continue
		}
		idx[e.ID] = len(out)
		out = append(out, e)
	}
	return out
}

// vectorSeeds embeds the query and returns the ids of the top-vectorSeedK
// current entries whose stored content vectors clear vectorMinSim.
func (g *GraphStore) vectorSeeds(ctx context.Context, query string, kept []core.Entry) ([]core.ID, error) {
	qvec, err := g.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil, errClosed
	}
	stored, err := g.loadEmbeddings()
	g.mu.Unlock()
	if err != nil {
		return nil, err
	}

	type scored struct {
		id  core.ID
		sim float64
	}
	var matches []scored
	seen := map[core.ID]bool{}
	for _, e := range kept {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		vec, ok := stored[e.ID]
		if !ok {
			continue // record has no vector (embed failed at append; rebuild heals)
		}
		if sim := Cosine(qvec, vec); sim >= vectorMinSim {
			matches = append(matches, scored{id: e.ID, sim: sim})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].sim != matches[j].sim {
			return matches[i].sim > matches[j].sim
		}
		return matches[i].id < matches[j].id // deterministic tie order
	})
	if len(matches) > vectorSeedK {
		matches = matches[:vectorSeedK]
	}
	out := make([]core.ID, len(matches))
	for i, m := range matches {
		out[i] = m.id
	}
	return out, nil
}

// Neighbors returns the ids of memory records reachable within maxHops of any
// seed id over the traversal rel types (RELATES_TO/OWNS/ABOUT), in either
// direction, sorted for determinism. Entity nodes are legal intermediate stops
// (that is the connected-fact path: entry → entity-ref ← entry) but are never
// results — only records with a mem: id return. Invalid seed ids are rejected
// loudly.
//
// Seeds are interpolated as a literal list: ids are regex-validated
// `mem:YYYY-MM-DD-slug` strings (core.ID.Validate), so no quoting hazard
// exists, and a literal list keeps the query a single round trip.
func (g *GraphStore) Neighbors(ctx context.Context, seeds []core.ID) ([]core.ID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(seeds))
	for _, id := range seeds {
		if err := id.Validate(); err != nil {
			return nil, fmt.Errorf("graphstore: invalid traversal seed %q: %w", id, err)
		}
		ids = append(ids, "'"+string(id)+"'")
	}
	sort.Strings(ids) // deterministic query text
	rels := make([]string, len(traversalRels))
	for i, r := range traversalRels {
		rels[i] = string(r)
	}
	q := fmt.Sprintf(
		"MATCH (a)-[:%s*1..%d]-(b) WHERE a.id IN [%s] AND b.id IS NOT NULL RETURN DISTINCT b.id",
		strings.Join(rels, "|"), maxHops, strings.Join(ids, ", "))

	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil, errClosed
	}
	rows, err := g.queryRows(q)
	g.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("graphstore: traversal: %w", err)
	}
	var out []core.ID
	for _, row := range rows {
		if len(row) > 0 {
			if s, ok := row[0].(string); ok && s != "" {
				out = append(out, core.ID(s))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
