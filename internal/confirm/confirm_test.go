package confirm

import (
	"context"
	"errors"
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

// seedStore returns a memstore preloaded with the given entries.
func seedStore(t *testing.T, entries ...core.Entry) *memstore.MemStore {
	t.Helper()
	s := memstore.New()
	if err := s.Append(context.Background(), entries, nil); err != nil {
		t.Fatalf("seed append: %v", err)
	}
	return s
}

// scripted is an Operator that returns a fixed decision and records the
// proposal it was shown.
type scripted struct {
	decision Decision
	err      error
	seen     *Proposal
	calls    int
}

func (s *scripted) Decide(_ context.Context, p Proposal) (Decision, error) {
	s.calls++
	cp := p
	s.seen = &cp
	return s.decision, s.err
}

func fixedClock() func() time.Time { return func() time.Time { return fixedNow } }

// ---------------------------------------------------------------------------
// Happy path: detect → confirm → commit
// ---------------------------------------------------------------------------

func TestHandleText_HappyPath_CommitsCorrection(t *testing.T) {
	stale := mkEntry(t, "headcount-ask", "headcount ask is still overdue", 30*24*time.Hour)
	store := seedStore(t, stale)

	op := &scripted{decision: Decision{Confirm: true, NewLabel: "headcount-ask-landed"}}
	c := &Confirmer{
		Store:    store,
		Resolver: StoreResolver{Store: store},
		Operator: op,
		Policy:   DefaultPolicy(),
		Now:      fixedClock(),
	}

	res, err := c.HandleText(context.Background(),
		"actually, the headcount ask is now approved as of the Jun 18 1:1")
	if err != nil {
		t.Fatalf("HandleText: unexpected error: %v", err)
	}
	if !res.Committed {
		t.Fatalf("expected commit, got Committed=false")
	}
	if op.calls != 1 {
		t.Errorf("operator should be consulted exactly once, got %d", op.calls)
	}
	if res.Detection.Kind != core.CorrectRestate {
		t.Errorf("expected restate kind, got %s", res.Detection.Kind)
	}

	// Append-only (INV-2): the store now holds the ORIGINAL stale entry, the
	// re-appended CLOSED copy (ValidUntil set), and the NEW head = 3 entries,
	// plus 1 SUPERSEDES edge. Nothing is rewritten in place.
	entries, edges, _ := store.Load(context.Background())
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (original + closed re-append + new), got %d", len(entries))
	}
	if len(edges) != 1 || edges[0].Type != core.EdgeSupersedes {
		t.Fatalf("expected 1 SUPERSEDES edge, got %v", edges)
	}

	// Resolving "current" for the stale ID must yield a CLOSED entry: at least
	// one record with that ID carries ValidUntil and is not current at AsOf.
	var sawClosed bool
	for _, e := range entries {
		if e.ID == stale.ID && !e.IsCurrent(fixedNow) {
			sawClosed = true
		}
	}
	if !sawClosed {
		t.Errorf("no closed record for superseded id %s found after commit", stale.ID)
	}

	// The new head must be current and carry the corrected content.
	if !res.NewHead.IsCurrent(fixedNow) {
		t.Errorf("new head should be current")
	}
	if res.NewHead.Content == "" {
		t.Errorf("new head content should not be empty")
	}
	// Provenance: the new head should record the supersession.
	if got := res.NewHead.Extra[core.ExtraSupersededID]; got != string(stale.ID) {
		t.Errorf("new head supersedes provenance = %q, want %q", got, stale.ID)
	}
}

// ---------------------------------------------------------------------------
// Policy: non-corrections and below-threshold lines write nothing
// ---------------------------------------------------------------------------

func TestHandleText_NotACorrection(t *testing.T) {
	store := seedStore(t, mkEntry(t, "x", "some fact", time.Hour))
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "n"}}
	c := &Confirmer{Store: store, Resolver: StoreResolver{Store: store}, Operator: op, Policy: DefaultPolicy(), Now: fixedClock()}

	res, err := c.HandleText(context.Background(), "the weather is nice today")
	if !errors.Is(err, ErrNotACorrection) {
		t.Fatalf("expected ErrNotACorrection, got %v", err)
	}
	if res.Committed || op.calls != 0 {
		t.Errorf("non-correction must not commit or consult operator (committed=%v calls=%d)", res.Committed, op.calls)
	}
	if res.Detection.IsCorrection {
		t.Errorf("detection should report not-a-correction")
	}
}

func TestHandleText_BelowThreshold_NotSurfaced(t *testing.T) {
	store := seedStore(t, mkEntry(t, "x", "some fact", time.Hour))
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "n"}}
	// Policy requires STRONG; a weak "ball is in Ed's court" reframe must be dropped.
	c := &Confirmer{
		Store:    store,
		Resolver: StoreResolver{Store: store},
		Operator: op,
		Policy:   Policy{MinConfidence: core.DetectStrong},
		Now:      fixedClock(),
	}

	res, err := c.HandleText(context.Background(), "actually the ball is in Ed's court now")
	if !errors.Is(err, ErrBelowThreshold) {
		t.Fatalf("expected ErrBelowThreshold, got %v", err)
	}
	if res.Committed || op.calls != 0 {
		t.Errorf("below-threshold must not commit or consult operator")
	}
	// Detection is still preserved for audit even though not surfaced.
	if !res.Detection.IsCorrection {
		t.Errorf("below-threshold detection should still be carried for audit")
	}
}

// ---------------------------------------------------------------------------
// Target resolution
// ---------------------------------------------------------------------------

func TestHandleText_NoTarget(t *testing.T) {
	// Empty store → nothing to supersede.
	store := memstore.New()
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "n"}}
	c := &Confirmer{Store: store, Resolver: StoreResolver{Store: store}, Operator: op, Policy: DefaultPolicy(), Now: fixedClock()}

	_, err := c.HandleText(context.Background(), "actually that's stale, it's now resolved")
	if !errors.Is(err, ErrNoTarget) {
		t.Fatalf("expected ErrNoTarget, got %v", err)
	}
	if op.calls != 0 {
		t.Errorf("no-target must not consult operator")
	}
}

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

// ---------------------------------------------------------------------------
// Operator rejection and abort
// ---------------------------------------------------------------------------

func TestHandleText_OperatorRejects(t *testing.T) {
	stale := mkEntry(t, "x", "stale fact", time.Hour)
	store := seedStore(t, stale)
	op := &scripted{decision: Decision{Confirm: false}}
	c := &Confirmer{Store: store, Resolver: StoreResolver{Store: store}, Operator: op, Policy: DefaultPolicy(), Now: fixedClock()}

	res, err := c.HandleText(context.Background(), "actually it's now obsolete")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if res.Committed {
		t.Errorf("rejected proposal must not commit")
	}
	// Store unchanged.
	entries, edges, _ := store.Load(context.Background())
	if len(entries) != 1 || len(edges) != 0 {
		t.Errorf("store mutated on rejection: %d entries %d edges", len(entries), len(edges))
	}
	// The proposal should still be reported for audit.
	if res.Proposal == nil {
		t.Errorf("expected proposal to be carried even on rejection")
	}
}

func TestHandleText_OperatorError_Aborts(t *testing.T) {
	stale := mkEntry(t, "x", "stale fact", time.Hour)
	store := seedStore(t, stale)
	boom := errors.New("operator I/O failure")
	op := &scripted{err: boom}
	c := &Confirmer{Store: store, Resolver: StoreResolver{Store: store}, Operator: op, Policy: DefaultPolicy(), Now: fixedClock()}

	_, err := c.HandleText(context.Background(), "actually it's now obsolete")
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected operator error to propagate, got %v", err)
	}
	entries, _, _ := store.Load(context.Background())
	if len(entries) != 1 {
		t.Errorf("store mutated despite operator error")
	}
}

// ---------------------------------------------------------------------------
// Commit-time validation
// ---------------------------------------------------------------------------

func TestHandleText_MissingLabel_FailsLoud(t *testing.T) {
	stale := mkEntry(t, "x", "stale fact", time.Hour)
	store := seedStore(t, stale)
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "   "}} // blank
	c := &Confirmer{Store: store, Resolver: StoreResolver{Store: store}, Operator: op, Policy: DefaultPolicy(), Now: fixedClock()}

	_, err := c.HandleText(context.Background(), "actually it's now resolved")
	if err == nil {
		t.Fatalf("expected error on missing label")
	}
	entries, _, _ := store.Load(context.Background())
	if len(entries) != 1 {
		t.Errorf("store mutated despite missing label")
	}
}

func TestHandleText_NoContentAnywhere_FailsLoud(t *testing.T) {
	stale := mkEntry(t, "x", "stale fact", time.Hour)
	store := seedStore(t, stale)
	// A bare whose-court reframe extracts no content; operator supplies none either.
	// Force surfacing of the weak detection (default policy already surfaces weak),
	// but give an empty operator content so the no-content guard fires.
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "frame", Content: "   "}}
	c := &Confirmer{Store: store, Resolver: StoreResolver{Store: store}, Operator: op, Policy: DefaultPolicy(), Now: fixedClock()}

	res, err := c.HandleText(context.Background(), "ball is in Ed's court")
	// Detection must be the reframe with no extracted content.
	if res.Detection.Content != "" {
		t.Fatalf("precondition: expected no extracted content, got %q", res.Detection.Content)
	}
	if err == nil {
		t.Fatalf("expected no-content guard to fire")
	}
	entries, _, _ := store.Load(context.Background())
	if len(entries) != 1 {
		t.Errorf("store mutated despite no content")
	}
}

// ---------------------------------------------------------------------------
// Ambiguous targets: operator picks via TargetIndex
// ---------------------------------------------------------------------------

func TestHandleText_AmbiguousTarget_OperatorPicks(t *testing.T) {
	a := mkEntry(t, "a", "fact A", 1*time.Hour)
	b := mkEntry(t, "b", "fact B", 2*time.Hour)
	store := seedStore(t, a, b)
	// FixedResolver returns both, so the proposal is ambiguous; operator picks #1 (b).
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "fact-b-fixed", TargetIndex: 1}}
	c := &Confirmer{
		Store:    store,
		Resolver: FixedResolver{Candidates: []core.Entry{a, b}},
		Operator: op,
		Policy:   DefaultPolicy(),
		Now:      fixedClock(),
	}

	res, err := c.HandleText(context.Background(), "actually it's now corrected")
	if err != nil {
		t.Fatalf("HandleText: %v", err)
	}
	if res.Proposal == nil || res.Proposal.Unambiguous() {
		t.Fatalf("expected an ambiguous proposal with >1 candidate")
	}
	if res.Superseded.ID != b.ID {
		t.Errorf("operator picked index 1 (b=%s) but superseded %s", b.ID, res.Superseded.ID)
	}
}

func TestHandleText_TargetIndexOutOfRange_Fails(t *testing.T) {
	a := mkEntry(t, "a", "fact A", time.Hour)
	b := mkEntry(t, "b", "fact B", 2*time.Hour)
	store := seedStore(t, a, b)
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "n", TargetIndex: 9}}
	c := &Confirmer{
		Store:    store,
		Resolver: FixedResolver{Candidates: []core.Entry{a, b}},
		Operator: op,
		Policy:   DefaultPolicy(),
		Now:      fixedClock(),
	}
	_, err := c.HandleText(context.Background(), "actually it's now corrected")
	if err == nil {
		t.Fatalf("expected out-of-range target index to fail")
	}
}

// ---------------------------------------------------------------------------
// Auto-accept policy
// ---------------------------------------------------------------------------

func TestHandleText_AutoAccept_Strong_Unambiguous(t *testing.T) {
	stale := mkEntry(t, "status", "deploy is pending", time.Hour)
	store := seedStore(t, stale)
	// Operator that would FAIL the test if consulted — auto-accept must bypass it.
	op := OperatorFunc(func(_ context.Context, _ Proposal) (Decision, error) {
		t.Fatalf("operator should NOT be consulted on auto-accept")
		return Decision{}, nil
	})
	c := &Confirmer{
		Store:    store,
		Resolver: FixedResolver{Candidates: []core.Entry{stale}}, // exactly one → unambiguous
		Operator: op,
		Policy:   Policy{MinConfidence: core.DetectWeak, AutoAcceptStrong: true},
		Now:      fixedClock(),
	}

	res, err := c.HandleText(context.Background(), "actually it's now deployed to prod")
	if err != nil {
		t.Fatalf("HandleText: %v", err)
	}
	if !res.Committed || !res.AutoAccepted {
		t.Fatalf("expected auto-accepted commit, got committed=%v auto=%v", res.Committed, res.AutoAccepted)
	}
	entries, edges, _ := store.Load(context.Background())
	// original + closed re-append + new = 3 entries, 1 edge (append-only).
	if len(entries) != 3 || len(edges) != 1 {
		t.Errorf("auto-accept did not persist the triple: %d entries %d edges", len(entries), len(edges))
	}
}

func TestHandleText_AutoAccept_RefusedWhenWeak(t *testing.T) {
	stale := mkEntry(t, "status", "owns the doc", time.Hour)
	store := seedStore(t, stale)
	consulted := false
	op := OperatorFunc(func(_ context.Context, _ Proposal) (Decision, error) {
		consulted = true
		return Decision{Confirm: false}, nil // human declines
	})
	c := &Confirmer{
		Store:    store,
		Resolver: FixedResolver{Candidates: []core.Entry{stale}},
		Operator: op,
		Policy:   Policy{MinConfidence: core.DetectWeak, AutoAcceptStrong: true},
		Now:      fixedClock(),
	}
	// A weak reframe ("ball is in X's court") must NOT auto-accept even with the flag on.
	_, err := c.HandleText(context.Background(), "ball is in Ed's court")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected weak detection to fall through to operator (then ErrRejected), got %v", err)
	}
	if !consulted {
		t.Errorf("weak detection should have been routed to the operator, not auto-accepted")
	}
}

func TestHandleText_AutoAccept_RefusedWhenAmbiguous(t *testing.T) {
	a := mkEntry(t, "a", "fact A", time.Hour)
	b := mkEntry(t, "b", "fact B", 2*time.Hour)
	store := seedStore(t, a, b)
	consulted := false
	op := OperatorFunc(func(_ context.Context, _ Proposal) (Decision, error) {
		consulted = true
		return Decision{Confirm: true, NewLabel: "picked", TargetIndex: 0}, nil
	})
	c := &Confirmer{
		Store:    store,
		Resolver: FixedResolver{Candidates: []core.Entry{a, b}}, // two → ambiguous
		Operator: op,
		Policy:   Policy{MinConfidence: core.DetectWeak, AutoAcceptStrong: true},
		Now:      fixedClock(),
	}
	res, err := c.HandleText(context.Background(), "actually it's now corrected")
	if err != nil {
		t.Fatalf("HandleText: %v", err)
	}
	if res.AutoAccepted {
		t.Errorf("ambiguous target must not auto-accept")
	}
	if !consulted {
		t.Errorf("ambiguous strong detection should route to the operator")
	}
}

// ---------------------------------------------------------------------------
// Policy.surfaces unit coverage
// ---------------------------------------------------------------------------

func TestPolicy_Surfaces(t *testing.T) {
	tests := []struct {
		name string
		pol  Policy
		conf core.DetectionConfidence
		want bool
	}{
		{"default surfaces strong", DefaultPolicy(), core.DetectStrong, true},
		{"default surfaces weak", DefaultPolicy(), core.DetectWeak, true},
		{"default drops none", DefaultPolicy(), core.DetectNone, false},
		{"strong-only drops weak", Policy{MinConfidence: core.DetectStrong}, core.DetectWeak, false},
		{"strong-only keeps strong", Policy{MinConfidence: core.DetectStrong}, core.DetectStrong, true},
		{"zero policy defaults to weak threshold", Policy{}, core.DetectWeak, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pol.surfaces(tc.conf); got != tc.want {
				t.Errorf("surfaces(%s) = %v, want %v", tc.conf, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Proposal.Summary smoke (presentation only; must not panic, must mention key bits)
// ---------------------------------------------------------------------------

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
}

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

// ---------------------------------------------------------------------------
// New() convenience wiring
// ---------------------------------------------------------------------------

func TestNew_DefaultWiring(t *testing.T) {
	stale := mkEntry(t, "x", "stale fact", time.Hour)
	store := seedStore(t, stale)
	op := &scripted{decision: Decision{Confirm: true, NewLabel: "fixed-now"}}
	c := New(store, op)
	c.Now = fixedClock() // override clock for determinism

	res, err := c.HandleText(context.Background(), "actually it's now fixed")
	if err != nil {
		t.Fatalf("HandleText via New(): %v", err)
	}
	if !res.Committed {
		t.Errorf("expected commit through default wiring")
	}
}
