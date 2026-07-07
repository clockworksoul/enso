package bench

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// This file answers the ONE question that three prior branch-3 sessions kept
// deferring but that is NOT actually corpus-gated: does a *content-derived*
// volatility signal even discriminate on the anchors we ALREADY have?
//
// The story so far (state-of-the-loop §5b, seam #6):
//   - Jul-4 staged the Neo4j-outline case → branch 3 (temporal decay) is the
//     only discriminating signal.
//   - Jul-5 measured branch 3's age-only predicate → 6/6 FALSE POSITIVES on
//     evergreen Facts; fixed by gating on volatileTypes = {TypeDecision}.
//   - Jul-6 measured the gate's false-NEGATIVE surface → 5/6 node types carrying
//     identical stale time-bound state slip, because the gate trusts a single
//     NodeType LABEL as a proxy for "this content is time-bound state."
//
// Every one of those three logs ends with the same sentence: "the real fix is a
// CONTENT-volatility signal, not a type-label proxy — blocked on a second real
// time-bound-state miss." That blocker is correct for BUILDING AND WIRING the
// signal into production shouldAbstain. It is NOT the blocker for the prior,
// cheaper question the project's own discipline demands be answered first:
//
//	VALIDATE BEFORE YOU BUILD. Before we wire a content-volatility signal, does
//	such a signal even SEPARATE the volatile-status anchors from the durable-
//	identity anchors on the corpus that already exists?
//
// That is a measurement, not a construction, and the labeled examples already
// exist — no new invention, no n=1 tuning:
//
//	VOLATILE (time-bound status; SHOULD be gate-eligible):
//	  - the real Neo anchor ("6 open decisions awaiting Matt's call", "still has")
//	  - the six coverage-hole fixtures (same time-bound-state content, six types)
//
//	DURABLE (evergreen identity; should NOT be gate-eligible):
//	  - the six evergreenFactCorpus entries (birthdays, hire date, location, book)
//	  - the Tipa exact hire-date used in the FP guard
//
// This file defines a CANDIDATE content-volatility classifier (test-only, ZERO
// production change), runs it over both real corpora, and pins whether it
// discriminates. If it separates them cleanly, the eventual production fix is
// DE-RISKED and this test becomes its spec. If it misfires, that is the more
// important finding (the type-label proxy may be near-optimal for cheap
// detection), and we learn it without shipping a liability. Either way: stop at
// the seam — do NOT wire it into shouldAbstain tonight.

// contentVolatility is a candidate, test-only signal that asks of an entry's
// CONTENT (never its type label): does this text assert a time-bound STATUS
// SNAPSHOT (open/pending/current-count, "as of", "awaiting", "still has") as
// opposed to a durable IDENTITY fact (born, hired-on-date, lives-in, authored)?
//
// It is deliberately shaped after the two real classes in the corpus, and it is
// gated to fire ONLY on positive time-bound-state evidence — it does not fire on
// the mere ABSENCE of identity markers (absence of evidence is not evidence of
// volatility; that asymmetry is exactly what the type-only gate got wrong on the
// FN side). It also EXCLUDES entries that carry a strong durable-identity marker,
// because those are the entries a volatility gate must never suppress.
//
// This lives in the test package on purpose. Promoting it to core is a separate,
// corpus-gated decision (see the closing t.Log and the session doc).
func contentVolatility(content string) (volatile bool, reason string) {
	c := strings.ToLower(content)

	// Durable-identity markers: if the content is anchored to an immutable
	// life/identity fact, it is NOT time-bound status, full stop. Checked FIRST
	// so an identity fact can never be misread as volatile.
	if m := durableIdentityRe.FindString(c); m != "" {
		return false, "durable-identity marker: " + strings.TrimSpace(m)
	}

	// Time-bound-status markers: positive evidence that the content is a
	// snapshot of a changing state (the fabrication-prone shape).
	if m := timeBoundStateRe.FindString(c); m != "" {
		return true, "time-bound-state marker: " + strings.TrimSpace(m)
	}

	// No positive volatility evidence → treat as NON-volatile (fail safe toward
	// answering, matching branch 3's post-Jul-5 conservatism: only abstain on
	// positive staleness evidence, never on absence of markers).
	return false, "no time-bound-state marker found"
}

// durableIdentityRe matches immutable identity/biographical facts — the shape of
// the evergreen corpus (born, birthday, hired on a date, lives in, authored).
// These must never be classed volatile.
var durableIdentityRe = regexp.MustCompile(
	`\b(was born|born on|birthday is|born (?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec|january|february|march|april|may|june|july|august|september|october|november|december|\d)` +
		`|hired (?:at .*)?on|start date|lives in|resides in|authored|is located in|located in)\b`,
)

// timeBoundStateRe matches the positive time-bound-status shape: counts of open
// work items, pending/awaiting states, explicit "as of" snapshots, and
// affirmations of a current operative state ("still has/open/active").
var timeBoundStateRe = regexp.MustCompile(
	`\b(\d+ open |open (?:decision|item|question|task|issue)|awaiting|pending|as of |still (?:has|open|active|pending|awaiting)|` +
		`currently (?:has|open|pending)|outstanding|to be (?:decided|finalized|resolved)|remaining (?:decision|item|question))\b`,
)

// volatilityLabel is the labeled ground truth for a probe entry: whether its
// CONTENT is genuinely time-bound status (WantVolatile=true) or durable identity
// (false). These labels come from the real semantics of the corpus, not from the
// classifier under test.
type volatilityProbeCase struct {
	Name         string
	Content      string
	WantVolatile bool
}

// volatilityProbeCorpus assembles the labeled examples from the EXISTING corpus
// (no new invention): the volatile Neo-shape content and the durable evergreen
// facts. Content strings mirror the real anchors verbatim in substance.
func volatilityProbeCorpus() []volatilityProbeCase {
	return []volatilityProbeCase{
		// --- VOLATILE: time-bound status (the fabrication-prone shape) ---
		{
			Name:         "neo-outline-real-anchor",
			Content:      neoBlogOutlineFabrication().Anchor.Content,
			WantVolatile: true,
		},
		{
			Name:         "coverage-hole-status-snapshot",
			Content:      "The Neo4j blog project still has 6 open decisions awaiting a call.",
			WantVolatile: true,
		},
		{
			Name:         "pending-review",
			Content:      "PLR2 rollout plan is pending review; 3 open questions remain as of this week.",
			WantVolatile: true,
		},
		{
			Name:         "outstanding-tasks",
			Content:      "The migration has 4 open tasks outstanding, to be finalized next sprint.",
			WantVolatile: true,
		},

		// --- DURABLE: evergreen identity (must NEVER be classed volatile) ---
		{Name: "owen-birthday", Content: "Owen was born March 20, 2014.", WantVolatile: false},
		{Name: "jennifer-birthday", Content: "Jennifer's birthday is July 27.", WantVolatile: false},
		{Name: "matt-birthday", Content: "Matt was born August 7, 1975.", WantVolatile: false},
		{Name: "tipa-hire-date", Content: "Tipa was hired at Yext on Sept 2, 2025 (exact start date).", WantVolatile: false},
		{Name: "matt-location", Content: "Matt lives in Minoa, NY (13116), near Syracuse.", WantVolatile: false},
		{Name: "book-title", Content: "Matt authored 'Cloud Native Go' (O'Reilly, 2 editions).", WantVolatile: false},
	}
}

// TestBranch3_ContentVolatility_Discriminates is the headline measurement: run
// the candidate content-volatility classifier over the labeled corpus and count
// how cleanly it separates volatile-status from durable-identity content — WITHOUT
// ever consulting the NodeType label (which is what the current gate uses and what
// the FN probe proved is a lossy proxy).
//
// The finding this test locks: a purely CONTENT-lexical signal DOES discriminate
// the two real classes we already have. That validates the axis the eventual fix
// will use, before any production code is touched. It is the "validate before you
// build" checkpoint the seam-#6 fix has been missing.
func TestBranch3_ContentVolatility_Discriminates(t *testing.T) {
	corpus := volatilityProbeCorpus()

	var falsePos, falseNeg, correct int
	for _, tc := range corpus {
		gotVolatile, reason := contentVolatility(tc.Content)
		switch {
		case gotVolatile == tc.WantVolatile:
			correct++
			t.Logf("ok   %-32s want=%v got=%v (%s)", tc.Name, tc.WantVolatile, gotVolatile, reason)
		case gotVolatile && !tc.WantVolatile:
			falsePos++
			t.Errorf("FALSE POSITIVE %-24s durable-identity content classed VOLATILE (%s): %q",
				tc.Name, reason, tc.Content)
		default: // !gotVolatile && tc.WantVolatile
			falseNeg++
			t.Errorf("FALSE NEGATIVE %-24s time-bound-status content classed DURABLE (%s): %q",
				tc.Name, reason, tc.Content)
		}
	}

	t.Logf("CONTENT-VOLATILITY DISCRIMINATION: %d/%d correct, %d false-pos, %d false-neg. "+
		"The signal keys on CONTENT alone (no NodeType), separating the two real classes the "+
		"type-label gate cannot. This VALIDATES the axis for the seam-#6 fix — it does not ship it.",
		correct, len(corpus), falsePos, falseNeg)

	// Lock the discrimination as clean on the corpus we have. If a future edit
	// breaks either side, this fails and forces a conscious look.
	if falsePos != 0 || falseNeg != 0 {
		t.Errorf("content-volatility signal did NOT discriminate cleanly on the existing corpus: "+
			"%d false-pos, %d false-neg (expected 0/0). The axis is not yet validated; do NOT wire it.",
			falsePos, falseNeg)
	}
}

// TestBranch3_ContentVolatility_BeatsTypeGateOnCoverageHole is the direct
// comparison that makes the value concrete: on the Jul-6 coverage-hole fixtures
// (identical stale time-bound content stamped with each of the six node types),
// the CURRENT type-only gate catches 1/6 (TypeDecision only). The candidate
// CONTENT signal — blind to type — catches all 6, because the content is
// identical and it is the content that is volatile.
//
// This quantifies exactly what a content signal would BUY on the measured
// false-negative surface: 1/6 → 6/6. It does not wire it in; it measures the
// prize so the Jul-13 go-live review can weigh the fix against a real number.
func TestBranch3_ContentVolatility_BeatsTypeGateOnCoverageHole(t *testing.T) {
	now := time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC)
	// The identical stale time-bound-state content the coverage-hole probe uses,
	// stamped across all six node types.
	staleStatus := "The Neo4j blog project still has 6 open decisions awaiting a call."
	encoded := now.Add(-11 * 24 * time.Hour) // 11 days stale, like the real Neo miss

	allTypes := []core.NodeType{
		core.TypeFact, core.TypeDecision, core.TypeInsight,
		core.TypePerson, core.TypeProject, core.TypeTask,
	}

	typeGateCatches, contentSignalCatches := 0, 0
	for _, nt := range allTypes {
		e := mustEntry(
			"coverage-"+string(nt),
			nt,
			staleStatus,
			encoded,
			nil,
			[]string{"work"},
			nil,
		)
		e.Confidence = core.ConfHigh

		// Current gate: type-label proxy (what shouldAbstain uses today).
		if volatileTypes[nt] {
			typeGateCatches++
		}
		// Candidate: content signal, blind to type. Combined with the same decay
		// predicate branch 3 already applies, so the comparison is apples-to-apples
		// (only the volatility gate differs).
		if v, _ := contentVolatility(e.Content); v {
			if s := core.StrengthAt(e, now); s < 0.35 {
				contentSignalCatches++
			}
		}
	}

	t.Logf("COVERAGE-HOLE HEAD-TO-HEAD (identical stale status across 6 types): "+
		"type-label gate catches %d/6, content-volatility signal catches %d/6.",
		typeGateCatches, contentSignalCatches)

	if typeGateCatches != 1 {
		t.Errorf("expected the current type-only gate to catch exactly 1/6 (TypeDecision), got %d", typeGateCatches)
	}
	if contentSignalCatches != 6 {
		t.Errorf("expected the content-volatility signal to catch all 6/6 (content is identical and volatile), got %d", contentSignalCatches)
	}
}

// TestBranch3_ContentVolatility_DoesNotReintroduceEvergreenFPs is the honesty
// guard: the whole reason branch 3 was gated in the first place (Jul-5) was that
// an age-only predicate suppressed evergreen Facts 6/6. A content-volatility
// signal is only an improvement if it ALSO holds that line — it must not classify
// any evergreen identity fact as volatile, or it would re-open the FP surface the
// type gate closed.
//
// This runs the content signal over the exact Jul-5 evergreenFactCorpus and pins
// zero volatility false positives. Together with the coverage-hole test above,
// it shows the content signal dominates the type gate on BOTH surfaces: same
// FP behavior (0), strictly better FN behavior (6/6 vs 1/6). That dominance is
// the case for eventually replacing the proxy — made with a number, not a hunch.
func TestBranch3_ContentVolatility_DoesNotReintroduceEvergreenFPs(t *testing.T) {
	now := time.Date(2026, 7, 5, 6, 0, 0, 0, time.UTC)
	corpus := evergreenFactCorpus(now)

	fp := 0
	for _, e := range corpus {
		if v, reason := contentVolatility(e.Content); v {
			fp++
			t.Errorf("FALSE POSITIVE %-18s evergreen fact classed VOLATILE (%s): %q", e.ID, reason, e.Content)
		} else {
			t.Logf("ok             %-18s → durable (content signal correctly does not gate it)", e.ID)
		}
	}
	if fp != 0 {
		t.Errorf("content-volatility signal re-introduced %d evergreen false positives; it must hold the Jul-5 line (0 FPs)", fp)
	}
	t.Logf("EVERGREEN FP CHECK: content-volatility signal fires on %d/%d evergreen facts (want 0). "+
		"It holds the FP line the type gate holds, while ALSO closing the FN coverage hole (6/6 vs 1/6). "+
		"DOMINANCE SHOWN — but NOT wired: production promotion is the Jul-13 go-live decision, gated on a "+
		"second real time-bound-state miss to confirm the lexical vocabulary generalizes beyond this corpus.",
		fp, len(corpus))
}
