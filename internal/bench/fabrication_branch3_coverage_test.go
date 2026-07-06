package bench

import (
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// This file is the COVERAGE-HOLE (false-negative) probe for branch 3 of
// shouldAbstain — the complement of the Jul-5 false-positive probe
// (fabrication_branch3_fp_test.go).
//
// BACKGROUND. Branch 3 abstains when a stale, decayed anchor is used to assert
// a confident current-state answer (the Neo4j-outline fabrication: a Jun-22
// entry elaborated into a current scenario 11 days later). The Jul-5 FP probe
// found the FIRST-CUT age-only predicate over-fired 6/6 on evergreen Facts, and
// the fix (commit f611767) gated branch 3 on a VOLATILITY set:
//
//	var volatileTypes = map[core.NodeType]bool{ core.TypeDecision: true }
//
// That fix eliminated the FP surface (TypeFact is exempt → 0/6). GOOD. But it
// introduced a symmetric, UNMEASURED risk on the other side: the gate now
// trusts a single NodeType label (TypeDecision) to stand in for "this content
// is time-bound state." Any stale time-bound entry carrying a DIFFERENT type
// label now slips branch 3 entirely and would be confidently confabulated —
// the exact Neo-case miss shape, invisible because it wasn't typed Decision.
//
// This matters because the real Neo fabrication is fundamentally a PROJECT-
// STATUS confabulation ("the blog project still has 6 open decisions") that
// merely HAPPENS to be modeled as TypeDecision. Had the same stale state been
// encoded as a TypeProject status note or a TypeTask, the identical miss would
// slip. The volatility gate's narrowness is deliberate and correct (widening it
// against n=1 is the trap the project's discipline names) — but the COVERAGE
// HOLE that narrowness creates has never been quantified. The Jul-13 go-live
// review needs BOTH error surfaces measured, not just the FP side.
//
// So this file MEASURES the false-negative surface and pins it as a known,
// intentional limitation. It makes ZERO production change and does NOT widen
// volatileTypes. Stop at the seam: characterize precisely, defer the fix to a
// real second time-bound-state case.

// staleTimeBoundState builds a stale, decayed entry carrying genuinely
// time-bound state (the Neo-case content shape: "N open decisions awaiting a
// call") under an arbitrary node type, so we can ask branch 3 the same question
// across every type. Content is IDENTICAL across types — only the type label
// changes — which isolates the variable under test: does branch 3's firing
// depend on the type LABEL rather than on content volatility?
func staleTimeBoundState(id string, nt core.NodeType, staleDays int, now time.Time) core.Entry {
	encoded := now.Add(-time.Duration(staleDays) * 24 * time.Hour)
	e := mustEntry(
		id,
		nt,
		"Project has 6 open decisions awaiting a call (a time-bound status snapshot as of encoding).",
		encoded,
		nil,
		[]string{"work", "status"},
		[]string{"project:example"},
	)
	// High confidence AS OF ENCODING — like the Neo anchor, it was correct and
	// precise when written. The fabrication trap is treating a
	// high-confidence-as-of-then state as high-confidence-now after decay, so
	// branches 1 and 2 must NOT fire; branch 3 is the only relevant signal.
	e.Confidence = core.ConfHigh
	return e
}

// TestFabrication_Branch3_CoverageHole is the headline false-negative probe. It
// takes ONE stale (11-day) time-bound-state content and stamps it with each of
// the six node types, then measures which ones branch 3 catches. The staleness
// and content are identical across all six; only the type label differs.
//
// EXPECTED (current gated reality): branch 3 fires for TypeDecision ONLY. Every
// other type — including TypeProject and TypeTask, which carry genuinely
// time-bound state — is a FALSE NEGATIVE: the same stale-state confabulation
// would be asserted confidently. This is pinned as the current measured reality
// so the go-live review sees the exact size and shape of the coverage hole.
//
// This is a characterization test. When volatileTypes is eventually broadened
// (against a real second time-bound-state case, per the discipline), the
// expected-caught set here changes and this test's failure will ANNOUNCE the
// widening — forcing a conscious decision, never a silent drift.
func TestFabrication_Branch3_CoverageHole(t *testing.T) {
	now := time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC)
	const staleDays = 11 // same age as the real Neo miss

	allTypes := []core.NodeType{
		core.TypeFact,
		core.TypeDecision,
		core.TypeInsight,
		core.TypePerson,
		core.TypeProject,
		core.TypeTask,
	}

	// The ONLY type the current volatility gate treats as branch-3-eligible.
	// Keep this in sync with volatileTypes in fabrication_test.go — the test
	// asserts against it explicitly so a change to volatileTypes that isn't
	// mirrored here fails loudly.
	caughtExpected := map[core.NodeType]bool{
		core.TypeDecision: true,
	}

	falseNegatives := 0
	for _, nt := range allTypes {
		anchor := staleTimeBoundState("stale-state-"+string(nt), nt, staleDays, now)
		// preciseSupport=true and ConfHigh guarantee branches 1 and 2 do NOT
		// fire, so any abstention (or its absence) is branch 3 alone.
		abstain, why := shouldAbstain(anchor, true /* precise support */, now)
		strength := core.StrengthAt(anchor, now)

		if abstain != caughtExpected[nt] {
			t.Errorf("type %-9s: branch-3 abstain=%v, expected %v (volatileTypes drift?) reason=%q strength=%.4f",
				nt, abstain, caughtExpected[nt], why, strength)
		}

		if !caughtExpected[nt] {
			// This stale time-bound state is NOT caught: a false negative. The
			// identical confabulation would be asserted confidently.
			falseNegatives++
			t.Logf("FALSE NEGATIVE %-9s strength=%.4f → ANSWER (branch 3 quiet) — same stale time-bound state as the Neo miss, but not typed Decision, so it slips",
				nt, strength)
		} else {
			t.Logf("caught         %-9s strength=%.4f → ABSTAIN (%s)", nt, strength, why)
		}
	}

	// PIN THE HOLE: with volatileTypes = {Decision}, 5 of 6 type labels carrying
	// identical stale time-bound state slip branch 3. This is the measured
	// false-negative surface. It is INTENTIONAL (narrow gate beats an
	// over-broad one that re-creates the 6/6 FP problem), but it must be VISIBLE.
	const wantFN = 5
	if falseNegatives != wantFN {
		t.Errorf("expected %d false negatives across the 6 node types (only TypeDecision gated), got %d — "+
			"volatileTypes likely changed; update caughtExpected and re-review the go-live coverage tradeoff",
			wantFN, falseNegatives)
	}
	t.Logf("BRANCH-3 COVERAGE HOLE: %d/%d node types carrying identical stale time-bound state slip branch 3 "+
		"(only TypeDecision is gated). Intentional narrowness; quantified here for the Jul-13 go-live review.",
		falseNegatives, len(allTypes))
}

// TestFabrication_Branch3_ProjectStatusIsTheSharpestMiss isolates the single
// most defensible widening candidate and states why it is deferred, not done.
//
// TypeProject is the sharpest false negative because the REAL Neo miss is, in
// substance, a project-status confabulation ("the blog PROJECT still has 6 open
// decisions"). It is modeled as TypeDecision only because that's how the anchor
// happened to be encoded. A stale TypeProject status note is the same miss
// wearing a different label — and it slips.
//
// Yet this test deliberately does NOT widen volatileTypes to include
// TypeProject. Two reasons, both from the project's own discipline:
//
//  1. n=1. There is exactly ONE real time-bound-state case (Neo), and it is
//     TypeDecision. Widening to TypeProject on zero real TypeProject misses is
//     tuning a layer against a case that doesn't exist — the exact trap the
//     state-of-the-loop map names for every open seam.
//  2. TypeProject has a HIGHER SFloor (0.2) and LOWER Lambda (0.02) than
//     Decision — it is deliberately the SLOWEST-decaying type, precisely
//     BECAUSE project identity ("the Neo4j blog exists") is meant to be
//     durable. Naively gating branch 3 on TypeProject would abstain on durable
//     project-IDENTITY facts, re-creating a Fact-like FP surface for the
//     identity half of a Project. TypeProject conflates durable identity with
//     volatile status; branch 3 needs to key on the CONTENT (status snapshot vs
//     identity), which the current type-only gate cannot express.
//
// So the honest finding is: the volatility gate cannot be widened by TYPE
// alone without either missing real status confabulations (as now) or
// re-inflating FPs on durable identity. The real fix is a CONTENT volatility
// signal, and it is blocked on a second real time-bound-state miss to validate
// against. This test records that reasoning executably and asserts the current
// (deferred) behavior: TypeProject slips.
func TestFabrication_Branch3_ProjectStatusIsTheSharpestMiss(t *testing.T) {
	now := time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC)

	// The same stale time-bound status as the Neo miss, but typed as the thing
	// it substantively IS: a project status snapshot.
	proj := staleTimeBoundState("stale-project-status", core.TypeProject, 11, now)
	abstain, why := shouldAbstain(proj, true, now)
	strength := core.StrengthAt(proj, now)

	if abstain {
		t.Fatalf("unexpected: branch 3 fired on TypeProject (strength=%.4f, %q). "+
			"If volatileTypes was widened to include TypeProject, this test and the "+
			"coverage-hole count must be re-reviewed against the FP risk on durable "+
			"project-identity content (SFloor 0.2, Lambda 0.02).", strength, why)
	}
	t.Logf("TypeProject stale status (strength=%.4f) → ANSWER: the sharpest false negative (same substance as the Neo miss) "+
		"is DEFERRED, not fixed. Widening needs a real 2nd time-bound-state case AND a content-volatility signal, "+
		"because TypeProject also carries durable identity (SFloor 0.2) that must NOT abstain on age.", strength)
}

// TestFabrication_Branch3_TypeTaskIsTheCleanestFutureGate records the cleanest
// eventual widening: TypeTask. Unlike TypeProject, a Task is UNAMBIGUOUSLY
// time-bound (a task is a status by nature — open/done), with the highest
// Lambda (0.10) and lowest SFloor (0.05) of any type, so a stale Task is almost
// definitionally a decayed status. The current gate still excludes it (no
// corpus case), so a stale Task confabulation slips today. This pins that gap
// and marks TypeTask as the first widening to make WHEN a real TypeTask
// confabulation lands as the regression anchor.
func TestFabrication_Branch3_TypeTaskIsTheCleanestFutureGate(t *testing.T) {
	now := time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC)

	task := staleTimeBoundState("stale-task-status", core.TypeTask, 11, now)
	abstain, why := shouldAbstain(task, true, now)
	strength := core.StrengthAt(task, now)

	if abstain {
		t.Fatalf("unexpected: branch 3 fired on TypeTask (strength=%.4f, %q). If volatileTypes was "+
			"widened to include TypeTask, update the coverage-hole count and this test.", strength, why)
	}
	// Sanity: a Task decays fastest (Lambda 0.10), so an 11-day stale Task is
	// deep below the floor — it is the LEAST ambiguous stale-status case and
	// would be the safest first widening. Confirm it's well below floor so the
	// "cleanest gate" claim is grounded in the actual decay, not assertion.
	const staleFloor = 0.35
	if strength >= staleFloor {
		t.Errorf("expected an 11-day stale Task to be deep below staleFloor %.2f (fastest decay), got %.4f", staleFloor, strength)
	}
	t.Logf("TypeTask stale status (strength=%.4f, deep below floor) → ANSWER today, but it is the CLEANEST future widening: "+
		"a Task is time-bound by nature (Lambda 0.10, no durable-identity half like Project). "+
		"Add TypeTask to volatileTypes when a real TypeTask confabulation provides the regression anchor.", strength)
}
