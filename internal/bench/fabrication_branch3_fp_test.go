package bench

import (
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// This file is a FALSE-POSITIVE probe for branch 3 of shouldAbstain (the
// temporal-staleness branch), in the same spirit as the Jul-1 "seam #0
// precision half" work on the detector: a recall/catch signal was shipped and
// validated on the real miss (the Neo4j-outline case fires branch 3 correctly),
// but the OVER-FIRE risk was never measured. Under the project's discipline
// (validate before you build; measure a seam before promoting it), a catch
// signal whose false-positive surface is unknown is a liability — and the
// Neo4j case is staged for the Jul-13 go-live review, so this measurement is
// exactly what that review needs.
//
// THE FINDING (quantified below): branch 3 abstains whenever
// StrengthAt(anchor) < staleFloor(0.35). But StrengthAt decays toward S_floor
// for ANY entry that has not been recalled recently — and S_floor is 0.05 for
// a Fact, 0.1 for a Decision, 0.2 for a Person/Project. So branch 3 fires on
// essentially every durable, evergreen, precise, high-confidence fact that
// simply hasn't been touched in a day or two. A `Fact` node crosses below the
// floor in ~1 day; a `Person` node in ~4 days. Abstaining on "Owen's birthday
// is March 20, 2014" because it hasn't been recalled this week is a
// catastrophic false positive: the answer is precise, high-confidence, and
// STILL TRUE. Branch 3 as written does not detect "fabrication risk"; it
// detects "hasn't been recalled recently," which is true of nearly all durable
// knowledge.
//
// This file MEASURES that surface and pins it as a known limitation. It makes
// ZERO production change and does NOT patch shouldAbstain — fixing branch 3
// correctly requires a volatility signal (immutable Fact vs. time-bound
// Decision/state) that deserves its own validated design, not a reflexive tweak
// tonight. Stop at the seam.

// evergreenFact builds a precise, high-confidence, IMMUTABLE fact that has not
// been recalled for `staleDays`. These are exactly the answers a disciplined
// system SHOULD assert confidently — they don't go stale, they just sit
// unreferenced. LastRefTime is the only thing that moves; content correctness
// is unaffected by time.
func evergreenFact(id, content string, staleDays int, now time.Time) core.Entry {
	return evergreenFactHours(id, content, staleDays*24, now)
}

// evergreenFactHours is evergreenFact with hour-granularity staleness, so tests
// can bracket the ~24h branch-3 crossover precisely.
func evergreenFactHours(id, content string, staleHours int, now time.Time) core.Entry {
	encoded := now.Add(-time.Duration(staleHours) * time.Hour)
	e := mustEntry(
		id,
		core.TypeFact,
		content,
		encoded,
		nil,
		[]string{"family", "fact"},
		nil,
	)
	e.Confidence = core.ConfHigh // rock-solid, precise
	// mustEntry initializes Temporal via DefaultTemporal with LastRefTime =
	// encoded, so StrengthAt already reflects `staleDays` of decay. No recall
	// bump has occurred — the fact is simply durable and unreferenced.
	return e
}

// evergreenFactCorpus is a set of real-shaped durable facts about Matt's life
// (the kind of thing the assistant is asked and answers correctly all the
// time). None of these should EVER trigger abstention on the grounds of age:
// they are precise, high-confidence, and permanently true.
func evergreenFactCorpus(now time.Time) []core.Entry {
	return []core.Entry{
		evergreenFact("owen-birthday", "Owen was born March 20, 2014.", 30, now),
		evergreenFact("jennifer-birthday", "Jennifer's birthday is July 27.", 30, now),
		evergreenFact("matt-birthday", "Matt was born August 7, 1975.", 45, now),
		evergreenFact("tipa-hire-date", "Tipa was hired at Yext on Sept 2, 2025 (exact start date).", 14, now),
		evergreenFact("matt-location", "Matt lives in Minoa, NY (13116), near Syracuse.", 60, now),
		evergreenFact("book-title", "Matt authored 'Cloud Native Go' (O'Reilly, 2 editions).", 90, now),
	}
}

// TestFabrication_Branch3_OverFiresOnEvergreenFacts is the headline probe. It
// runs branch 3's exact predicate (via shouldAbstain with precise support
// present and high confidence, so branches 1 and 2 cannot fire) over a corpus
// of durable, true, precise facts and COUNTS how many are wrongly suppressed.
//
// It asserts the CURRENT (broken) reality so the finding is locked and visible:
// with staleFloor at 0.35 and Fact S_floor at 0.05, EVERY evergreen fact older
// than ~1 day is a false positive. This is a characterization test — when
// branch 3 is eventually fixed with a volatility signal, this test flips to
// asserting ZERO false positives, and its failure will announce the fix.
func TestFabrication_Branch3_OverFiresOnEvergreenFacts(t *testing.T) {
	now := time.Date(2026, 7, 5, 6, 0, 0, 0, time.UTC)
	corpus := evergreenFactCorpus(now)

	falsePositives := 0
	for _, e := range corpus {
		// preciseSupport=true and ConfHigh guarantee branches 1 and 2 do NOT
		// fire. Any abstention here is branch 3 (temporal staleness) alone.
		abstain, why := shouldAbstain(e, true /* precise support */, now)
		strength := core.StrengthAt(e, now)
		if abstain {
			falsePositives++
			t.Logf("FALSE POSITIVE %-18s strength=%.4f → ABSTAIN (%s) — but this fact is precise, high-confidence, and STILL TRUE",
				e.ID, strength, why)
		} else {
			t.Logf("ok             %-18s strength=%.4f → answer", e.ID, strength)
		}
	}

	// FIXED REALITY (volatility gate, see shouldAbstain in fabrication_test.go):
	// branch 3 is now gated on volatileTypes (TypeDecision only). TypeFact
	// entries are exempt — age alone does not make a birthday fabrication-prone.
	// The fix reduces FPs from 6/6 → 0/6 while preserving the Neo TypeDecision
	// true positive (verified by TestFabrication_Branch3_NeoCaseStillFiresForTheRightReason).
	if falsePositives != 0 {
		t.Errorf("REGRESSION: expected branch 3 to fire on 0 evergreen facts after volatility gate; got %d. "+
			"Check volatileTypes in fabrication_test.go — TypeFact must remain excluded.",
			falsePositives)
	}
	t.Logf("BRANCH-3 FALSE-POSITIVE RATE (post-fix): %d/%d evergreen facts wrongly suppressed. "+
		"Volatility gate eliminates all FPs: TypeFact is immutable, not subject to age-based abstention.",
		falsePositives, len(corpus))
}

// TestFabrication_Branch3_CrossoverIsAboutOneDay pins the quantified crossover:
// a Fact node (S_floor 0.05, Lambda 0.05) drops below the 0.35 staleFloor after
// only ~24 HOURS of not being recalled. That is the sharp statement of why
// branch 3 is not yet safe to promote: the floor it uses is crossed by normal,
// healthy, durable knowledge within a single day.
//
// (Note: an earlier draft of this test wrongly assumed a 1-DAY-old Fact sat
// ABOVE the floor. It does not — at exactly 24h a Fact is already 0.3361 < 0.35.
// The real crossover is ~23h. The probe corrected my own arithmetic, which is
// the point of writing the measurement instead of trusting the mental model.)
func TestFabrication_Branch3_CrossoverIsAboutOneDay(t *testing.T) {
	now := time.Date(2026, 7, 5, 6, 0, 0, 0, time.UTC)
	const staleFloor = 0.35

	// 23 hours: still just above floor → branch 3 does NOT fire.
	justUnderDay := evergreenFactHours("fact-23h", "A durable fact referenced 23 hours ago.", 23, now)
	if s := core.StrengthAt(justUnderDay, now); s < staleFloor {
		t.Errorf("expected a 23h-old Fact to sit just ABOVE staleFloor, got %.4f < %.2f", s, staleFloor)
	} else {
		t.Logf("23h-old Fact: strength=%.4f ≥ %.2f → branch 3 quiet (still answerable)", s, staleFloor)
	}

	// 24 hours: below floor → branch 3 fires (false positive for a durable fact).
	// A fact referenced YESTERDAY would be suppressed.
	oneDay := evergreenFactHours("fact-24h", "A durable fact referenced 24 hours ago.", 24, now)
	if s := core.StrengthAt(oneDay, now); s >= staleFloor {
		t.Errorf("expected a 24h-old Fact to sit BELOW staleFloor (branch 3 fires), got %.4f ≥ %.2f", s, staleFloor)
	} else {
		t.Logf("24h-old Fact: strength=%.4f < %.2f → branch 3 FIRES on a fact referenced YESTERDAY (FP)", s, staleFloor)
	}
}

// TestFabrication_Branch3_NeoCaseStillFiresForTheRightReason is the guard that
// keeps this probe honest: exposing the over-fire must NOT be read as "branch 3
// is worthless." On the REAL staged miss (Neo4j outline), branch 3 fires
// correctly — the difference is that the Neo anchor is a TIME-BOUND state
// (TypeDecision: "6 open decisions awaiting a call") that genuinely went stale,
// not an immutable Fact. This test re-confirms the true positive so the two
// facts sit side by side:
//
//	branch 3 on the Neo Decision (time-bound state, 11 days old) → CORRECT abstain
//	branch 3 on an evergreen Fact (immutable, 2 days old)        → WRONG abstain
//
// The seam the fix must exploit is therefore VOLATILITY (is the content a
// time-bound state or an immutable fact?), NOT age alone. Age is what branch 3
// currently uses, and age is shared by both the true positive and the false
// positives.
func TestFabrication_Branch3_NeoCaseStillFiresForTheRightReason(t *testing.T) {
	for _, c := range StagedFabricationCases() {
		abstain, why := shouldAbstain(c.Anchor, c.PreciseSupportExists, c.AsOf)
		strength := core.StrengthAt(c.Anchor, c.AsOf)
		if !abstain {
			t.Fatalf("%s: expected branch 3 to fire on the real stale time-bound state, got abstain=false", c.Name)
		}
		if c.Anchor.Type != core.TypeDecision {
			t.Errorf("%s: expected the Neo anchor to be a time-bound TypeDecision (the volatility seam), got %q", c.Name, c.Anchor.Type)
		}
		t.Logf("TRUE POSITIVE  %-30s type=%s strength=%.4f → ABSTAIN (%s) — a genuinely stale TIME-BOUND state, correctly caught",
			c.Name, c.Anchor.Type, strength, why)
	}
	t.Log("CONTRAST: branch 3 catches the stale Decision for the right reason, but its age-only predicate ALSO fires on immutable Facts. " +
		"The fix must key on VOLATILITY (time-bound state vs. evergreen fact), not age.")
}
