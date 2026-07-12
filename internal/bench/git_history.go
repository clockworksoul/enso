package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// GitHistoryRecord is the JSONL record produced by cmd/corpus-builder.
// It captures a single supersession event from the MEMORY.md git history:
// the stale text that was removed and the current text that replaced it.
type GitHistoryRecord struct {
	ID            string `json:"id"`
	HashStale     string `json:"hash_stale"`
	HashCurrent   string `json:"hash_current"`
	StaleDate     string `json:"stale_date"`
	CurrentDate   string `json:"current_date"`
	StaleText     string `json:"stale_text"`
	CurrentText   string `json:"current_text"`
	Query         string `json:"query"`
	SectionHeader string `json:"section_header"`
	Category      string `json:"category"`
}

// LoadGitHistoryRecords reads a JSONL file of GitHistoryRecord values.
func LoadGitHistoryRecords(path string) ([]GitHistoryRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var records []GitHistoryRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // 1MB line buffer for long entries
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r GitHistoryRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("decode line: %w", err)
		}
		records = append(records, r)
	}
	return records, sc.Err()
}

// GitHistoryCases converts raw JSONL records into bench Cases suitable for
// running through Run() or RunQueryAware().
//
// Each record becomes a Case with:
//   - Two candidates: the stale entry (ValidUntil=currentDate) and the
//     current entry (no ValidUntil).
//   - One SUPERSEDES edge: current → stale.
//   - WantID: the current entry.
//   - AsOf: one day after the current commit date (the query arrives after the
//     supersession has landed).
//
// The BaselineModel (recency-only) will correctly surface the current entry
// because it has the later EncodedTime. The EnsoModel additionally uses the
// SUPERSEDES edge and ValidUntil to explicitly filter the stale entry — it is
// correct for the right reason. Future semantic-only baseline variants
// (embedding similarity without temporal ranking) are where the differentiation
// becomes load-bearing.
func GitHistoryCases(records []GitHistoryRecord) ([]Case, error) {
	var cases []Case
	for _, r := range records {
		c, err := recordToCase(r)
		if err != nil {
			// Skip malformed records rather than aborting the whole corpus.
			continue
		}
		cases = append(cases, c)
	}
	return cases, nil
}

func recordToCase(r GitHistoryRecord) (Case, error) {
	staleDate, err := time.Parse("2006-01-02", r.StaleDate)
	if err != nil {
		return Case{}, fmt.Errorf("parse stale_date %q: %w", r.StaleDate, err)
	}
	currentDate, err := time.Parse("2006-01-02", r.CurrentDate)
	if err != nil {
		return Case{}, fmt.Errorf("parse current_date %q: %w", r.CurrentDate, err)
	}

	// The stale entry's validity ended when the current entry was written.
	staleEntry, err := core.NewEntry(core.NewEntryParams{
		ID:          mustNewID(staleDate, r.ID+"-stale"),
		Type:        core.TypeFact,
		Content:     r.StaleText,
		EncodedTime: staleDate,
		ValidUntil:  &currentDate, // closed when superseded
		Confidence:  core.ConfHigh,
		Tags:        []string{"git-history", r.Category},
	})
	if err != nil {
		return Case{}, fmt.Errorf("build stale entry: %w", err)
	}

	currentEntry, err := core.NewEntry(core.NewEntryParams{
		ID:          mustNewID(currentDate, r.ID+"-current"),
		Type:        core.TypeFact,
		Content:     r.CurrentText,
		EncodedTime: currentDate,
		Confidence:  core.ConfHigh,
		Tags:        []string{"git-history", r.Category},
	})
	if err != nil {
		return Case{}, fmt.Errorf("build current entry: %w", err)
	}

	// Query arrives one day after the supersession so both entries exist
	// in the store but the stale one's ValidUntil has passed.
	asOf := currentDate.AddDate(0, 0, 1)

	edge := core.Edge{
		From: currentEntry.ID,
		Type: core.EdgeSupersedes,
		To:   string(staleEntry.ID),
	}

	return Case{
		Name:       r.ID,
		MissClass:  "STALE",
		Query:      r.Query,
		AsOf:       asOf,
		WantID:     currentEntry.ID,
		Candidates: []core.Entry{staleEntry, currentEntry},
		Edges:      []core.Edge{edge},
	}, nil
}

// mustNewID constructs a core.ID, panicking on error. Suitable only for bench
// test support where invalid input is a programming error.
func mustNewID(date time.Time, label string) core.ID {
	id, err := core.NewID(date, label)
	if err != nil {
		panic(fmt.Sprintf("mustNewID(%q, %q): %v", date.Format("2006-01-02"), label, err))
	}
	return id
}
