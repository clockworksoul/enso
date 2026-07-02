package confirm

import (
	"context"
	"testing"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/memstore"
)

// contradict_test.go proves the SEAM #0 completion is REACHABLE end to end from
// a store: the resolver-side contradiction path finds the bare-reaffirmation
// STALE miss that the lexical Propose path cannot, and stays silent otherwise.

// TestProposeContradiction_GranolaBanReachable is the headline: the exact
// held-out H1 miss, driven through a real store + resolver.
//   - Propose (lexical) MISSES the bare utterance (ok=false).
//   - ProposeContradiction FINDS it via the stored "banned" belief (ok=true).
func TestProposeContradiction_GranolaBanReachable(t *testing.T) {
	store := memstore.New()
	stale := mkEntry(t, "granola-banned",
		"Granola is banned per Yext policy (Jun 22). Default to the Zoom -> yext/transcripts replacement workflow.",
		48*60*60*1e9, // 48h ago (duration in ns)
	)
	if err := store.Append(context.Background(), []core.Entry{stale}, nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	resolver := StoreResolver{Store: store}

	utter := "Granola still works" // bare — no lexical correction marker

	// Lexical path misses.
	if _, ok, err := Propose(context.Background(), resolver, utter, fixedNow); err != nil {
		t.Fatalf("Propose err: %v", err)
	} else if ok {
		t.Fatalf("expected lexical Propose to MISS bare utterance, but it fired")
	}

	// Contradiction path finds it.
	prop, ok, err := ProposeContradiction(context.Background(), resolver, utter, fixedNow)
	if err != nil {
		t.Fatalf("ProposeContradiction err: %v", err)
	}
	if !ok {
		t.Fatalf("expected ProposeContradiction to FIND the stored contradiction")
	}
	if len(prop.Candidates) != 1 {
		t.Fatalf("expected exactly 1 contradicted candidate, got %d", len(prop.Candidates))
	}
	if prop.Candidates[0].ID != stale.ID {
		t.Errorf("wrong candidate: got %s, want %s", prop.Candidates[0].ID, stale.ID)
	}
	if prop.Detection.Kind != core.CorrectRestate {
		t.Errorf("want restate, got %s", prop.Detection.Kind)
	}
	if prop.Detection.Confidence != core.DetectWeak {
		t.Errorf("want weak, got %s", prop.Detection.Confidence)
	}
	if prop.Detection.Content != "" {
		t.Errorf("contradiction must carry empty content (operator-supplied), got %q", prop.Detection.Content)
	}
}

// TestProposeContradiction_SilentOnInnocentCorpus: an ordinary status remark
// against a corpus with NO contradicting belief stays silent (no false alarm).
func TestProposeContradiction_SilentOnInnocentCorpus(t *testing.T) {
	store := memstore.New()
	ordinary := mkEntry(t, "staging-note",
		"The staging environment mirrors production for the checkout flow.", 24*60*60*1e9)
	if err := store.Append(context.Background(), []core.Entry{ordinary}, nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	resolver := StoreResolver{Store: store}

	for _, u := range []string{
		"the staging link is still live",
		"the checkout flow still works",
		"the API key still works",
	} {
		if _, ok, err := ProposeContradiction(context.Background(), resolver, u, fixedNow); err != nil {
			t.Fatalf("err on %q: %v", u, err)
		} else if ok {
			t.Errorf("false positive: %q fired against an innocent corpus", u)
		}
	}
}

// TestProposeContradiction_MixedCorpusNoCrossFire: a corpus holding BOTH an
// unrelated negation note ("the old VPN is deprecated") AND innocent notes must
// not let an affirmation about a DIFFERENT subject cross-match the negation.
// This is the realistic multi-entry false-positive risk the single-entry core
// tests can't exercise.
func TestProposeContradiction_MixedCorpusNoCrossFire(t *testing.T) {
	store := memstore.New()
	if err := store.Append(context.Background(), []core.Entry{
		mkEntry(t, "old-vpn-deprecated", "The old VPN client is deprecated; migrate to Twingate.", 72*60*60*1e9),
		mkEntry(t, "checkout-note", "The checkout flow mirrors production in staging.", 24*60*60*1e9),
	}, nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	resolver := StoreResolver{Store: store}

	// Affirmations about the checkout/staging/API — NOT the deprecated VPN.
	for _, u := range []string{
		"the checkout flow still works",
		"staging is still live",
		"the API key still works fine",
	} {
		if _, ok, err := ProposeContradiction(context.Background(), resolver, u, fixedNow); err != nil {
			t.Fatalf("err on %q: %v", u, err)
		} else if ok {
			t.Errorf("false positive: %q cross-fired against the unrelated VPN-deprecated note", u)
		}
	}

	// But a real affirmation about the VPN SHOULD fire.
	if _, ok, err := ProposeContradiction(context.Background(), resolver, "the old VPN client still works", fixedNow); err != nil {
		t.Fatalf("err: %v", err)
	} else if !ok {
		t.Error("expected the VPN affirmation to contradict the VPN-deprecated note")
	}
}

// TestProposeContradiction_IgnoresSupersededBelief: once the ban belief has been
// superseded (it is no longer current), the same utterance no longer
// contradicts anything — the loop does not re-fire on a belief already fixed.
func TestProposeContradiction_IgnoresSupersededBelief(t *testing.T) {
	store := memstore.New()
	stale := mkEntry(t, "granola-banned", "Granola is banned per Yext policy.", 48*60*60*1e9)

	// Supersede it through the real path: Correct returns the closed stale entry
	// (ValidUntil set), the new current entry, and the SUPERSEDES edge.
	closed, current, edge, err := stale.Correct(core.Correction{
		Kind:       core.CorrectRestate,
		Content:    "Granola still works and is the transcript source of record; the ban is not operative.",
		NewLabel:   "granola still source of record",
		AsOf:       fixedNow.Add(-1 * 60 * 60 * 1e9),
		Confidence: core.ConfHigh,
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if err := store.Append(context.Background(),
		[]core.Entry{closed, current}, []core.Edge{edge}); err != nil {
		t.Fatalf("append: %v", err)
	}
	resolver := StoreResolver{Store: store}

	// The stale belief is closed; the current belief carries no negation. So a
	// re-affirmation must NOT contradict anything.
	if _, ok, err := ProposeContradiction(context.Background(), resolver, "Granola still works", fixedNow); err != nil {
		t.Fatalf("err: %v", err)
	} else if ok {
		t.Error("false positive: fired against an already-superseded ban belief")
	}
}
