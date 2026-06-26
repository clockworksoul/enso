package bench

import (
	"context"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/confirm"
	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/memstore"
)

// TestEndToEnd_PreCorrectionCaptureLoop is the first test of the WHOLE Phase-1
// arc on a single real case, starting from the PRE-correction world.
//
// Every other test so far validated a link in isolation, and the seed cases in
// cases.go start AFTER the correction already closed the stale entry (so they
// prove ranking, not capture). This test starts where reality starts: a stale
// entry that is still OPEN, no SUPERSEDES edge yet, and a raw correction
// utterance. It then runs the entire loop end to end:
//
//  1. DETECT   the utterance (confirm.Propose over the live store)
//  2. RESOLVE  the open stale entry as a supersession target
//  3. COMMIT   the correction (core.CommitCorrection — the real chokepoint)
//  4. RECALL   re-rank with the Ensō model and assert the NEW head now wins,
//     and that the naive baseline would still have been fooled.
//
// If this passes, the Phase-1 value claim ("Ensō recovers the current answer
// where the flat model loses") is proven on one real case end to end — not
// assumed. If RESOLVE picks the wrong target, that is the experiment correctly
// surfacing that content-blind StoreResolver needs a content-aware layer; we
// would stop there rather than guess a fix.
func TestEndToEnd_PreCorrectionCaptureLoop(t *testing.T) {
	ctx := context.Background()

	// ── PRE-correction world ─────────────────────────────────────────────────
	// The adam-headcount stale TODO as it existed on Jun 16: still OPEN (no
	// ValidUntil), no correction captured yet. This is the entry that, left
	// uncorrected, keeps winning recall and produces the real miss.
	jun16 := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	jun18 := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC) // when the correction lands
	jun23 := time.Date(2026, 6, 23, 21, 0, 0, 0, time.UTC) // the later recall query

	staleOpen := mustEntry(
		"adam-headcount-todo",
		core.TypeTask,
		"Message Adam re: Axon headcount/investment. Target Jun 16; prep slipped.",
		jun16,
		nil, // OPEN — not yet superseded
		[]string{"work", "career", "team"},
		[]string{"person:adam", "project:axon"},
	)

	store := memstore.New()
	if err := store.Append(ctx, []core.Entry{staleOpen}, nil); err != nil {
		t.Fatalf("seed open stale entry: %v", err)
	}

	// Sanity: before any correction, the stale entry is the current head and
	// therefore the thing recall would surface. This is the miss we are about to
	// fix — assert the pre-correction state actually exhibits it.
	pre, _, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load pre: %v", err)
	}
	if len(pre) != 1 || !pre[0].IsCurrent(jun23) {
		t.Fatalf("pre-correction: expected exactly one open current entry, got %d", len(pre))
	}

	// ── (1)(2) DETECT + RESOLVE via the real Propose path ────────────────────
	// The real Jun-18 correction utterance, faithfully (no seasoned markers).
	utterance := "actually the Adam headcount ask already landed at the Jun 18 1:1"
	prop, ok, err := confirm.Propose(ctx, confirm.StoreResolver{Store: store}, utterance, jun18)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if !ok || !prop.Detection.IsCorrection {
		t.Fatalf("detector did not fire on the real correction utterance: %q", utterance)
	}
	if len(prop.Candidates) == 0 {
		t.Fatalf("resolution returned no supersession target for an open stale entry")
	}
	// The open stale entry must be among the resolved candidates — it is the
	// thing the correction supersedes. With one entry in the store it must be
	// candidate 0; assert it explicitly so a future multi-candidate corpus that
	// resolves the WRONG target fails loudly here (the content-blindness seam).
	target := prop.Candidates[0]
	if target.ID != staleOpen.ID {
		t.Fatalf("resolved the wrong target: got %s, want the open stale entry %s",
			target.ID, staleOpen.ID)
	}

	// ── (3) COMMIT via the real chokepoint ───────────────────────────────────
	newer, err := core.CommitCorrection(ctx, store, target, core.Correction{
		Kind:     prop.Detection.Kind, // restate, from the detector
		Content:  "Adam headcount ask landed at the Jun 18 1:1. Adam aligned; prefers an internal move; no specific person yet.",
		NewLabel: "adam headcount landed",
		AsOf:     jun18,
		Type:     core.TypeDecision,
	})
	if err != nil {
		t.Fatalf("commit correction: %v", err)
	}

	// ── (4) RECALL — re-rank the post-correction corpus ──────────────────────
	post, edges, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load post: %v", err)
	}
	// The store now holds: the closed stale entry, the new current head, and a
	// SUPERSEDES edge (From:newer To:stale). Reproduce the live failure dynamic
	// that makes recency insufficient: the closed TODO keeps getting re-scanned,
	// so by touch-recency it looks fresher than the buried Jun-18 outcome right
	// up to the query. Only supersession rescues it.
	jun23scan := time.Date(2026, 6, 23, 20, 0, 0, 0, time.UTC)
	for i := range post {
		if post[i].ID == staleOpen.ID {
			post[i].EncodedTime = jun23scan
			post[i].Temporal.LastRefTime = jun23scan
		}
	}

	enso := EnsoModel{}.Rank(post, edges, jun23)
	if len(enso) == 0 {
		t.Fatal("enso ranking returned nothing")
	}
	if enso[0].ID != newer.ID {
		t.Errorf("Ensō recall FAILED: ranked %s first, want the new current head %s",
			enso[0].ID, newer.ID)
	}

	// And prove the case is discriminating: the naive baseline, fooled by the
	// re-scanned stale entry's fresh touch-time, still ranks the stale one first.
	base := BaselineModel{}.Rank(post, edges, jun23)
	if base[0].ID == newer.ID {
		t.Errorf("baseline unexpectedly got it right — case no longer discriminates; "+
			"baseline ranked %s first", base[0].ID)
	}

	t.Logf("end-to-end OK: detect(%s/%s) → resolve(%s) → commit(%s) → enso ranks new head first; baseline ranks stale first",
		prop.Detection.Kind, prop.Detection.Confidence, target.ID, newer.ID)
}
