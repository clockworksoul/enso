package mdstore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

func mustEntry(t *testing.T, p core.NewEntryParams) core.Entry {
	t.Helper()
	e, err := core.NewEntry(p)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

func sampleEntry(t *testing.T) core.Entry {
	t.Helper()
	enc := time.Date(2026, 6, 20, 18, 30, 0, 0, time.UTC)
	evt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	return mustEntry(t, core.NewEntryParams{
		ID:          "mem:2026-06-20-omega-lite",
		Type:        core.TypeProject,
		Content:     "Omega Lite MVP shipped its 5th adapter",
		EncodedTime: enc,
		EventTime:   &evt,
		Confidence:  core.ConfHigh,
		Tags:        []string{"omega", "milestone"},
		About:       []string{"project:omega", "person:matt"},
	})
}

// TestRoundTrip is the INV-1 law: parse(serialize(x)) == x (tech spec §3.4).
func TestRoundTrip_Entry(t *testing.T) {
	in := sampleEntry(t)
	doc := MarshalEntry(in)

	entries, edges, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(edges))
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	assertEntryEqual(t, in, entries[0])
}

func TestRoundTrip_Edge(t *testing.T) {
	in := core.Edge{
		From:  "mem:2026-06-21-omega-update",
		Type:  core.EdgeSupersedes,
		To:    "mem:2026-06-20-omega-lite",
		Extra: map[string]string{},
	}
	doc := MarshalEdge(in)
	_, edges, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if !reflect.DeepEqual(in, edges[0]) {
		t.Errorf("edge round-trip mismatch:\n in=%+v\nout=%+v", in, edges[0])
	}
}

// TestRoundTrip_NullsAndExtra exercises explicit nulls and unknown-key
// preservation in one pass.
func TestRoundTrip_NullsAndExtra(t *testing.T) {
	in := sampleEntry(t)
	in.EventTime = nil // explicit null
	in.Extra["provenance"] = "backfill-2026-06-20"
	in.Extra["source_file"] = "memory/2026-06-01.md"

	doc := MarshalEntry(in)
	entries, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	assertEntryEqual(t, in, entries[0])
	if entries[0].Extra["provenance"] != "backfill-2026-06-20" {
		t.Errorf("Extra provenance lost: %v", entries[0].Extra)
	}
}

func TestRoundTrip_Corpus(t *testing.T) {
	e1 := sampleEntry(t)
	e2 := mustEntry(t, core.NewEntryParams{
		ID:          "mem:2026-06-20-note",
		Type:        core.TypeFact,
		Content:     "a second fact",
		EncodedTime: time.Date(2026, 6, 20, 19, 0, 0, 0, time.UTC),
		Confidence:  core.ConfMedium,
	})
	ed := core.Edge{From: e2.ID, Type: core.EdgeRelatesTo, To: string(e1.ID), Extra: map[string]string{}}

	doc := Marshal([]core.Entry{e1, e2}, []core.Edge{ed})
	gotE, gotEd, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(gotE) != 2 || len(gotEd) != 1 {
		t.Fatalf("got %d entries, %d edges; want 2,1", len(gotE), len(gotEd))
	}
	assertEntryEqual(t, e1, gotE[0])
	assertEntryEqual(t, e2, gotE[1])
}

// assertEntryEqual compares two entries field-by-field with time normalization
// (the serializer normalizes to UTC, so compare instants, not wall-clock zones).
func assertEntryEqual(t *testing.T, want, got core.Entry) {
	t.Helper()
	if want.ID != got.ID {
		t.Errorf("ID: want %q got %q", want.ID, got.ID)
	}
	if want.Type != got.Type {
		t.Errorf("Type: want %q got %q", want.Type, got.Type)
	}
	if want.Content != got.Content {
		t.Errorf("Content: want %q got %q", want.Content, got.Content)
	}
	if !want.EncodedTime.Equal(got.EncodedTime) {
		t.Errorf("EncodedTime: want %v got %v", want.EncodedTime, got.EncodedTime)
	}
	assertTimePtrEqual(t, "EventTime", want.EventTime, got.EventTime)
	assertTimePtrEqual(t, "ValidFrom", want.ValidFrom, got.ValidFrom)
	assertTimePtrEqual(t, "ValidUntil", want.ValidUntil, got.ValidUntil)
	if want.Confidence != got.Confidence {
		t.Errorf("Confidence: want %q got %q", want.Confidence, got.Confidence)
	}
	if !reflect.DeepEqual(want.Tags, got.Tags) {
		t.Errorf("Tags: want %v got %v", want.Tags, got.Tags)
	}
	if !reflect.DeepEqual(want.About, got.About) {
		t.Errorf("About: want %v got %v", want.About, got.About)
	}
	if !want.Temporal.LastRefTime.Equal(got.Temporal.LastRefTime) {
		t.Errorf("LastRefTime: want %v got %v", want.Temporal.LastRefTime, got.Temporal.LastRefTime)
	}
	if want.Temporal.SLast != got.Temporal.SLast ||
		want.Temporal.SFloor != got.Temporal.SFloor ||
		want.Temporal.Lambda != got.Temporal.Lambda ||
		want.Temporal.SCap != got.Temporal.SCap {
		t.Errorf("Temporal mismatch: want %+v got %+v", want.Temporal, got.Temporal)
	}
	if !reflect.DeepEqual(want.Extra, got.Extra) {
		t.Errorf("Extra: want %v got %v", want.Extra, got.Extra)
	}
}

func assertTimePtrEqual(t *testing.T, name string, want, got *time.Time) {
	t.Helper()
	switch {
	case want == nil && got == nil:
		return
	case want == nil || got == nil:
		t.Errorf("%s: nil mismatch want=%v got=%v", name, want, got)
	case !want.Equal(*got):
		t.Errorf("%s: want %v got %v", name, *want, *got)
	}
}

// TestGolden pins the exact on-disk format (AMEND-1: the format is a public
// contract). If this breaks, the format changed — update the golden file
// deliberately, not reflexively.
func TestGolden(t *testing.T) {
	e := sampleEntry(t)
	e.Extra["provenance"] = "live"
	ed := core.Edge{From: "mem:2026-06-21-x", Type: core.EdgeSupersedes, To: string(e.ID), Extra: map[string]string{}}
	got := Marshal([]core.Entry{e}, []core.Edge{ed})

	goldenPath := filepath.Join("testdata", "golden_entry.md")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden: %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if string(want) != got {
		t.Errorf("golden mismatch.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
