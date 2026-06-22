package core

import (
	"sort"
	"time"
)

// RankedEntry pairs an Entry with its pre-computed retrieval strength at a
// specific query time. The strength is computed once by Rank and cached here so
// callers can display or reason about it without recomputing.
type RankedEntry struct {
	Entry    Entry
	Strength float64 // StrengthAt(Entry, queryTime)
}

// Rank computes the retrieval strength of each entry at time now and returns
// the entries sorted by descending strength (highest-strength first). Ties
// preserve input order (stable sort).
//
// Rank does NOT filter by IsCurrent. Staleness filtering — excluding entries
// whose ValidUntil has passed — is a caller responsibility. This keeps Rank
// composable: callers can rank the full corpus, rank only current entries, or
// rank superseded entries for audit, all without modifying Rank itself.
//
// Usage pattern for retrieval (filter-then-rank):
//
//	current := slices.DeleteFunc(entries, func(e Entry) bool {
//	    return !e.IsCurrent(now)
//	})
//	ranked := core.Rank(current, now)
func Rank(entries []Entry, now time.Time) []RankedEntry {
	ranked := make([]RankedEntry, len(entries))
	for i, e := range entries {
		ranked[i] = RankedEntry{Entry: e, Strength: StrengthAt(e, now)}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Strength > ranked[j].Strength
	})
	return ranked
}
