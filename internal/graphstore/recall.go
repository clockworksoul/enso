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
//  4. RANK — core.RankBySpecificity (specificity-first, decay-tiebroken),
//     unchanged from Phase 1.
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

// Recall returns ALL current entries ranked best-first, with their
// specificity and decay-strength scores attached. Like the Phase-1 pipeline,
// recall never hides a current entry — ranking, not omission, expresses
// relevance (callers take top-k). The graph's contribution is a two-tier
// order:
//
//	tier 1: entries that match the query lexically (seeds) PLUS entries
//	        reached from a seed by traversal — ranked specificity-first,
//	        decay-tiebroken (core.RankBySpecificity, unchanged).
//	tier 2: every other current entry, in pure decay order.
//
// The tiering is where connected-fact retrieval becomes real: an entry that
// matches nothing lexically but is EDGE-CONNECTED to a match outranks
// unconnected noise, which no flat-file ranker can express. On a corpus with
// no edges (or a query matching nothing) the tiers collapse to exactly the
// Phase-1 RankBySpecificity order, so the graph strictly adds reach and never
// costs parity.
//
// Recall v2 (WP-4): when an embedder is configured, the query is embedded and
// entries whose stored content vectors clear vectorMinSim join the seed set
// (top vectorSeedK by cosine) BEFORE traversal — the doorfinder finds the
// nodes, the graph walks from them, the same filter/rank judges. Any embedder
// failure degrades to exactly the WP-3 result with Mode/Degraded reporting it.
func (g *GraphStore) Recall(ctx context.Context, query string, now time.Time) (RecallResult, error) {
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

	// FILTER — same staleness/supersession semantics as the P1 pipeline.
	superseded := map[core.ID]bool{}
	for _, ed := range edges {
		if ed.Type == core.EdgeSupersedes {
			superseded[core.ID(ed.To)] = true
		}
	}
	kept := make([]core.Entry, 0, len(entries))
	for _, e := range entries {
		if superseded[e.ID] || !e.IsCurrent(now) {
			continue
		}
		kept = append(kept, e)
	}

	// Recent mode: no query terms means no seeds and no tiering — pure decay
	// rank, identical to core.Rank (the RankBySpecificity degradation
	// invariant).
	if len(terms) == 0 {
		return RecallResult{Ranked: core.RankBySpecificity(kept, terms, now), Mode: mode}, nil
	}

	// SEED (lexical) — current entries that match the query lexically at all.
	var seeds []core.ID
	seedSet := map[core.ID]bool{}
	for _, e := range kept {
		if !seedSet[e.ID] && core.Specificity(e, terms) > 0 {
			seedSet[e.ID] = true
			seeds = append(seeds, e.ID)
		}
	}

	// SEED (vector, WP-4) — the doorfinder. Failure at any step degrades to
	// the lexical pipeline and reports itself; it never empties the answer.
	var degraded error
	if g.embedder != nil {
		vecSeeds, err := g.vectorSeeds(ctx, query, kept)
		if err != nil {
			mode, degraded = ModeDegraded, err
		} else {
			for _, id := range vecSeeds {
				if !seedSet[id] {
					seedSet[id] = true
					seeds = append(seeds, id)
				}
			}
		}
	}

	// EXPAND — Cypher traversal from the seeds (1–2 hops over relevance rels).
	tier1 := map[core.ID]bool{}
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
		}
	}

	// RANK — tier 1 then tier 2, each via the unchanged core ranker (tier 2
	// entries all score specificity 0, so their RankBySpecificity order IS
	// pure decay order).
	var t1, t2 []core.Entry
	for _, e := range kept {
		if tier1[e.ID] {
			t1 = append(t1, e)
		} else {
			t2 = append(t2, e)
		}
	}
	ranked := append(core.RankBySpecificity(t1, terms, now), core.RankBySpecificity(t2, terms, now)...)
	return RecallResult{Ranked: ranked, Mode: mode, Degraded: degraded}, nil
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
