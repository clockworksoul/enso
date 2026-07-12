package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

// P1ExitCase is one labeled case in the P1 exit measurement.
//
// Each case pairs a natural-language recall query with the structured entry
// that should surface as the top result. Cases are marked ready=true once the
// relevant structured entry has been written to the live corpus; pending cases
// (ready=false) are skipped so the test does not regress on un-written content.
type P1ExitCase struct {
	ID        string `json:"id"`
	Query     string `json:"query"`
	WantID    string `json:"want_id"`    // expected mem: ID; empty = any hit on WantTopic suffices
	WantTopic string `json:"want_topic"` // human description of expected content (for reporting)
	Notes     string `json:"notes"`
	Ready     bool   `json:"ready"`
	SkipUntil string `json:"skip_until"` // ISO date; skip if today is before this
}

// P1ExitResult is the outcome of one case.
type P1ExitResult struct {
	Case    P1ExitCase
	Skipped bool
	Hit     bool   // correct entry ranked #1
	TopID   string // ID of whatever actually ranked #1
}

// LoadP1ExitCases reads the labeled exit cases from a JSONL file.
func LoadP1ExitCases(path string) ([]P1ExitCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var cases []P1ExitCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var c P1ExitCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		cases = append(cases, c)
	}
	return cases, sc.Err()
}

// RunP1Exit loads the live FSStore corpus from corpusRoot and evaluates each
// ready case using the EnsoSpecificityModel — the full Phase-1 pipeline:
// staleness filter + supersession filter + specificity-first, decay-tiebroken
// ranking.
//
// Cases marked ready=false, or whose skip_until date has not yet passed, are
// recorded as Skipped in the results. This lets the test grow naturally as the
// corpus grows: new cases become active by flipping ready=true and writing the
// corresponding structured entry.
//
// The corpusRoot path should point to the root of the memory store (the
// directory whose memory/ subdirectory contains the daily .md files). Typically
// this is ~/.openclaw/workspace. If the directory does not exist or the memory/
// subdirectory is empty, RunP1Exit returns an empty result set (not an error),
// so CI does not break on a fresh checkout.
func RunP1Exit(corpusRoot string, cases []P1ExitCase) ([]P1ExitResult, error) {
	store := mdstore.NewFSStore(corpusRoot)
	entries, edges, err := store.Load(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load corpus: %w", err)
	}

	model := EnsoSpecificityModel{}
	now := time.Now().UTC()
	results := make([]P1ExitResult, len(cases))

	for i, c := range cases {
		results[i].Case = c

		if !c.Ready || isSkipUntilFuture(c.SkipUntil, now) {
			results[i].Skipped = true
			continue
		}

		ranked := model.RankQuery(c.Query, entries, edges, now)
		if len(ranked) == 0 {
			results[i].TopID = "(empty corpus)"
			continue
		}

		results[i].TopID = string(ranked[0].ID)
		if c.WantID != "" {
			results[i].Hit = ranked[0].ID == core.ID(c.WantID)
		} else {
			// No want_id specified: any result is acceptable for reporting
			// (case exists for tracking, not scoring).
			results[i].Hit = true
		}
	}
	return results, nil
}

// P1ExitScore returns precision@1 over the active (non-skipped) cases.
func P1ExitScore(results []P1ExitResult) (hits, active int, score float64) {
	for _, r := range results {
		if r.Skipped {
			continue
		}
		active++
		if r.Hit {
			hits++
		}
	}
	if active == 0 {
		return 0, 0, 0
	}
	return hits, active, float64(hits) / float64(active)
}

// P1ExitVerdict returns true if the structured corpus has beaten the P0
// flat-file baseline (0.63) on the active exit cases. When true, the spec
// says to PAUSE before building WP-3 (the graph) and confirm the graph is
// still needed.
func P1ExitVerdict(results []P1ExitResult) (pass bool, score float64, msg string) {
	const p0Baseline = 0.63
	hits, active, s := P1ExitScore(results)
	score = s
	if active == 0 {
		return false, 0, "no active cases — corpus not mature enough yet"
	}
	if s > p0Baseline {
		return true, s, fmt.Sprintf(
			"P1 PASS: structured corpus P@1=%.2f > P0 baseline %.2f (%d/%d active cases)",
			s, p0Baseline, hits, active)
	}
	return false, s, fmt.Sprintf(
		"P1 in progress: P@1=%.2f ≤ P0 baseline %.2f (%d/%d active cases — grow the corpus)",
		s, p0Baseline, hits, active)
}

func isSkipUntilFuture(skipUntil string, now time.Time) bool {
	if skipUntil == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", skipUntil)
	if err != nil {
		return false
	}
	return now.Before(t)
}
