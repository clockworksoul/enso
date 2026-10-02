// enso-recall runs one Ensō recall over the canonical Markdown corpus and
// prints the ranked result as JSON on stdout. It is the WP-7 process bridge:
// the OpenClaw shadow extension (and any other host) spawns it per call —
// Matt's 2026-07-18 signed choice: a boring one-shot binary, no long-lived
// sidecar until a real latency case is logged (RH-2; elapsed_ms in the output
// is that datum).
//
// # Persisted index (WP-8 blocker (b) fix, 2026-10-01)
//
// Each call used to rebuild a FRESH in-memory graph (dbPath="") with the
// embedder attached, which re-embeds the ENTIRE corpus over the network at
// append time — measured at ~19s for 68 entries and growing linearly
// (docs/2026-09-30-wp8-latency-rootcause-corpus-reembed.md). The embeddings
// are DERIVED data meant to live in the KùzuDB index (ADR-002 / WP-4); they
// just were never persisted by this binary.
//
// enso-recall now keeps an on-disk index at <root>/.enso/index.kuzu (override
// with -index). On the WARM path (index present and no memory/*.md file newer
// than it) it Opens the persisted graph and attaches the embedder ONLY for the
// single query embed — no corpus re-embed, measured ~0.46s end to end, ~40x
// faster and ~9x under the 4s shadow deadline. On the COLD path (index absent,
// stale, or -rebuild) it rebuilds the on-disk index from the Markdown corpus,
// paying the one-time embed cost, then recalls. A wrong staleness call degrades
// to a rebuild, never to corruption: the index is derived and the
// kill-the-graph drill proves it is always reconstructable from Markdown
// (INV-1). The corpus itself is still never written.
//
// READ-ONLY WITH RESPECT TO THE CORPUS BY CONSTRUCTION: this binary loads the
// Markdown corpus and (re)builds a derived index UNDER <root>/.enso/; it never
// writes to the memory/ substrate, the workspace, or anywhere else. The index
// directory is derived cache, not canonical memory. Recall is a read; the only
// write a read may ever trigger (the Phase-3 material-recall bump) is
// deliberately NOT wired here — shadow mode must observe without touching the
// corpus (see dev-spec §12 non-goals).
//
// Usage:
//
//	enso-recall -root ~/.openclaw/workspace -query "what happened with granola?" [-k 10] [-now RFC3339] [-index PATH] [-rebuild]
//
// With GEMINI_API_KEY set, recall v2 (vector doorfinder) runs; without it, or
// on any provider failure, recall degrades to lexical+traversal and the JSON
// says so (mode/degraded) — degrade, don't fail (ADR-002).
//
// Output (schema version 1; the shadow extension parses this. The index_path
// and index_built fields are ADDITIVE — a strict superset the existing bridge
// ignores, so the version stays 1; bump the version field only on a
// NON-additive shape change the bridge must refuse):
//
//	{
//	  "version": 1,
//	  "query": "...",
//	  "as_of": "2026-07-18T12:00:00Z",
//	  "mode": "lexical" | "vector" | "degraded",
//	  "degraded": "",            // provider error when mode == "degraded"
//	  "elapsed_ms": 42,
//	  "corpus_entries": 35,
//	  "index_path": ".../.enso/index.kuzu",  // "" when a fresh in-memory index was used
//	  "index_built": false,      // true when this call paid the cold rebuild+embed cost
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
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/clockworksoul/enso/internal/graphstore"
	"github.com/clockworksoul/enso/internal/mdstore"
)

// schemaVersion is the stdout JSON contract version. The shadow extension
// refuses output whose version it does not know, so bumps are deliberate.
// index_path/index_built were added as an ADDITIVE superset (2026-10-01), so
// the version stays 1 — the bridge reads only known fields and ignores extras.
const schemaVersion = 1

// defaultIndexRelPath is where the persisted KùzuDB index lives relative to the
// corpus root, under the .enso/ directory the shadow logs already use. Derived
// cache, safe to delete (kill-the-graph drill rebuilds it from Markdown).
const defaultIndexRelPath = ".enso/index.kuzu"

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
	IndexPath     string       `json:"index_path"`
	IndexBuilt    bool         `json:"index_built"`
	Results       []resultJSON `json:"results"`
}

func main() {
	root := flag.String("root", "", "corpus root (directory containing memory/); required")
	query := flag.String("query", "", "recall query; empty = recent mode (decay order)")
	k := flag.Int("k", 10, "maximum results to emit")
	nowFlag := flag.String("now", "", "as-of instant, RFC3339 (default: now UTC); for replay/tests")
	indexFlag := flag.String("index", "", "persisted index path (default <root>/"+defaultIndexRelPath+"); \"-\" forces a fresh in-memory index")
	rebuild := flag.Bool("rebuild", false, "force a cold rebuild of the persisted index even if it looks fresh")
	flag.Parse()

	if err := run(runConfig{
		root:    *root,
		query:   *query,
		k:       *k,
		nowFlag: *nowFlag,
		index:   *indexFlag,
		rebuild: *rebuild,
	}, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "enso-recall: %v\n", err)
		os.Exit(1)
	}
}

type runConfig struct {
	root    string
	query   string
	k       int
	nowFlag string
	index   string // "" = default path; "-" = fresh in-memory (no persistence)
	rebuild bool
}

func run(cfg runConfig, out *os.File) error {
	if cfg.root == "" {
		return fmt.Errorf("-root is required")
	}
	now := time.Now().UTC()
	if cfg.nowFlag != "" {
		t, err := time.Parse(time.RFC3339, cfg.nowFlag)
		if err != nil {
			return fmt.Errorf("parse -now: %w", err)
		}
		now = t.UTC()
	}

	start := time.Now()
	ctx := context.Background()

	// Resolve the index path. "-" opts out of persistence entirely (the old
	// fresh-in-memory behavior, kept for tests and ad-hoc diagnostics).
	indexPath := cfg.index
	switch indexPath {
	case "":
		indexPath = filepath.Join(cfg.root, defaultIndexRelPath)
	case "-":
		indexPath = "" // graphstore treats "" as in-memory
	}

	// Vector doorfinder only when a key is present; its absence or failure is
	// reported, never fatal (ADR-002 degradation contract). On the warm path
	// the embedder is used ONLY for the single query embed — never to re-embed
	// the corpus — because no Append runs against an already-populated index.
	var emb graphstore.Embedder
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		emb = graphstore.GeminiEmbedder{APIKey: key}
	}

	g, corpusEntries, built, err := openIndex(ctx, indexPath, emb, cfg.root, cfg.rebuild)
	if err != nil {
		return err
	}
	defer g.Close()

	rr, err := g.Recall(ctx, cfg.query, now)
	if err != nil {
		return err
	}

	o := outputJSON{
		Version:       schemaVersion,
		Query:         cfg.query,
		AsOf:          now.Format(time.RFC3339),
		Mode:          string(rr.Mode),
		ElapsedMS:     time.Since(start).Milliseconds(),
		CorpusEntries: corpusEntries,
		IndexPath:     indexPath,
		IndexBuilt:    built,
		Results:       []resultJSON{}, // always an array, never null
	}
	if rr.Degraded != nil {
		o.Degraded = rr.Degraded.Error()
	}
	for i, r := range rr.Ranked {
		if i >= cfg.k {
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

// openIndex returns a ready-to-recall GraphStore, (re)building the persisted
// index only when necessary. It reports how many canonical corpus entries back
// the current index and whether this call paid the cold rebuild cost.
//
// WARM: the persisted index exists, is not forced, and no memory/*.md file is
// newer than it — Open it and attach the embedder for the query embed only. The
// corpus is loaded once (via the graph's own Load during Recall); its entry
// count is read cheaply from the opened graph.
//
// COLD: the index is absent, stale, forced (-rebuild), or in-memory ("") — load
// the Markdown corpus and rebuild the index from it, embedding at append time.
//
// A stale-but-present index that we misjudge as fresh is the only soft-failure
// mode, and it degrades to "slightly out-of-date recall," never corruption: the
// corpus stays canonical and the next rebuild heals it (INV-1). The mtime guard
// below is conservative — ANY memory file newer than the index triggers a
// rebuild.
func openIndex(ctx context.Context, indexPath string, emb graphstore.Embedder, root string, forceRebuild bool) (*graphstore.GraphStore, int, bool, error) {
	// In-memory mode ("-"): no persistence, always a cold rebuild (old behavior).
	if indexPath == "" {
		return coldRebuild(ctx, "", emb, root)
	}

	fresh, err := indexIsFresh(indexPath, root)
	if err != nil {
		return nil, 0, false, err
	}
	if forceRebuild || !fresh {
		return coldRebuild(ctx, indexPath, emb, root)
	}

	// WARM path: reuse the persisted index. Attach the embedder for the query
	// embed only — no Append runs here, so the corpus is never re-embedded.
	g, err := graphstore.Open(indexPath)
	if err != nil {
		return nil, 0, false, err
	}
	if emb != nil {
		g.SetEmbedder(emb)
	}
	entries, _, err := g.Load(ctx)
	if err != nil {
		g.Close()
		return nil, 0, false, err
	}
	return g, len(entries), false, nil
}

// coldRebuild loads the canonical Markdown corpus and rebuilds the index from
// it (embedding at append time when emb != nil). Used for the first build, a
// stale index, a forced -rebuild, or the in-memory diagnostic path.
func coldRebuild(ctx context.Context, indexPath string, emb graphstore.Embedder, root string) (*graphstore.GraphStore, int, bool, error) {
	if indexPath != "" {
		// Ensure the parent (.enso/) exists; it is derived cache, not corpus.
		if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
			return nil, 0, false, fmt.Errorf("enso-recall: create index dir: %w", err)
		}
	}
	corpus := mdstore.NewFSStore(root)
	entries, edges, err := corpus.Load(ctx)
	if err != nil {
		return nil, 0, false, err // mdstore errors are already located (file+line) and loud
	}
	g, err := graphstore.OpenRebuiltWith(ctx, indexPath, emb, entries, edges)
	if err != nil {
		return nil, 0, false, err
	}
	return g, len(entries), true, nil
}

// indexIsFresh reports whether the persisted index at indexPath is present and
// no canonical memory/*.md file is newer than it. A missing index is not fresh
// (triggers a cold build). The mtime of the index DIRECTORY (KùzuDB stores the
// database as a directory/file set) is compared against the newest mtime under
// <root>/memory. Any error reading the corpus tree is loud, not silently
// treated as fresh — a staleness check that fails open would silently serve a
// stale index.
func indexIsFresh(indexPath, root string) (bool, error) {
	info, err := os.Stat(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // no index yet -> cold build
		}
		return false, fmt.Errorf("enso-recall: stat index %q: %w", indexPath, err)
	}
	indexMod := info.ModTime()

	newest, err := newestMemoryMod(filepath.Join(root, "memory"))
	if err != nil {
		return false, err
	}
	// Index is fresh iff it is at least as new as the newest corpus file.
	// (A corpus with no memory dir/files leaves newest == zero time, so any
	// present index counts as fresh — there is nothing it could be stale
	// against.)
	return !indexMod.Before(newest), nil
}

// newestMemoryMod returns the most recent modification time among all regular
// files under dir (the corpus's memory/ tree). A missing dir yields the zero
// time with no error (an empty corpus has nothing to be stale against).
func newestMemoryMod(dir string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return filepath.SkipDir // no memory/ dir: nothing to compare
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("enso-recall: scan corpus mtimes: %w", err)
	}
	return newest, nil
}
