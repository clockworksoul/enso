package core

import (
	"sort"
	"strings"
	"time"
)

// specificity.go — Stage 5: specificity-aware ranking for the NEIGHBOR/path
// miss class.
//
// # The miss this addresses
//
// NEIGHBOR/path is the DRM/centroid-adjacent failure: a vaguer PARENT entry is
// centroid-adjacent to the query and looks fresher (it is general background
// knowledge that keeps being referenced), so pure decay ranking prefers it over
// the specific CHILD that actually answers the query. The canonical real case
// (2026-06-23): asked "where does the enso repo live locally?", the model held
// the parent fact ("clockworksoul repos live under ~/workspace/clockworksoul/")
// and confabulated "not found" for the child ("...clockworksoul/enso"). Both
// recency-baseline and the decay-only Ensō model pick the parent.
//
// # The signal
//
// The child entry literally matches the query better: the query token "enso"
// appears in the child's Tags/About but NOT in the parent's. That is the
// load-bearing, content-grounded signal — no embeddings, no graph traversal
// required for the common case. A term that appears in an entry's Tags or About
// means the entry is *specifically about* that thing (a strong match); a term
// in the free-text Content is a weaker match.
//
// # The design rule (AGENTS.md: Complexity Kills, Simplicity Scales)
//
// Specificity is a QUERY-DEPENDENT signal; the existing Rank is query-
// independent (pure decay). So Stage 5 is a query-aware ranker layered ON TOP
// of decay, not a replacement. The invariant that keeps every STALE case green:
//
//	An empty query degrades RankBySpecificity to exactly Rank (pure decay).
//
// With no query terms there is no specificity to measure, so the function MUST
// behave identically to the decay-only path. STALE cases (which the harness
// replays with the recent/empty-query mode) are unaffected by construction.

// stopWords are high-frequency query tokens that carry no specificity signal.
// Kept deliberately tiny: only words that appeared as noise in the real query
// corpus ("where does the X live locally?"). This is NOT a general NLP stoplist
// — it is the minimum needed so generic scaffolding words don't dilute or, worse,
// spuriously match across unrelated entries. Expand only when a real case
// demands it (YAGNI).
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "was": true,
	"were": true, "of": true, "to": true, "in": true, "on": true, "at": true,
	"for": true, "and": true, "or": true, "where": true, "what": true,
	"when": true, "which": true, "who": true, "does": true, "do": true,
	"did": true, "live": true, "lives": true, "located": true, "locally": true,
	"local": true, "it": true, "its": true,
}

// Tokenize lowercases s and splits it into content-bearing terms: maximal runs
// of letters/digits, with common path/punctuation separators treated as
// boundaries, then stop-words dropped. It is the SHARED tokenizer for both the
// query and an entry's searchable surface, so the two tokenize identically (a
// query term and an entry token match iff their normalized forms are equal).
//
// Path-ish input matters here: "~/workspace/clockworksoul/enso" must yield the
// component token "enso", which is exactly the specific signal NEIGHBOR/path
// needs. Splitting on any non-alphanumeric rune achieves that without a path
// parser.
func Tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !isAlphaNum(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == "" || stopWords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// Specificity weights. A query term found in an entry's Tags or About is a
// STRONG match (the entry is curated to be *about* that term); a term found
// only in free-text Content is a WEAK match (it mentions the term in passing).
// The asymmetry is the whole point: the specific child carries "enso" as a tag,
// the vague parent only ever mentions "clockworksoul". TUNABLE — these are
// Phase-1 priors, calibrate against the corpus if more NEIGHBOR cases land.
const (
	specStructuralWeight = 1.0 // TUNABLE — term matched in Tags/About
	specContentWeight    = 0.4 // TUNABLE — term matched only in Content
)

// Specificity scores how specifically entry e answers a query described by
// queryTerms (already tokenized via Tokenize). For each DISTINCT query term it
// adds the strongest match weight that term earns against e:
//
//	term ∈ e.Tags ∪ e.About            → +specStructuralWeight  (strong)
//	term ∈ tokens(e.Content) (only)    → +specContentWeight     (weak)
//	term matches nothing in e          → +0
//
// The result is normalized by len(queryTerms) so it lies in [0, structural
// weight] and is comparable across entries for the SAME query. Each query term
// contributes at most once (distinct terms), so a parent that merely repeats a
// generic word cannot inflate its score.
//
// Returns 0 when queryTerms is empty — the load-bearing invariant that makes
// RankBySpecificity degrade to pure decay on empty/recent-mode queries.
func Specificity(e Entry, queryTerms []string) float64 {
	if len(queryTerms) == 0 {
		return 0
	}

	// Build the entry's match surfaces once.
	structural := map[string]bool{} // tokens from Tags + About (strong)
	for _, t := range e.Tags {
		for _, tok := range Tokenize(t) {
			structural[tok] = true
		}
	}
	for _, a := range e.About {
		for _, tok := range Tokenize(a) {
			structural[tok] = true
		}
	}
	content := map[string]bool{} // tokens from Content (weak)
	for _, tok := range Tokenize(e.Content) {
		content[tok] = true
	}

	// Distinct query terms only.
	seen := map[string]bool{}
	var sum float64
	var n int
	for _, qt := range queryTerms {
		if seen[qt] {
			continue
		}
		seen[qt] = true
		n++
		switch {
		case structural[qt]:
			sum += specStructuralWeight
		case content[qt]:
			sum += specContentWeight
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// ScoredEntry pairs an Entry with its specificity (query-match) and strength
// (decay) at query time, so callers can inspect why an entry ranked where it
// did. Specificity is the primary sort key; Strength is the tiebreaker.
type ScoredEntry struct {
	Entry       Entry
	Specificity float64 // Specificity(Entry, queryTerms) — primary key
	Strength    float64 // StrengthAt(Entry, now)         — tiebreaker
}

// RankBySpecificity is the Stage 5 query-aware ranker. It sorts entries by
// specificity first (does this entry actually match the query?), then by decay
// strength (of the equally-specific entries, which is freshest/most-consolidated?).
//
// CRITICAL invariant (preserves all STALE cases): when queryTerms is empty,
// every Specificity is 0, the primary key is constant, and the stable sort
// falls through to the Strength tiebreaker — i.e. the result is IDENTICAL to
// Rank (pure decay). Callers in "recent"/no-query mode lose nothing.
//
// Like Rank, this does NOT filter by IsCurrent or supersession; the caller
// composes filter-then-rank (the EnsoModel pipeline does exactly that). Keeping
// specificity orthogonal to staleness is deliberate: a specific match that has
// been superseded must still be droppable by the staleness filter before this
// ranker ever sees it.
func RankBySpecificity(entries []Entry, queryTerms []string, now time.Time) []ScoredEntry {
	scored := make([]ScoredEntry, len(entries))
	for i, e := range entries {
		scored[i] = ScoredEntry{
			Entry:       e,
			Specificity: Specificity(e, queryTerms),
			Strength:    StrengthAt(e, now),
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Specificity != scored[j].Specificity {
			return scored[i].Specificity > scored[j].Specificity
		}
		return scored[i].Strength > scored[j].Strength
	})
	return scored
}
