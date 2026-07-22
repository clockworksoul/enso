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
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "out-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := run(root, query, 10, "2026-07-10T12:00:00Z", f); err != nil {
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

// TestRecallIsReadOnly is the WP-7 DoD box: any invocation leaves the corpus
// byte-identical. Shadow mode observes; it never touches.
func TestRecallIsReadOnly(t *testing.T) {
	root := seedCorpus(t)
	before := hashTree(t, root)

	_ = runToJSON(t, root, "what happened with granola?")
	_ = runToJSON(t, root, "") // recent mode too

	after := hashTree(t, root)
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
	if err := run("", "q", 10, "", f); err == nil {
		t.Fatal("want error for missing -root")
	}
}
