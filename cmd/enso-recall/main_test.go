package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

// seedCorpus writes a small real-shaped corpus: one supersession pair plus an
// unrelated fact.
func seedCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ctx := context.Background()
	d := func(day int) time.Time { return time.Date(2026, 7, day, 9, 0, 0, 0, time.UTC) }
	mk := func(day int, label, content string, tags []string) core.Entry {
		id, err := core.NewID(d(day), label)
		if err != nil {
			t.Fatal(err)
		}
		e, err := core.NewEntry(core.NewEntryParams{
			ID: id, Type: core.TypeFact, Content: content, EncodedTime: d(day),
			Confidence: core.ConfHigh, Tags: tags, About: []string{},
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	stale := mk(3, "granola-installed", "granola stays installed for meeting notes", []string{"granola"})
	current := mk(4, "granola-uninstalled", "granola was uninstalled; notes move to markdown", []string{"granola"})
	other := mk(5, "espresso", "the good espresso beans are the dark roast", []string{"espresso"})

	store := mdstore.NewFSStore(root)
	if err := store.Append(ctx, []core.Entry{stale, other}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Supersede(ctx, stale, current); err != nil {
		t.Fatal(err)
	}
	return root
}

// hashTree fingerprints every file under root so the read-only guarantee is
// checkable byte-for-byte.
func hashTree(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = sha256.Sum256(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func runToJSON(t *testing.T, root, query string) outputJSON {
	return runCfgToJSON(t, runConfig{root: root, query: query, k: 10, nowFlag: "2026-07-10T12:00:00Z"})
}

func runCfgToJSON(t *testing.T, cfg runConfig) outputJSON {
	t.Helper()
	if cfg.k == 0 {
		cfg.k = 10
	}
	f, err := os.CreateTemp(t.TempDir(), "out-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := run(cfg, f); err != nil {
		t.Fatalf("run: %v", err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	var o outputJSON
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, b)
	}
	return o
}

// TestRecallJSONShape pins the schema-v1 contract the shadow extension parses.
//
// Hermetic: the assertion mode == "lexical" is only meaningful when NO vector
// doorfinder is configured, so the test clears GEMINI_API_KEY for its own
// process. Without this, the test's verdict depends on the ambient environment
// (it passes in a shell with no key exported, but fails under the
// service/cron environment where GEMINI_API_KEY is present — the embedder then
// runs against the tiny un-embedded temp corpus and reports vector/degraded).
func TestRecallJSONShape(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "") // pin the lexical path regardless of ambient env
	root := seedCorpus(t)
	o := runToJSON(t, root, "what happened with granola?")

	if o.Version != schemaVersion {
		t.Fatalf("version = %d, want %d", o.Version, schemaVersion)
	}
	if o.Mode != "lexical" { // no GEMINI_API_KEY in the test environment
		t.Fatalf("mode = %q, want lexical", o.Mode)
	}
	if len(o.Results) == 0 {
		t.Fatal("no results")
	}
	if o.Results[0].ID != "mem:2026-07-04-granola-uninstalled" {
		t.Fatalf("top = %s, want the current granola entry", o.Results[0].ID)
	}
	for _, r := range o.Results {
		if r.ID == "mem:2026-07-03-granola-installed" {
			t.Fatal("superseded entry surfaced")
		}
	}
	if o.CorpusEntries != 4 { // stale + other + current + closed copy
		t.Fatalf("corpus_entries = %d, want 4", o.CorpusEntries)
	}
}

// TestRecallEmptyQueryRecentMode: recent mode still emits valid JSON with a
// non-null results array.
func TestRecallEmptyQueryRecentMode(t *testing.T) {
	root := seedCorpus(t)
	o := runToJSON(t, root, "")
	if o.Results == nil || len(o.Results) == 0 {
		t.Fatal("recent mode must emit results")
	}
}

// TestRecallIsReadOnly is the WP-7 DoD box: any invocation leaves the CORPUS
// byte-identical. Shadow mode observes; it never touches the canonical memory/
// substrate. The derived index under <root>/.enso/ is cache, not corpus, and is
// expected to appear/update — it is explicitly excluded here (and the corpus-
// untouched guarantee on the persisted path is pinned by
// TestPersistedIndexLeavesCorpusUntouched).
func TestRecallIsReadOnly(t *testing.T) {
	root := seedCorpus(t)
	corpus := filepath.Join(root, "memory")
	before := hashTree(t, corpus)

	_ = runToJSON(t, root, "what happened with granola?")
	_ = runToJSON(t, root, "") // recent mode too

	after := hashTree(t, corpus)
	if len(before) != len(after) {
		t.Fatalf("file count changed: %d -> %d", len(before), len(after))
	}
	for path, h := range before {
		if after[path] != h {
			t.Fatalf("corpus file modified by recall: %s", path)
		}
	}
}

// TestRecallMissingRootIsLoud: setup errors exit the loud path, not empty JSON.
func TestRecallMissingRootIsLoud(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := run(runConfig{root: "", query: "q", k: 10}, f); err == nil {
		t.Fatal("want error for missing -root")
	}
}

// TestPersistedIndexColdThenWarm is the WP-8 blocker (b) fix contract: the
// first call builds the on-disk index (index_built=true), and a second call
// against the unchanged corpus reuses it (index_built=false) while returning
// identical results. This is what makes a warm vector recall ~40x cheaper than
// re-embedding the corpus every call.
func TestPersistedIndexColdThenWarm(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "") // pin the lexical path; no network in unit tests
	root := seedCorpus(t)
	q := "what happened with granola?"

	cold := runToJSON(t, root, q)
	if !cold.IndexBuilt {
		t.Fatalf("first call must build the index (index_built=true), got false")
	}
	if cold.IndexPath == "" {
		t.Fatal("index_path must be reported on the persisted path")
	}
	if _, err := os.Stat(cold.IndexPath); err != nil {
		t.Fatalf("index not persisted at %s: %v", cold.IndexPath, err)
	}

	warm := runToJSON(t, root, q)
	if warm.IndexBuilt {
		t.Fatalf("second call must reuse the index (index_built=false), got true")
	}
	if len(warm.Results) != len(cold.Results) || warm.Results[0].ID != cold.Results[0].ID {
		t.Fatalf("warm recall diverged from cold: cold top %q, warm top %q",
			firstID(cold), firstID(warm))
	}
	if warm.CorpusEntries != cold.CorpusEntries {
		t.Fatalf("corpus_entries changed across warm reuse: %d -> %d", cold.CorpusEntries, warm.CorpusEntries)
	}
}

func firstID(o outputJSON) string {
	if len(o.Results) == 0 {
		return "<none>"
	}
	return o.Results[0].ID
}

// TestPersistedIndexStaleTriggersRebuild: touching a memory file newer than the
// index must force a cold rebuild on the next call. The staleness guard fails
// SAFE toward rebuilding, never toward serving a stale index.
func TestPersistedIndexStaleTriggersRebuild(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	root := seedCorpus(t)
	q := "what happened with granola?"

	_ = runToJSON(t, root, q) // cold build
	if warm := runToJSON(t, root, q); warm.IndexBuilt {
		t.Fatal("expected warm reuse before touching the corpus")
	}

	// Make a corpus file newer than the index.
	future := time.Now().Add(2 * time.Hour)
	memDir := filepath.Join(root, "memory")
	ents, err := os.ReadDir(memDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		t.Fatal("seed corpus wrote no memory files")
	}
	touched := filepath.Join(memDir, ents[0].Name())
	if err := os.Chtimes(touched, future, future); err != nil {
		t.Fatal(err)
	}

	if rebuilt := runToJSON(t, root, q); !rebuilt.IndexBuilt {
		t.Fatal("a corpus file newer than the index must trigger a cold rebuild")
	}
}

// TestPersistedIndexLeavesCorpusUntouched extends the read-only guarantee to
// the persisted path: building and reusing the index writes only under
// <root>/.enso/, never into memory/.
func TestPersistedIndexLeavesCorpusUntouched(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	root := seedCorpus(t)
	memBefore := hashTree(t, filepath.Join(root, "memory"))

	_ = runToJSON(t, root, "granola") // cold build
	_ = runToJSON(t, root, "granola") // warm reuse

	memAfter := hashTree(t, filepath.Join(root, "memory"))
	if len(memBefore) != len(memAfter) {
		t.Fatalf("memory/ file count changed: %d -> %d", len(memBefore), len(memAfter))
	}
	for path, h := range memBefore {
		if memAfter[path] != h {
			t.Fatalf("persisted index path modified a corpus file: %s", path)
		}
	}
}

// TestForceRebuildFlag: -rebuild rebuilds even when the index is fresh.
func TestForceRebuildFlag(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	root := seedCorpus(t)
	_ = runToJSON(t, root, "granola") // cold build
	forced := runCfgToJSON(t, runConfig{root: root, query: "granola", k: 10, nowFlag: "2026-07-10T12:00:00Z", rebuild: true})
	if !forced.IndexBuilt {
		t.Fatal("-rebuild must force a cold rebuild even on a fresh index")
	}
}

// TestInMemoryIndexOptOut: -index "-" keeps the old fresh-in-memory behavior
// (no persisted path, always rebuilt, no index file written).
func TestInMemoryIndexOptOut(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	root := seedCorpus(t)
	o := runCfgToJSON(t, runConfig{root: root, query: "granola", k: 10, nowFlag: "2026-07-10T12:00:00Z", index: "-"})
	if o.IndexPath != "" {
		t.Fatalf("in-memory opt-out must report empty index_path, got %q", o.IndexPath)
	}
	if !o.IndexBuilt {
		t.Fatal("in-memory mode always rebuilds (index_built=true)")
	}
	if _, err := os.Stat(filepath.Join(root, defaultIndexRelPath)); !os.IsNotExist(err) {
		t.Fatalf("in-memory mode must not write the default index file (stat err=%v)", err)
	}
}
