// enso-recall runs one Ensō recall over the canonical Markdown corpus and
// prints the ranked result as JSON on stdout. It is the WP-7 process bridge:
// the OpenClaw shadow extension (and any other host) spawns it per call —
// Matt's 2026-07-18 signed choice: a boring one-shot binary, no long-lived
// sidecar until a real latency case is logged (RH-2; elapsed_ms in the output
// is that datum).
//
// READ-ONLY BY CONSTRUCTION: this binary loads the corpus and rebuilds an
// in-memory graph; it never writes to the corpus, the workspace, or anywhere
// else. Recall is a read; the only write a read may ever trigger (the
// Phase-3 material-recall bump) is deliberately NOT wired here — shadow mode
// must observe without touching (see dev-spec §12 non-goals).
//
// Usage:
//
//	enso-recall -root ~/.openclaw/workspace -query "what happened with granola?" [-k 10] [-now RFC3339]
//
// With GEMINI_API_KEY set, recall v2 (vector doorfinder) runs; without it, or
// on any provider failure, recall degrades to lexical+traversal and the JSON
// says so (mode/degraded) — degrade, don't fail (ADR-002).
//
// Output (schema version 1; the shadow extension parses this — bump the
// version field on any shape change):
//
//	{
//	  "version": 1,
//	  "query": "...",
//	  "as_of": "2026-07-18T12:00:00Z",
//	  "mode": "lexical" | "vector" | "degraded",
//	  "degraded": "",            // provider error when mode == "degraded"
//	  "elapsed_ms": 42,
//	  "corpus_entries": 35,
//	  "results": [ { "id", "type", "content", "specificity", "strength" }, ... ]
//	}
//
// Errors (unreadable corpus, malformed entries) are LOUD: message on stderr,
// exit 1, no partial JSON on stdout.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/clockworksoul/enso/internal/graphstore"
	"github.com/clockworksoul/enso/internal/mdstore"
)

// schemaVersion is the stdout JSON contract version. The shadow extension
// refuses output whose version it does not know, so bumps are deliberate.
const schemaVersion = 1

type resultJSON struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Content     string  `json:"content"`
	Specificity float64 `json:"specificity"`
	Strength    float64 `json:"strength"`
}

type outputJSON struct {
	Version       int          `json:"version"`
	Query         string       `json:"query"`
	AsOf          string       `json:"as_of"`
	Mode          string       `json:"mode"`
	Degraded      string       `json:"degraded"`
	ElapsedMS     int64        `json:"elapsed_ms"`
	CorpusEntries int          `json:"corpus_entries"`
	Results       []resultJSON `json:"results"`
}

func main() {
	root := flag.String("root", "", "corpus root (directory containing memory/); required")
	query := flag.String("query", "", "recall query; empty = recent mode (decay order)")
	k := flag.Int("k", 10, "maximum results to emit")
	nowFlag := flag.String("now", "", "as-of instant, RFC3339 (default: now UTC); for replay/tests")
	flag.Parse()

	if err := run(*root, *query, *k, *nowFlag, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "enso-recall: %v\n", err)
		os.Exit(1)
	}
}

func run(root, query string, k int, nowFlag string, out *os.File) error {
	if root == "" {
		return fmt.Errorf("-root is required")
	}
	now := time.Now().UTC()
	if nowFlag != "" {
		t, err := time.Parse(time.RFC3339, nowFlag)
		if err != nil {
			return fmt.Errorf("parse -now: %w", err)
		}
		now = t.UTC()
	}

	start := time.Now()
	ctx := context.Background()

	corpus := mdstore.NewFSStore(root)
	entries, edges, err := corpus.Load(ctx)
	if err != nil {
		return err // mdstore errors are already located (file+line) and loud
	}

	// Vector doorfinder only when a key is present; its absence or failure is
	// reported, never fatal (ADR-002 degradation contract).
	var emb graphstore.Embedder
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		emb = graphstore.GeminiEmbedder{APIKey: key}
	}

	g, err := graphstore.OpenRebuiltWith(ctx, "", emb, entries, edges)
	if err != nil {
		return err
	}
	defer g.Close()

	rr, err := g.Recall(ctx, query, now)
	if err != nil {
		return err
	}

	o := outputJSON{
		Version:       schemaVersion,
		Query:         query,
		AsOf:          now.Format(time.RFC3339),
		Mode:          string(rr.Mode),
		ElapsedMS:     time.Since(start).Milliseconds(),
		CorpusEntries: len(entries),
		Results:       []resultJSON{}, // always an array, never null
	}
	if rr.Degraded != nil {
		o.Degraded = rr.Degraded.Error()
	}
	for i, r := range rr.Ranked {
		if i >= k {
			break
		}
		o.Results = append(o.Results, resultJSON{
			ID:          string(r.Entry.ID),
			Type:        string(r.Entry.Type),
			Content:     r.Entry.Content,
			Specificity: r.Specificity,
			Strength:    r.Strength,
		})
	}

	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(o)
}
