package mdstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

func newEntry(t *testing.T, id core.ID, nt core.NodeType, content string, enc time.Time) core.Entry {
	t.Helper()
	e, err := core.NewEntry(core.NewEntryParams{
		ID: id, Type: nt, Content: content, EncodedTime: enc, Confidence: core.ConfHigh,
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

func TestFSStore_AppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := NewFSStore(dir)
	ctx := context.Background()

	e1 := newEntry(t, "mem:2026-06-20-a", core.TypeFact, "first", time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC))
	e2 := newEntry(t, "mem:2026-06-21-b", core.TypeTask, "second", time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC))

	if err := s.Append(ctx, []core.Entry{e1, e2}, nil); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// e1 -> 2026-06-20.md, e2 -> 2026-06-21.md (bucketed by ID date).
	for _, name := range []string{"2026-06-20.md", "2026-06-21.md"} {
		if _, err := os.Stat(filepath.Join(dir, "memory", name)); err != nil {
			t.Errorf("expected daily file %s: %v", name, err)
		}
	}

	entries, edges, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 2 || len(edges) != 0 {
		t.Fatalf("want 2 entries 0 edges, got %d/%d", len(entries), len(edges))
	}
	// Sorted by filename => chronological.
	if entries[0].ID != "mem:2026-06-20-a" || entries[1].ID != "mem:2026-06-21-b" {
		t.Errorf("unexpected order: %q, %q", entries[0].ID, entries[1].ID)
	}
}

// TestFSStore_AppendOnly verifies INV-2 at the storage layer: a second Append
// to the same daily file does not rewrite or destroy the first block; both
// survive and round-trip.
func TestFSStore_AppendOnly(t *testing.T) {
	dir := t.TempDir()
	s := NewFSStore(dir)
	ctx := context.Background()
	day := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)

	e1 := newEntry(t, "mem:2026-06-20-a", core.TypeFact, "first", day)
	if err := s.Append(ctx, []core.Entry{e1}, nil); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	path := filepath.Join(dir, "memory", "2026-06-20.md")
	firstBytes, _ := os.ReadFile(path)

	e2 := newEntry(t, "mem:2026-06-20-c", core.TypeInsight, "later", day)
	if err := s.Append(ctx, []core.Entry{e2}, nil); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	secondBytes, _ := os.ReadFile(path)

	// The original content must still be a prefix-ish presence: append-only
	// means the first block's text is unchanged and still in the file.
	if !strings.Contains(string(secondBytes), "mem:2026-06-20-a") {
		t.Error("first entry disappeared after second append (not append-only)")
	}
	if !strings.Contains(string(secondBytes), "mem:2026-06-20-c") {
		t.Error("second entry missing after append")
	}
	if len(secondBytes) <= len(firstBytes) {
		t.Error("file did not grow on append")
	}

	entries, _, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries after two appends, got %d", len(entries))
	}
}

// TestFSStore_PreservesProse: appending into a file that already has prose
// keeps the prose and still parses the structured block (inline §3.5a).
func TestFSStore_PreservesProse(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(memDir, "2026-06-20.md")
	prose := "# 2026-06-20\n\nSome existing daily-note prose.\n"
	if err := os.WriteFile(path, []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewFSStore(dir)
	ctx := context.Background()
	e := newEntry(t, "mem:2026-06-20-z", core.TypeFact, "appended after prose", time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	if err := s.Append(ctx, []core.Entry{e}, nil); err != nil {
		t.Fatalf("Append: %v", err)
	}

	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "existing daily-note prose") {
		t.Error("prose was clobbered")
	}
	entries, _, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "mem:2026-06-20-z" {
		t.Errorf("structured block not parsed alongside prose: %+v", entries)
	}
}

func TestFSStore_SupersessionAdditive(t *testing.T) {
	dir := t.TempDir()
	s := NewFSStore(dir)
	ctx := context.Background()

	old := newEntry(t, "mem:2026-06-20-granola-free", core.TypeFact, "Granola plan is free tier", time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC))
	if err := s.Append(ctx, []core.Entry{old}, nil); err != nil {
		t.Fatalf("Append old: %v", err)
	}

	newer := newEntry(t, "mem:2026-06-21-granola-biz", core.TypeFact, "Granola plan is Business $14/mo", time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC))
	closed, edge := old.Supersede(newer.ID, time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC))
	// Append the new entry, the supersession edge, and the re-appended closed
	// copy of the old entry. (Edge buckets by From=newer's date => 06-21 file.)
	if err := s.Append(ctx, []core.Entry{newer, closed}, []core.Edge{edge}); err != nil {
		t.Fatalf("Append supersession: %v", err)
	}

	entries, edges, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(edges) != 1 || edges[0].Type != core.EdgeSupersedes {
		t.Fatalf("want 1 SUPERSEDES edge, got %+v", edges)
	}
	// Both the free-tier and business facts are present in history (nothing
	// destroyed); the closed copy carries valid_until.
	var sawClosed bool
	for _, e := range entries {
		if e.ID == "mem:2026-06-20-granola-free" && e.ValidUntil != nil {
			sawClosed = true
		}
	}
	if !sawClosed {
		t.Error("expected a closed (valid_until set) copy of the superseded entry in history")
	}
}

func TestFSStore_RefusesInvalid(t *testing.T) {
	dir := t.TempDir()
	s := NewFSStore(dir)
	bad := core.Entry{ID: "not-an-id", Type: core.TypeFact, Content: "x", EncodedTime: time.Now(), Confidence: core.ConfHigh, Tags: []string{}}
	if err := s.Append(context.Background(), []core.Entry{bad}, nil); err == nil {
		t.Fatal("Append should refuse an invalid entry")
	}
}

func TestFSStore_EmptyLoad(t *testing.T) {
	s := NewFSStore(t.TempDir())
	entries, edges, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load on empty: %v", err)
	}
	if entries != nil || edges != nil {
		t.Errorf("empty corpus should return nil slices, got %v/%v", entries, edges)
	}
}
