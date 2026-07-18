package bench

import (
	"os"
	"regexp"
	"testing"
)

// TestWP3SameDayCaptureBarStratification characterizes the same-day
// supersession pairs (stale_date == current_date) by EDGE-FREE detectability.
//
// Background: the Jul-14/15 WP-3 gate docs treated the same-day flips as one
// irreducible block — same LastRefTime ⇒ decay powerless ⇒ only the SUPERSEDES
// edge (capture) can break the tie. That is true about DECAY. It is NOT the
// whole story about CAPTURE: the same-day pairs are heterogeneous, and most of
// them carry an edge-free lexical signal a resolver-side nomination could use.
//
// This test pins the distribution so a future corpus change that shifts the
// genuinely-irreducible floor gets noticed. The classifier is deliberately
// crude (the point is the shape of the distribution, not a shippable detector);
// it is bench-only and consumes no production code.
//
// Tiers (ordered, first-match-wins):
//
//	A status-completion  — current gains a completion marker (✅/done/merged/…)
//	B explicit-retraction — current names the reversal (retired/postponed from/…)
//	C scalar-flip        — a changed number/date/day/path token
//	D reword/rescope      — equally-specific prose, no lexical cue → EDGE-ONLY
//
// Load-bearing assertion: the D (genuinely irreducible) floor is SMALL. The
// capture layer's minimal job is tiers A+B+C, not "all same-day flips".
func TestWP3SameDayCaptureBarStratification(t *testing.T) {
	if _, err := os.Stat(corpusPath); os.IsNotExist(err) {
		t.Skipf("corpus not found at %s — run cmd/corpus-builder first", corpusPath)
	}
	records, err := LoadGitHistoryRecords(corpusPath)
	if err != nil {
		t.Fatalf("load records: %v", err)
	}

	var sameDay []GitHistoryRecord
	for _, r := range records {
		if r.StaleDate == r.CurrentDate {
			sameDay = append(sameDay, r)
		}
	}

	// Correction to the record: the earlier WP-3 docs say 27 same-day pairs.
	// The JSONL actually has 29 (27 need the edge; 2 are already won by
	// specificity — the Adam/Ed out-specifies-stale shape). Pin 29 so the
	// discrepancy stays reconciled.
	const wantSameDay = 29
	if len(sameDay) != wantSameDay {
		t.Errorf("same-day pair count = %d, want %d "+
			"(if the corpus changed, re-run the stratification probe and "+
			"update docs/2026-07-18-wp3-sameday-capture-bar-stratification.md)",
			len(sameDay), wantSameDay)
	}

	doneRe := regexp.MustCompile(`(?i)✅|complete|\bdone\b|merged|unblocked|\bselected\b|\bresolved\b`)
	retractRe := regexp.MustCompile(`(?i)retired|postponed from|previous .*rule|no longer|instead of|\breplaced\b|superseded`)
	scalarRe := regexp.MustCompile(`\d+\.?\d+%|Mon|Tue|Wed|Thu|Fri|\b\d{1,4}\b|[\w/.-]+\.(?:go|md)|[\w-]+/[\w/-]+`)

	countMatches := func(re *regexp.Regexp, s string) int { return len(re.FindAllString(s, -1)) }

	classify := func(r GitHistoryRecord) string {
		s, c := r.StaleText, r.CurrentText
		if retractRe.MatchString(c) && !retractRe.MatchString(s) {
			return "B_explicit_retraction"
		}
		if countMatches(doneRe, c) > countMatches(doneRe, s) {
			return "A_status_completion"
		}
		// scalar flip: the set of scalar tokens differs between versions
		if diffSets(scalarRe.FindAllString(s, -1), scalarRe.FindAllString(c, -1)) {
			return "C_scalar_flip"
		}
		return "D_reword_rescope"
	}

	tiers := map[string]int{}
	for _, r := range sameDay {
		tiers[classify(r)]++
	}

	t.Logf("same-day stratification (n=%d): A=%d B=%d C=%d D=%d",
		len(sameDay),
		tiers["A_status_completion"], tiers["B_explicit_retraction"],
		tiers["C_scalar_flip"], tiers["D_reword_rescope"])

	// The irreducible (edge-only) floor must stay small. The whole point of the
	// finding is that same-day capture is NOT a monolithic 27/29 problem — the
	// signal-bearing majority (A+B+C) is reachable by a scoped resolver-side
	// nomination, and only a handful of pure prose-rewordings are edge-only.
	// If the corpus grows and this floor balloons, the capture-layer scope
	// analysis in the Jul-18 doc must be revisited.
	const maxIrreducible = 8
	if d := tiers["D_reword_rescope"]; d > maxIrreducible {
		t.Errorf("irreducible (edge-only) same-day floor = %d, want ≤ %d — "+
			"same-day capture may no longer be dominated by signal-bearing "+
			"shapes; revisit the Jul-18 stratification doc", d, maxIrreducible)
	}

	// The signal-bearing majority claim: A+B+C should be the clear majority.
	signalBearing := tiers["A_status_completion"] + tiers["B_explicit_retraction"] + tiers["C_scalar_flip"]
	if signalBearing <= len(sameDay)/2 {
		t.Errorf("edge-free-signal-bearing same-day pairs = %d of %d — "+
			"expected a clear majority (the capture layer's minimal job is "+
			"narrower than 'all same-day flips')", signalBearing, len(sameDay))
	}
}

// diffSets reports whether two token slices represent different sets.
func diffSets(a, b []string) bool {
	set := func(xs []string) map[string]struct{} {
		m := make(map[string]struct{}, len(xs))
		for _, x := range xs {
			m[x] = struct{}{}
		}
		return m
	}
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		return true
	}
	for k := range sa {
		if _, ok := sb[k]; !ok {
			return true
		}
	}
	return false
}
