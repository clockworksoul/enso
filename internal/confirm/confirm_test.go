package confirm

import (
	"context"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/memstore"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

var fixedNow = time.Date(2026, 6, 26, 2, 0, 0, 0, time.UTC)

// mkEntry builds a valid, current Entry encoded `ago` before fixedNow.
func mkEntry(t *testing.T, label, content string, ago time.Duration) core.Entry {
	t.Helper()
	enc := fixedNow.Add(-ago)
	id, err := core.NewID(enc, label)
	if err != nil {
		t.Fatalf("NewID(%q): %v", label, err)
	}
	e, err := core.NewEntry(core.NewEntryParams{
		ID:          id,
		Type:        core.TypeFact,
		Content:     content,
		EncodedTime: enc,
		Confidence:  core.ConfHigh,
		Tags:        []string{"test"},
		About:       []string{"project:enso"},
	})
	if err != nil {
		t.Fatalf("NewEntry(%q): %v", label, err)
	}
	return e
}

func seedStore(t *testing.T, entries ...core.Entry) *memstore.MemStore {
	t.Helper()
	s := memstore.New()
	if err := s.Append(context.Background(), entries, nil); err != nil {
		t.Fatalf("seed append: %v", err)
	}
	return s
}

func fixedClock() func() time.Time { return func() time.Time { return fixedNow } }

// ---------------------------------------------------------------------------
// Target resolution
// ---------------------------------------------------------------------------

func TestStoreResolver_FiltersClosedEntries(t *testing.T) {
	cur := mkEntry(t, "current", "current fact", time.Hour)
	closed := mkEntry(t, "closed", "closed fact", 2*time.Hour)
	until := fixedNow.Add(-time.Minute)
	closed.ValidUntil = &until

	store := seedStore(t, cur, closed)
	r := StoreResolver{Store: store}
	cands, err := r.Resolve(context.Background(), core.Detection{}, fixedNow)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(cands) != 1 || cands[0].ID != cur.ID {
		t.Fatalf("expected only the current entry, got %d candidates: %v", len(cands), cands)
	}
}

func TestStoreResolver_CapsCandidates(t *testing.T) {
	var entries []core.Entry
	for i := 0; i < 8; i++ {
		entries = append(entries, mkEntry(t, "e"+string(rune('a'+i)), "fact", time.Duration(i+1)*time.Hour))
	}
	store := seedStore(t, entries...)
	r := StoreResolver{Store: store, MaxCandidates: 3}
	cands, err := r.Resolve(context.Background(), core.Detection{}, fixedNow)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("expected cap of 3 candidates, got %d", len(cands))
	}
}

func TestFixedResolver_FiltersClosedEntries(t *testing.T) {
	cur := mkEntry(t, "current", "current fact", time.Hour)
	closed := mkEntry(t, "closed", "closed fact", 2*time.Hour)
	until := fixedNow.Add(-time.Minute)
	closed.ValidUntil = &until

	r := FixedResolver{Candidates: []core.Entry{cur, closed}}
	cands, err := r.Resolve(context.Background(), core.Detection{}, fixedNow)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(cands) != 1 || cands[0].ID != cur.ID {
		t.Fatalf("expected only the current entry, got %d: %v", len(cands), cands)
	}
}

// ---------------------------------------------------------------------------
// Proposal value
// ---------------------------------------------------------------------------

func TestProposal_Unambiguous(t *testing.T) {
	one := Proposal{Candidates: []core.Entry{mkEntry(t, "a", "x", time.Hour)}}
	if !one.Unambiguous() {
		t.Error("single-candidate proposal should be unambiguous")
	}
	two := Proposal{Candidates: []core.Entry{
		mkEntry(t, "a", "x", time.Hour), mkEntry(t, "b", "y", time.Hour),
	}}
	if two.Unambiguous() {
		t.Error("two-candidate proposal should be ambiguous")
	}
	none := Proposal{}
	if none.Unambiguous() {
		t.Error("zero-candidate proposal should not be unambiguous")
	}
}

func TestProposal_Summary(t *testing.T) {
	single := Proposal{
		Detection:  core.Detection{IsCorrection: true, Kind: core.CorrectRestate, Confidence: core.DetectStrong, Signals: []string{"restate:actually-now"}, Content: "it is now approved"},
		Candidates: []core.Entry{mkEntry(t, "x", "old content here", time.Hour)},
		AsOf:       fixedNow,
	}
	s := single.Summary()
	if s == "" {
		t.Fatal("summary empty")
	}
	for _, want := range []string{"restate", "strong", "approved", "supersedes"} {
		if !contains(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}

	multi := Proposal{
		Detection:  core.Detection{IsCorrection: true, Kind: core.CorrectReframe, Confidence: core.DetectWeak},
		Candidates: []core.Entry{mkEntry(t, "a", "cand A", time.Hour), mkEntry(t, "b", "cand B", 2*time.Hour)},
		AsOf:       fixedNow,
	}
	ms := multi.Summary()
	if !contains(ms, "candidates") {
		t.Errorf("multi-candidate summary should mention candidates:\n%s", ms)
	}

	noTarget := Proposal{
		Detection: core.Detection{IsCorrection: true, Kind: core.CorrectRestate, Confidence: core.DetectStrong, Content: "x"},
		AsOf:      fixedNow,
	}
	if !contains(noTarget.Summary(), "no target") {
		t.Errorf("zero-candidate summary should note no target:\n%s", noTarget.Summary())
	}
}

// ---------------------------------------------------------------------------
// Propose convenience
// ---------------------------------------------------------------------------

func TestPropose_NotACorrection(t *testing.T) {
	store := seedStore(t, mkEntry(t, "e", "some fact", time.Hour))
	_, ok, err := Propose(context.Background(), StoreResolver{Store: store}, "what time is the meeting?", fixedClock()())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Error("a plain question should not be detected as a correction")
	}
}

func TestPropose_CorrectionWithTarget(t *testing.T) {
	target := mkEntry(t, "tip", "Team member 'Tip' on Axon", time.Hour)
	store := seedStore(t, target)

	prop, ok, err := Propose(context.Background(), StoreResolver{Store: store},
		"actually, it's Tipa, not Tip", fixedClock()())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("expected a correction to be detected")
	}
	if !prop.Detection.IsCorrection {
		t.Error("proposal detection should be a correction")
	}
	if len(prop.Candidates) == 0 {
		t.Error("expected at least one resolved candidate")
	}
	if !prop.AsOf.Equal(fixedNow) {
		t.Errorf("AsOf = %v, want %v", prop.AsOf, fixedNow)
	}
}

func TestPropose_CorrectionNoTarget(t *testing.T) {
	// Empty store: detection should still succeed (ok=true) but with zero
	// candidates — exactly the distinction a validation harness measures.
	store := seedStore(t)
	prop, ok, err := Propose(context.Background(), StoreResolver{Store: store},
		"actually, it's Tipa, not Tip", fixedClock()())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("detection should succeed even with no target")
	}
	if len(prop.Candidates) != 0 {
		t.Errorf("expected zero candidates from empty store, got %d", len(prop.Candidates))
	}
}

// ---------------------------------------------------------------------------
// small string helpers (no deps)
// ---------------------------------------------------------------------------

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
