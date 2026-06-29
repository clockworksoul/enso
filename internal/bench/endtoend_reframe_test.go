package bench

import (
	"context"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/confirm"
	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/memstore"
)

// TestEndToEnd_ReframeRequiresOperatorContent is the REFRAME twin of
// TestEndToEnd_PreCorrectionCaptureLoop. The restate case (adam-headcount) is
// the easy half of the loop: the detector extracts the corrected statement
// straight out of the utterance ("actually X already landed…"), so a commit can
// in principle be assembled from detection alone. The reframe class is the hard
// half and the one the code keeps flagging as where recency is most dangerous,
// so it deserves its own end-to-end proof — AND it has a structural property the
// restate case does not, which this test pins as an executable invariant:
//
//	A REFRAME CANNOT BE COMMITTED FROM DETECTION ALONE.
//
// Why. The whose-court / owes signals (detect.go) deliberately have NO
// captureRe: "the ball is in Ed's court" tells you a FRAME flipped, but the
// corrected statement (what is now true, and crucially WHEN it became true) is
// not lexically present in the sentence. So DetectCorrection fires reframe/weak
// with Content="". Feeding that straight to the chokepoint produces an
// empty-content Correction, which core.Entry.Correct (via NewEntry.Validate)
// MUST reject. That rejection is not a bug to route around — it is the
// detect-don't-decide asymmetry made mechanical: with no reconsolidation a wrong
// auto-write is permanent corruption, so the riskiest miss class is structurally
// barred from the unattended path and forced through a human who supplies the
// reframed content (and the EventTime that makes the corrected fact older than
// the stale belief). This test asserts BOTH halves: the detection-only commit
// fails loudly, and the operator-completed commit then drives the full loop.
//
// Flow (starting, like reality, from the PRE-correction world):
//
//  1. DETECT   the real whose-court utterance → reframe/weak, Content=""
//  2. RESOLVE  the OPEN stale belief as the supersession target
//  3. REFUSE   committing from detection alone (empty content) — invariant
//  4. COMMIT   only after the operator supplies content + EventTime(May 26)
//  5. RECALL   Ensō ranks the reframed head first; baseline still fooled
func TestEndToEnd_ReframeRequiresOperatorContent(t *testing.T) {
	ctx := context.Background()

	// ── PRE-correction world ─────────────────────────────────────────────────
	// The Ed-thread belief as it stood before the reframe: "next move is on
	// Matt." Still OPEN, no SUPERSEDES edge. Left uncorrected it keeps being
	// re-affirmed at every thread reference and so looks perpetually fresh —
	// exactly the dynamic that made the real Jun-23 miss (the drafted email
	// opened "apologies for the gap on my end," implying Matt owed the move).
	may26 := time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC)       // when the reframed fact became true
	jun23affirm := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC) // last re-affirmation of the stale belief
	jun23 := time.Date(2026, 6, 23, 17, 0, 0, 0, time.UTC)       // the recall query (draft time)

	staleOpen := mustEntry(
		"ed-thread-matt-owes",
		core.TypeFact,
		"Neo4j blog: internal Yext legal cleared. Next action felt to be on Matt to push the thread forward.",
		jun23affirm,
		nil, // OPEN — no correction captured yet
		[]string{"work", "omega", "career"},
		[]string{"person:ed-sandoval", "project:neo4j-blog"},
	)

	store := memstore.New()
	if err := store.Append(ctx, []core.Entry{staleOpen}, nil); err != nil {
		t.Fatalf("seed open stale belief: %v", err)
	}

	// ── (1)(2) DETECT + RESOLVE via the real Propose path ────────────────────
	// The faithful reframe utterance — no seasoned "actually," exactly as the
	// correction entered the conversation.
	utterance := "the ball is in Ed's court, not Matt's"
	prop, ok, err := confirm.Propose(ctx, confirm.StoreResolver{Store: store}, utterance, jun23)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if !ok || !prop.Detection.IsCorrection {
		t.Fatalf("detector did not fire on the real reframe utterance: %q", utterance)
	}
	if prop.Detection.Kind != core.CorrectReframe {
		t.Fatalf("expected reframe classification, got %s", prop.Detection.Kind)
	}
	// The defining property of the class: nothing actionable was extracted.
	if prop.Detection.Content != "" {
		t.Fatalf("reframe unexpectedly extracted content %q — if the detector "+
			"grows whose-court extraction, this invariant test must be revisited "+
			"deliberately, not silently",
			prop.Detection.Content)
	}
	if len(prop.Candidates) == 0 {
		t.Fatalf("resolution returned no supersession target for the open stale belief")
	}
	target := prop.Candidates[0]
	if target.ID != staleOpen.ID {
		t.Fatalf("resolved the wrong target: got %s, want the open stale belief %s",
			target.ID, staleOpen.ID)
	}

	// ── (3) REFUSE — detection alone must NOT be committable ──────────────────
	// Assemble the Correction exactly as a naive caller would: straight from the
	// detection, no operator content. ToCorrection carries the empty Content
	// through, and the chokepoint must reject it. We assert the commit fails AND
	// the store is unchanged (no half-capture), which is the whole safety claim.
	beforeN, beforeE := store.Len()
	naive := prop.Detection.ToCorrection(jun23, "ed thread reframe", "")
	if _, err := core.CommitCorrection(ctx, store, target, naive); err == nil {
		t.Fatal("detection-only reframe commit SUCCEEDED — the empty-content " +
			"invariant is broken; the riskiest miss class is no longer barred " +
			"from the unattended path")
	}
	afterN, afterE := store.Len()
	if afterN != beforeN || afterE != beforeE {
		t.Fatalf("refused commit still mutated the store: entries %d→%d edges %d→%d "+
			"(INV-2 / atomic-or-nothing violated)", beforeN, afterN, beforeE, afterE)
	}

	// ── (4) COMMIT — only after the operator supplies content + EventTime ─────
	// The human completes what the sensor could not: the reframed statement and
	// the EventTime (May 26) that makes the corrected fact OLDER by world-time
	// than the stale belief — the distinction the reframe class exists for.
	newer, err := core.CommitCorrection(ctx, store, target, core.Correction{
		Kind:      prop.Detection.Kind, // reframe, from the detector
		Content:   "Neo4j blog: open dependency is on ED's side. Matt asked May 26 for guest-post submission terms; Ed punted to DevRel and went silent ~4 weeks. Ball in Ed's court.",
		NewLabel:  "ed thread ed owes terms",
		AsOf:      jun23,
		EventTime: &may26,
	})
	if err != nil {
		t.Fatalf("operator-completed reframe commit: %v", err)
	}
	// Provenance must record the class so a later audit sees a reframe happened.
	if got := newer.Extra[core.ExtraCorrectionKind]; got != string(core.CorrectReframe) {
		t.Errorf("reframe provenance not stamped: correction_kind=%q", got)
	}

	// ── (5) RECALL — re-rank the post-correction corpus ──────────────────────
	post, edges, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load post: %v", err)
	}
	// Reproduce the live failure dynamic: the stale belief is re-affirmed at the
	// very moment of the miss — drafting the email ("apologies for the gap on my
	// end") re-asserts "Matt owes the move," so its touch-time is the FRESHEST
	// thing in the corpus at query time, newer even than the just-encoded reframe.
	// That is what makes the case discriminate: recency points at the stale frame;
	// only supersession rescues the reframed fact (true since May 26).
	jun23scan := jun23.Add(1 * time.Minute)
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
		t.Errorf("Ensō recall FAILED on reframe: ranked %s first, want reframed head %s",
			enso[0].ID, newer.ID)
	}
	// And prove the case discriminates: the baseline, fooled by the re-affirmed
	// stale belief's fresh touch-time, still ranks the stale frame first.
	base := BaselineModel{}.Rank(post, edges, jun23)
	if base[0].ID == newer.ID {
		t.Errorf("baseline unexpectedly got the reframe right — case no longer "+
			"discriminates; baseline ranked %s first", base[0].ID)
	}

	t.Logf("reframe end-to-end OK: detect(reframe/%s,Content=\"\") → resolve(%s) → "+
		"REFUSE detection-only → operator-commit(%s) → enso ranks reframed head first; baseline fooled",
		prop.Detection.Confidence, target.ID, newer.ID)
}
