// Package storetest is the shared core.Store conformance suite (WP-3 DoD:
// "graphstore passes the same Store-contract test suite as mdstore/memstore").
// Every Store implementation runs RunConformance from its own package's tests,
// so the port's semantics are pinned in ONE place and a new adapter cannot
// silently reinterpret them.
//
// The suite asserts observable port behavior only — nothing adapter-specific:
//
//   - empty corpus loads as (nil, nil, nil)
//   - append→load round-trips every field losslessly, including explicit-null
//     pointers, unknown Extra keys, and entity-ref / dangling edge targets
//   - append is append-only: earlier records survive later batches unchanged
//   - the supersession ceremony's re-appended closed copy coexists with the
//     original record (same mem: id, two records, order preserved)
//   - invalid input is rejected loudly and leaves the store unchanged
//
// Comparison notes: record equality is via a canonical string key (times
// UTC-normalized RFC3339Nano; tags/about/extra JSON with sorted keys), and
// Load order is asserted as (a) multiset equality and (b) per-id record order
// — NOT global order, because mdstore interleaves by daily-file bucket, which
// is legitimate adapter freedom the port does not constrain.
package storetest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// Factory returns a fresh, empty Store for one (sub)test. Register cleanup on t.
type Factory func(t *testing.T) core.Store

// RunConformance runs the full suite against stores produced by open.
func RunConformance(t *testing.T, open Factory) {
	t.Run("EmptyLoad", func(t *testing.T) { testEmptyLoad(t, open) })
	t.Run("RoundTrip", func(t *testing.T) { testRoundTrip(t, open) })
	t.Run("AppendOnly", func(t *testing.T) { testAppendOnly(t, open) })
	t.Run("SupersessionCeremony", func(t *testing.T) { testSupersessionCeremony(t, open) })
	t.Run("InvalidRejected", func(t *testing.T) { testInvalidRejected(t, open) })
}

func testEmptyLoad(t *testing.T, open Factory) {
	s := open(t)
	entries, edges, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load on empty store: %v", err)
	}
	if entries != nil || edges != nil {
		t.Fatalf("empty store must load as (nil, nil), got %d entries, %d edges", len(entries), len(edges))
	}
}

func testRoundTrip(t *testing.T, open Factory) {
	s := open(t)
	ctx := context.Background()

	day := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	eventT := day.Add(-48 * time.Hour)
	validFrom := day.Add(-24 * time.Hour)

	// One entry per node type, exercising nullable pointers both ways.
	var batch []core.Entry
	for i, nt := range []core.NodeType{
		core.TypeFact, core.TypeDecision, core.TypeInsight,
		core.TypePerson, core.TypeProject, core.TypeTask,
	} {
		p := core.NewEntryParams{
			ID:          mustID(t, day, fmt.Sprintf("rt-%d", i)),
			Type:        nt,
			Content:     fmt.Sprintf("round-trip fixture %d (%s) with path ~/workspace/clockworksoul/enso", i, nt),
			EncodedTime: day.Add(time.Duration(i) * time.Minute),
			Confidence:  core.ConfHigh,
			Tags:        []string{"conformance", fmt.Sprintf("t%d", i)},
			About:       []string{"project:enso"},
		}
		if i%2 == 0 {
			p.EventTime = &eventT
			p.ValidFrom = &validFrom
		}
		e, err := core.NewEntry(p)
		if err != nil {
			t.Fatalf("build fixture: %v", err)
		}
		if i == 3 {
			// Unknown-key preservation is part of the port contract (§3.4).
			e.Extra["x_future_key"] = "kept-verbatim"
		}
		batch = append(batch, e)
	}

	edges := []core.Edge{
		{From: batch[0].ID, Type: core.EdgeRelatesTo, To: string(batch[1].ID), Extra: map[string]string{}},
		{From: batch[1].ID, Type: core.EdgeAbout, To: "project:enso", Extra: map[string]string{"x_note": "entity-ref target"}},
		// Dangling target: the grammar does not force edge targets to exist.
		{From: batch[2].ID, Type: core.EdgeOwns, To: "mem:2026-06-01-never-written", Extra: map[string]string{}},
	}

	if err := s.Append(ctx, batch, edges); err != nil {
		t.Fatalf("Append: %v", err)
	}
	gotE, gotEd, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertEntryMultiset(t, batch, gotE)
	assertEdgeMultiset(t, edges, gotEd)
}

func testAppendOnly(t *testing.T, open Factory) {
	s := open(t)
	ctx := context.Background()
	d1 := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)

	first := []core.Entry{newFixture(t, d1, "batch-one", core.TypeFact, "first batch record")}
	if err := s.Append(ctx, first, nil); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	second := []core.Entry{newFixture(t, d2, "batch-two", core.TypeTask, "second batch record")}
	if err := s.Append(ctx, second, nil); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	gotE, _, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertEntryMultiset(t, append(append([]core.Entry{}, first...), second...), gotE)
}

func testSupersessionCeremony(t *testing.T, open Factory) {
	s := open(t)
	ctx := context.Background()
	d1 := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)

	old := newFixture(t, d1, "belief-v1", core.TypeFact, "granola bar stays installed")
	if err := s.Append(ctx, []core.Entry{old}, nil); err != nil {
		t.Fatalf("Append original: %v", err)
	}

	newer := newFixture(t, d2, "belief-v2", core.TypeFact, "granola bar was uninstalled")
	closed, edge := old.Supersede(newer.ID, d2)
	if err := s.Append(ctx, []core.Entry{newer, closed}, []core.Edge{edge}); err != nil {
		t.Fatalf("Append ceremony: %v", err)
	}

	gotE, gotEd, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// All three records exist: history is never rewritten (INV-2). The old id
	// has TWO records — the open original and the closed copy — in that order.
	assertEntryMultiset(t, []core.Entry{old, newer, closed}, gotE)
	assertEdgeMultiset(t, []core.Edge{edge}, gotEd)

	var oldRecords []core.Entry
	for _, e := range gotE {
		if e.ID == old.ID {
			oldRecords = append(oldRecords, e)
		}
	}
	if len(oldRecords) != 2 {
		t.Fatalf("superseded id must keep both records, got %d", len(oldRecords))
	}
	if oldRecords[0].ValidUntil != nil || oldRecords[1].ValidUntil == nil {
		t.Fatalf("same-id record order lost: want open copy then closed copy, got ValidUntil %v then %v",
			oldRecords[0].ValidUntil, oldRecords[1].ValidUntil)
	}
}

func testInvalidRejected(t *testing.T, open Factory) {
	s := open(t)
	ctx := context.Background()
	day := time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC)

	bad := newFixture(t, day, "almost-valid", core.TypeFact, "content")
	bad.Type = core.NodeType("Rumor") // not in the closed type set
	if err := s.Append(ctx, []core.Entry{bad}, nil); err == nil {
		t.Fatalf("Append accepted an invalid entry type; want loud rejection")
	}
	if err := s.Append(ctx, nil, []core.Edge{{From: "not-an-id", Type: core.EdgeAbout, To: "x"}}); err == nil {
		t.Fatalf("Append accepted an invalid edge; want loud rejection")
	}

	entries, edges, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load after rejected appends: %v", err)
	}
	if len(entries) != 0 || len(edges) != 0 {
		t.Fatalf("rejected append mutated the store: %d entries, %d edges", len(entries), len(edges))
	}
}

// --- fixtures and canonical comparison ---

func mustID(t *testing.T, day time.Time, label string) core.ID {
	t.Helper()
	id, err := core.NewID(day, label)
	if err != nil {
		t.Fatalf("NewID(%q): %v", label, err)
	}
	return id
}

func newFixture(t *testing.T, day time.Time, label string, nt core.NodeType, content string) core.Entry {
	t.Helper()
	e, err := core.NewEntry(core.NewEntryParams{
		ID:          mustID(t, day, label),
		Type:        nt,
		Content:     content,
		EncodedTime: day,
		Confidence:  core.ConfMedium,
		Tags:        []string{"conformance"},
		About:       []string{},
	})
	if err != nil {
		t.Fatalf("NewEntry(%q): %v", label, err)
	}
	return e
}

// entryKey canonicalizes an Entry for comparison. Times are UTC-normalized;
// len-0 maps/slices compare equal to nil (adapters may represent "empty"
// either way without changing meaning).
func entryKey(e core.Entry) string {
	return fmt.Sprintf("id=%s|type=%s|content=%q|enc=%s|event=%s|vfrom=%s|vuntil=%s|conf=%s|tags=%s|about=%s|lref=%s|slast=%g|sfloor=%g|lambda=%g|scap=%g|extra=%s",
		e.ID, e.Type, e.Content,
		fmtT(e.EncodedTime), fmtTP(e.EventTime), fmtTP(e.ValidFrom), fmtTP(e.ValidUntil),
		e.Confidence, jsonStrs(e.Tags), jsonStrs(e.About),
		fmtT(e.Temporal.LastRefTime),
		e.Temporal.SLast, e.Temporal.SFloor, e.Temporal.Lambda, e.Temporal.SCap,
		jsonMap(e.Extra))
}

func edgeKey(ed core.Edge) string {
	return fmt.Sprintf("from=%s|type=%s|to=%s|extra=%s", ed.From, ed.Type, ed.To, jsonMap(ed.Extra))
}

func fmtT(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func fmtTP(t *time.Time) string {
	if t == nil {
		return "null"
	}
	return fmtT(*t)
}

func jsonStrs(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func jsonMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(m) // encoding/json sorts map keys
	return string(b)
}

func assertEntryMultiset(t *testing.T, want, got []core.Entry) {
	t.Helper()
	assertMultiset(t, "entry", keys(want, entryKey), keys(got, entryKey))
	// Per-id record order must be preserved (it is how readers resolve the
	// latest state of a re-appended id). Global order is adapter freedom.
	wantByID := map[core.ID][]string{}
	for _, e := range want {
		wantByID[e.ID] = append(wantByID[e.ID], entryKey(e))
	}
	gotByID := map[core.ID][]string{}
	for _, e := range got {
		gotByID[e.ID] = append(gotByID[e.ID], entryKey(e))
	}
	for id, w := range wantByID {
		g := gotByID[id]
		if len(w) != len(g) {
			continue // multiset assertion above already reported this
		}
		for i := range w {
			if w[i] != g[i] {
				t.Errorf("record order for id %s diverges at position %d:\n want %s\n  got %s", id, i, w[i], g[i])
			}
		}
	}
}

func assertEdgeMultiset(t *testing.T, want, got []core.Edge) {
	t.Helper()
	assertMultiset(t, "edge", keys(want, edgeKey), keys(got, edgeKey))
}

func keys[T any](in []T, key func(T) string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = key(v)
	}
	return out
}

func assertMultiset(t *testing.T, kind string, want, got []string) {
	t.Helper()
	w := append([]string{}, want...)
	g := append([]string{}, got...)
	sort.Strings(w)
	sort.Strings(g)
	if len(w) != len(g) {
		t.Errorf("%s count mismatch: want %d, got %d", kind, len(w), len(g))
	}
	for i := 0; i < len(w) && i < len(g); i++ {
		if w[i] != g[i] {
			t.Errorf("%s multiset diverges:\n want %s\n  got %s", kind, w[i], g[i])
			return
		}
	}
}
