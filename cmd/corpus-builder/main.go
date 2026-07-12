// corpus-builder extracts supersession pairs from the MEMORY.md git history
// and writes them as a JSONL file suitable for use in the git_history bench.
//
// Usage:
//
//	corpus-builder -repo <path-to-memory-repo> -out <output.jsonl>
//
// The tool walks every consecutive pair of commits that touched MEMORY.md,
// extracts changed hunks from the unified diff, filters for genuine
// supersession candidates (both sides non-trivially large, not a truncation
// expansion), synthesizes a heuristic query from the nearest section header,
// and emits one JSON record per candidate.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Record is the JSONL record type written by this tool and read by the bench.
type Record struct {
	ID            string `json:"id"`
	HashStale     string `json:"hash_stale"`
	HashCurrent   string `json:"hash_current"`
	StaleDate     string `json:"stale_date"`
	CurrentDate   string `json:"current_date"`
	StaleText     string `json:"stale_text"`
	CurrentText   string `json:"current_text"`
	Query         string `json:"query"`
	SectionHeader string `json:"section_header"`
	Category      string `json:"category"` // "supersession" | "addition" | "deletion"
}

const (
	minChars = 80   // minimum chars on each side to be a meaningful change
	maxRatio = 10.0 // max added/removed ratio — filters truncation expansions
)

// truncMarkers: if the removed text contains any of these, it's probably a
// truncation placeholder being expanded, not a real belief supersession.
var truncMarkers = []string{"truncated", "…", "...", "omitted", "kept ", "chars of"}

// headerRe matches markdown section headers (## or ###).
var headerRe = regexp.MustCompile(`^#{2,3}\s+(.+)`)

// dateRe strips trailing parenthetical dates and emojis from headers.
var dateRe = regexp.MustCompile(`\s*[\(\[].*?[\)\]]|\s*\p{So}+|\s*\p{Sm}+`)

func main() {
	repo := flag.String("repo", "", "path to the memory git repo")
	out := flag.String("out", "", "output JSONL file path")
	flag.Parse()

	if *repo == "" || *out == "" {
		log.Fatal("usage: corpus-builder -repo <path> -out <file.jsonl>")
	}

	records, err := extract(*repo)
	if err != nil {
		log.Fatalf("extraction: %v", err)
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("create %s: %v", *out, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			log.Fatalf("encode: %v", err)
		}
	}
	fmt.Fprintf(os.Stderr, "wrote %d records to %s\n", len(records), *out)
}

// commit is one entry in the MEMORY.md git log.
type commit struct{ hash, date string }

// hunk is a parsed diff hunk: removed lines, added lines, and the nearest
// section header extracted from the hunk header context or context lines.
type hunk struct {
	removed       []string
	added         []string
	sectionHeader string
}

func extract(repo string) ([]Record, error) {
	commits, err := logCommits(repo)
	if err != nil {
		return nil, err
	}

	var records []Record
	seen := map[string]int{}

	for i, curr := range commits[:len(commits)-1] {
		prev := commits[i+1]

		hunks, err := diffHunks(repo, prev.hash, curr.hash)
		if err != nil {
			continue
		}

		for _, h := range hunks {
			rec := classify(h, curr, prev, seen)
			if rec != nil {
				records = append(records, *rec)
			}
		}
	}
	return records, nil
}

func logCommits(repo string) ([]commit, error) {
	cmd := exec.Command("git", "log", "--format=%H %cd", "--date=short", "--", "MEMORY.md")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	var commits []commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 {
			commits = append(commits, commit{parts[0], parts[1]})
		}
	}
	return commits, nil
}

func diffHunks(repo, prevHash, currHash string) ([]hunk, error) {
	cmd := exec.Command("git", "diff", prevHash, currHash, "--", "MEMORY.md")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	return parseHunks(string(out)), nil
}

// hunkHeaderRe matches a unified diff hunk header line.
// The optional trailing context (after the last @@) is the section heading.
var hunkHeaderRe = regexp.MustCompile(`^@@[^@]+@@\s*(.*)$`)

func parseHunks(diff string) []hunk {
	lines := strings.Split(diff, "\n")
	var hunks []hunk
	var cur *hunk

	for _, line := range lines {
		if m := hunkHeaderRe.FindStringSubmatch(line); m != nil {
			if cur != nil {
				hunks = append(hunks, *cur)
			}
			cur = &hunk{sectionHeader: cleanHeader(m[1])}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			// file header lines — skip
		case strings.HasPrefix(line, "-"):
			cur.removed = append(cur.removed, line[1:])
		case strings.HasPrefix(line, "+"):
			cur.added = append(cur.added, line[1:])
		default:
			// context line — check if it's a section header we can capture
			// (overrides hunk header if a closer one appears)
			stripped := strings.TrimSpace(line)
			if m := headerRe.FindStringSubmatch(stripped); m != nil {
				cur.sectionHeader = cleanHeader(m[1])
			}
		}
	}
	if cur != nil {
		hunks = append(hunks, *cur)
	}
	return hunks
}

// cleanHeader strips markdown hashes, trailing date parentheticals, and excess
// whitespace from a section header, leaving a clean topic string.
func cleanHeader(s string) string {
	// strip leading # chars
	s = strings.TrimLeft(s, "#")
	// strip dates and emoji-ish characters (rough heuristic)
	s = regexp.MustCompile(`\s*[\(\[][^)\]]*[\)\]]`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s*(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{4}`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s*\d{4}-\d{2}-\d{2}`).ReplaceAllString(s, "")
	// strip common emoji prefixes (basic ranges)
	s = regexp.MustCompile(`[\x{1F300}-\x{1FFFF}\x{2600}-\x{27FF}]+\s*`).ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	// strip any remaining leading/trailing punctuation that looks like markdown
	s = strings.Trim(s, " \t-–—")
	return s
}

// synthesizeQuery turns a section header into a natural recall question.
func synthesizeQuery(header string) string {
	if header == "" {
		return ""
	}
	// Strip trailing status words that would make a weird question.
	topic := strings.TrimSuffix(strings.TrimSuffix(header, " — RESOLVED"), " ✅")
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return ""
	}
	return fmt.Sprintf("What is the current status of %s?", topic)
}

func isTruncation(text string) bool {
	lower := strings.ToLower(text)
	for _, m := range truncMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func classify(h hunk, curr, prev commit, seen map[string]int) *Record {
	rText := strings.Join(h.removed, " ")
	aText := strings.Join(h.added, " ")
	rLen, aLen := len(strings.TrimSpace(rText)), len(strings.TrimSpace(aText))

	// Must be a genuine supersession: both sides non-trivially large.
	if rLen < minChars || aLen < minChars {
		return nil
	}
	// Filter out truncation expansions.
	ratio := float64(aLen) / float64(rLen)
	if isTruncation(rText) || ratio > maxRatio {
		return nil
	}

	query := synthesizeQuery(h.sectionHeader)
	if query == "" {
		// Fallback: derive topic from first sentence of added text.
		first := strings.SplitN(strings.TrimSpace(aText), ".", 2)[0]
		if len(first) > 60 {
			first = first[:60]
		}
		query = fmt.Sprintf("What happened with: %s?", strings.TrimSpace(first))
	}

	// Unique ID: date + sequential counter per date.
	seen[curr.date]++
	id := fmt.Sprintf("gh-%s-%03d", curr.date, seen[curr.date])

	return &Record{
		ID:            id,
		HashStale:     prev.hash[:8],
		HashCurrent:   curr.hash[:8],
		StaleDate:     prev.date,
		CurrentDate:   curr.date,
		StaleText:     strings.TrimSpace(rText),
		CurrentText:   strings.TrimSpace(aText),
		Query:         query,
		SectionHeader: h.sectionHeader,
		Category:      "supersession",
	}
}
