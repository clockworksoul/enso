// Package harvest is a one-shot NEIGHBOR-miss discovery tool. It scans the live
// memory files (MEMORY.md + memory/*.md), loads all entries via mdstore.Parse,
// and replays a battery of real queries using both the recency-baseline model
// and the Stage-5 specificity model. Cases where the two models disagree — i.e.
// specificity would promote a different entry to the top — are flagged as
// NEIGHBOR/path candidates and written to a human-confirmation queue.
//
// This is NOT a benchmark and NOT a test. It runs once, produces a queue, and
// stops. A human (Matt) confirms each candidate before it enters bench/cases.go
// as a real NeighborCase. Nothing is written to the corpus automatically.
//
// Usage:
//
//	go run ./internal/bench/harvest/ [--store <path>] [--out <path>]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
	"github.com/clockworksoul/enso/internal/memstore"
)

// harvestQuery is one probe against the real memory corpus.
type harvestQuery struct {
	Query       string
	Explanation string // why this is a NEIGHBOR/path candidate
}

// queries is the battery, sourced from real conversation topics where a
// specific-vs-vague split is plausible in the live memory.
var queries = []harvestQuery{
	{"enso repo path", "Specific Ensō repo location vs. general Ensō project notes"},
	{"enso phase 1 loop", "Phase-1 loop state vs. general Ensō overview"},
	{"enso neighbor miss", "NEIGHBOR-class specifics vs. general miss taxonomy"},
	{"granola transcripts", "Granola-specific skill vs. general meeting/transcript notes"},
	{"neo4j blog post outline", "Specific outline vs. general blog or Neo4j notes"},
	{"omega auradb metrics cmi", "AuraDB CMI scrape specifics vs. general Omega/Neo4j notes"},
	{"tipa development trajectory", "Specific research doc vs. general team notes"},
	{"checkly browser check cost", "Browser-check spend specifics vs. general Checkly/PLR notes"},
	{"peng incident investigation assistant", "IR-assistant research vs. general Peng or IR notes"},
	{"shikhar t5 promotion packet", "T5 packet work vs. general Shikhar notes"},
	{"live topology productionization axon", "AXON-2420–2435 specifics vs. general Live Topology notes"},
	{"plr phase 2 migration july deadline", "Jul-17 deadline specifics vs. general PLR notes"},
	{"external services alloy neo4j", "ExternalServicesAlloy CR specifics vs. general Alloy notes"},
	{"adam 1on1 jira hygiene visibility", "Jun-29 1:1 specifics vs. general Adam/1:1 notes"},
	{"reliability vision 2027 omega", "Specific refresh doc vs. general reliability notes"},
	{"enso synthetic expectations corpus", "SyntheticExpectations bucket vs. general Ensō bench notes"},
	{"dross hour enso detector vocabulary", "Detector-vocabulary work vs. general Dross Hour notes"},
}

func main() {
	home, _ := os.UserHomeDir()
	defaultStore := filepath.Join(home, ".openclaw", "workspace")
	defaultOut := filepath.Join(home, ".openclaw", "workspace", "research",
		fmt.Sprintf("neighbor-harvest-%s.md", time.Now().Format("2006-01-02")))

	storePath := flag.String("store", defaultStore, "workspace path (contains MEMORY.md + memory/)")
	outPath := flag.String("out", defaultOut, "output markdown file for confirmation queue")
	flag.Parse()

	fmt.Printf("Ensō NEIGHBOR harvest — %s\n", time.Now().Format("2006-01-02 15:04 MST"))
	fmt.Printf("Store: %s\n", *storePath)

	// --- Load all memory entries from disk ---
	entries, edges, err := loadAll(*storePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load memory: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded: %d entries, %d edges from memory files\n\n", len(entries), len(edges))

	// Seed an in-memory store so we can call Query-like operations uniformly.
	ms := memstore.New()
	if err := ms.Append(context.Background(), entries, edges); err != nil {
		fmt.Fprintf(os.Stderr, "seed memstore: %v\n", err)
		os.Exit(1)
	}

	now := time.Now()
	var candidates []candidateResult

	for _, q := range queries {
		res := probe(q, entries, edges, now)
		symbol := "  "
		if res.IsCandidate {
			symbol = "🚩"
			candidates = append(candidates, res)
		}
		fmt.Printf("%s %-48q → %s\n", symbol, q.Query, res.Signal)
	}

	fmt.Printf("\n── harvest complete: %d queries, %d candidates ──\n\n",
		len(queries), len(candidates))

	if err := writeQueue(*outPath, candidates, now); err != nil {
		fmt.Fprintf(os.Stderr, "write queue: %v\n", err)
		os.Exit(1)
	}
	if len(candidates) > 0 {
		fmt.Printf("Confirmation queue written: %s\n", *outPath)
	} else {
		fmt.Println("No candidates — no confirmation queue needed.")
	}
}

// candidateResult is one flagged query's probe output.
type candidateResult struct {
	Query       string
	Explanation string
	RecencyTop  core.Entry // what baseline ranked #1
	SpecificTop core.Entry // what specificity ranked #1 (differs from RecencyTop)
	RecencyRest []core.Entry
	IsCandidate bool
	Signal      string
}

func probe(q harvestQuery, entries []core.Entry, edges []core.Edge, now time.Time) candidateResult {
	res := candidateResult{Query: q.Query, Explanation: q.Explanation}

	ranked := core.Rank(entries, now)
	queryTerms := core.Tokenize(q.Query)
	specific := core.RankBySpecificity(entries, queryTerms, now)

	if len(ranked) == 0 {
		res.Signal = "no entries"
		return res
	}
	res.RecencyTop = ranked[0].Entry
	if len(ranked) > 1 {
		for _, r := range ranked[1:4] {
			res.RecencyRest = append(res.RecencyRest, r.Entry)
		}
	}
	if len(specific) == 0 {
		res.Signal = "no entries after specificity filter"
		return res
	}
	res.SpecificTop = specific[0].Entry

	if res.SpecificTop.ID != res.RecencyTop.ID {
		res.IsCandidate = true
		res.Signal = fmt.Sprintf("recency=%q specificity promotes=%q",
			summarize(res.RecencyTop.Content, 50),
			summarize(res.SpecificTop.Content, 50))
	} else {
		res.Signal = fmt.Sprintf("agree on %q", summarize(res.RecencyTop.Content, 50))
	}
	return res
}

// loadAll walks the workspace and parses every .md file through mdstore.Parse,
// collecting all entries and edges. Files that don't parse as Ensō store format
// (no recognized blocks) are silently skipped — most memory files won't be
// Ensō-format and that's fine; the parse returns empty slices, not an error.
func loadAll(workspace string) ([]core.Entry, []core.Edge, error) {
	paths := []string{filepath.Join(workspace, "MEMORY.md")}
	memDir := filepath.Join(workspace, "memory")
	if infos, err := os.ReadDir(memDir); err == nil {
		for _, info := range infos {
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
				paths = append(paths, filepath.Join(memDir, info.Name()))
			}
		}
	}

	var allEntries []core.Entry
	var allEdges []core.Edge
	seen := map[core.ID]bool{}

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		es, eds, err := mdstore.Parse(string(raw))
		if err != nil {
			continue // not an Ensō-format file; skip
		}
		for _, e := range es {
			if !seen[e.ID] {
				seen[e.ID] = true
				allEntries = append(allEntries, e)
			}
		}
		allEdges = append(allEdges, eds...)
	}

	// Sort entries by EncodedTime descending for deterministic baseline output.
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].EncodedTime.After(allEntries[j].EncodedTime)
	})
	return allEntries, allEdges, nil
}

func writeQueue(path string, candidates []candidateResult, now time.Time) error {
	if len(candidates) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Ensō NEIGHBOR Harvest — Confirmation Queue\n\n")
	b.WriteString(fmt.Sprintf("*Generated %s. These are CANDIDATES only — not confirmed misses.*\n\n",
		now.Format("2006-01-02 15:04 MST")))
	b.WriteString("**For each candidate:** read the two entries and decide:\n")
	b.WriteString("- `CONFIRM` → recency ranked a vague parent first; the specific child should have won. Add to `bench/cases.go` as a `NeighborCase`.\n")
	b.WriteString("- `REJECT` → the recency result is correct, or both entries are equally specific.\n")
	b.WriteString("- `INVESTIGATE` → neither clearly right; needs more context.\n\n")
	b.WriteString("---\n\n")

	for i, c := range candidates {
		b.WriteString(fmt.Sprintf("## Candidate %d — `%s`\n\n", i+1, c.Query))
		b.WriteString(fmt.Sprintf("**Why flagged:** %s\n\n", c.Explanation))
		b.WriteString(fmt.Sprintf("**Harvest signal:** %s\n\n", c.Signal))
		b.WriteString("**Recency top (potential vague parent):**\n```\n")
		b.WriteString(fmt.Sprintf("ID:      %s\nContent: %s\n", c.RecencyTop.ID, truncate(c.RecencyTop.Content, 400)))
		b.WriteString("```\n\n")
		b.WriteString("**Specificity top (potential specific child):**\n```\n")
		b.WriteString(fmt.Sprintf("ID:      %s\nContent: %s\n", c.SpecificTop.ID, truncate(c.SpecificTop.Content, 400)))
		b.WriteString("```\n\n")
		if len(c.RecencyRest) > 0 {
			b.WriteString("**Recency 2nd–4th (context):**\n")
			for j, e := range c.RecencyRest {
				b.WriteString(fmt.Sprintf("  %d. `%s` — %s\n", j+2, e.ID, summarize(e.Content, 80)))
			}
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("**Decision:** `CONFIRM` / `REJECT` / `INVESTIGATE`\n\n"))
		b.WriteString("**Notes:**\n\n---\n\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0600)
}

func summarize(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(s))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
