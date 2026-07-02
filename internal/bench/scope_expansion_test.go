package bench

import (
	"regexp"
	"testing"

	"github.com/clockworksoul/enso/internal/core"
)

// scope_expansion_test.go is the MEASUREMENT for DROSS-TODO seam (0b): the
// LeanCTX H2 "scope-expansion" miss. It exists to pin down, executably, exactly
// what shape this miss is and exactly how far the current loop handles it —
// BEFORE any new detection signal is built (validate-before-construct).
//
// ─────────────────────────────────────────────────────────────────────────────
// THE PREMISE CORRECTION (recorded 2026-07-02, at the top, because it reframes
// the whole seam).
//
// Seam (0b) was opened on the belief that H2 is NOT caught by any path. That
// was true when it was written (Jun 29 probe: the Jun-23 vocabulary missed
// both held-out utterances). It is NO LONGER true. The Jun-30 seam-#0 recall
// extension added restate:scope-expansion, and the Jul-1 precision-hardening
// artifact-gated it. As of commit b13c875 the LEXICAL detector DOES fire on the
// real H2 utterance:
//
//	DetectCorrection("LeanCTX does more than that now, the note undersells its
//	                  current scope")
//	  => IsCorrection=true, Kind=restate, Confidence=weak,
//	     Signals=[restate:scope-expansion], Content=""
//
// held_out_test.go already asserts this (fired 2/2). So the honest statement of
// the remaining gap is NOT "H2 is undetected." It is the subtler one below.
//
// ─────────────────────────────────────────────────────────────────────────────
// THE REAL SHAPE — expansion, not negation; and empty-content is a DESIGN, not
// a bug, for the meta-only sub-form.
//
// The task framed scope-expansion as "stored belief X ⊂ true capability Y —
// the stored entry isn't WRONG, it's INCOMPLETE." That is exactly right, and it
// has a concrete consequence for where this class lives:
//
//   - It is NOT a CONTRADICTION. contradict.go fires only when a CURRENT stored
//     entry asserts a NEGATION (banned/removed/deprecated/disabled) that the
//     utterance affirms against. "LeanCTX is a narrow helper" asserts no
//     negation, so DetectContradiction correctly returns false (verified in
//     TestScopeExpansion_NotAContradiction below). Requiring the negation from
//     the stored side is what keeps the contradiction check precise; a
//     scope-expansion has no such stored negation to key on.
//
//   - It COLLAPSES INTO restate at the correction-KIND layer, and that is
//     correct. Once the tool's scope grows, the stored DESCRIPTION ("narrow
//     helper; limited scope") becomes operatively false — wrong-by-omission —
//     so the right mechanism is supersession: close the narrow-scope entry,
//     open a broader-scope one. The distinction "incomplete vs. wrong" lives at
//     the CONTENT-EXTRACTION layer, not the kind layer. The stored belief is
//     incomplete; the stored SENTENCE, treated as the whole truth, is wrong.
//
//   - The real H2 utterance is META-ONLY: it says THAT the stored scope is too
//     narrow ("does more than that now", "the note undersells its current
//     scope") but carries NO expansion payload — it does not enumerate what
//     LeanCTX now additionally does. So Content="" is CORRECT for this
//     utterance: there is nothing to extract, and the operator must supply the
//     fuller capability set. This is the SAME detect-don't-decide posture as the
//     reframe and contradiction classes, and for the same no-reconsolidation
//     reason: never auto-write invented content.
//
// So the loop already handles the real H2 miss as far as it safely can from the
// utterance alone: DETECT ✓ (weak restate), RESOLVE ✓ (the narrow-scope entry
// is the target), COMMIT-with-operator-content ✓ (the reframe end-to-end test
// already proves the empty-content-requires-operator invariant, which this
// class shares). There is no undetected-miss bug left here.
//
// ─────────────────────────────────────────────────────────────────────────────
// WHAT IS ACTUALLY OPEN — payload extraction for the PAYLOAD-BEARING sub-form,
// for which there is currently NO real corpus case (n=0).
//
// A DIFFERENT scope-expansion utterance DOES carry the payload:
// "LeanCTX also does X, Y, Z now." For those, the corrected content is present
// in the utterance and could be extracted, saving the operator a step. The
// probe below (TestScopeExpansion_PayloadExtractionSpec) shows a candidate
// "it also/now …" capture is PRECISE (0 false extractions on the innocent
// corpus) — but it fires ONLY when a payload clause exists, and the one real
// case we have (H2) has none. Building the extractor now would be constructing
// downstream of an n=0 validation: the trap Ensō's discipline names. So this
// test ENCODES the spec and the precision evidence and STOPS. When a real
// payload-bearing scope-expansion miss lands in the log, it becomes the
// regression target that earns the extractor.

// scopeStaleCandidates rebuilds the H2 stored-belief + correction target the
// same way held_out_cases.go does, so this measurement and the recall probe
// cannot drift. Returns (staleNarrowScopeEntry, correctedBroaderEntry).
func scopeStaleCandidates() Case {
	for _, c := range HeldOutStaleCases() {
		if c.Name == "leanctx-scope-stale" {
			return c
		}
	}
	panic("leanctx-scope-stale case not found in HeldOutStaleCases")
}

// TestScopeExpansion_DetectedAsRestateWithEmptyContent pins the CURRENT, correct
// handling of the real H2 utterance: it is detected as a weak restate, and its
// Content is empty by design (meta-only utterance carries no payload). This is
// the measurement anchor — if a future change either un-detects H2 or starts
// FABRICATING content for it, this fails.
func TestScopeExpansion_DetectedAsRestateWithEmptyContent(t *testing.T) {
	c := scopeStaleCandidates()

	d := core.DetectCorrection(c.Utterance)

	if !d.IsCorrection {
		t.Fatalf("H2 scope-expansion must be detected; got IsCorrection=false (signals=%v)", d.Signals)
	}
	if d.Kind != core.CorrectRestate {
		t.Errorf("H2 kind = %q, want restate (scope-expansion supersedes the stored description)", d.Kind)
	}
	if d.Confidence != core.DetectWeak {
		t.Errorf("H2 confidence = %q, want weak (inferred, not marker-explicit)", d.Confidence)
	}
	// The load-bearing invariant: a META-ONLY scope-expansion utterance carries
	// NO corrected content, so the detector must extract NOTHING. Empty content
	// here is CORRECT — the operator supplies the fuller capability set — not a
	// missing-extraction bug. Auto-writing invented scope would be permanent
	// corruption under no-reconsolidation.
	if d.Content != "" {
		t.Errorf("H2 Content = %q, want empty: a meta-only scope-expansion carries no payload to extract; "+
			"content must be operator-supplied", d.Content)
	}
	foundScopeSignal := false
	for _, s := range d.Signals {
		if s == "restate:scope-expansion" {
			foundScopeSignal = true
		}
	}
	if !foundScopeSignal {
		t.Errorf("expected restate:scope-expansion signal to fire; got %v", d.Signals)
	}
}

// TestScopeExpansion_NotAContradiction proves the class distinction executably:
// a scope-expansion is NOT a status-negation contradiction. The stored
// narrow-scope entry asserts no negation for the utterance to affirm against, so
// DetectContradiction correctly returns false. This is why seam #0's
// contradiction check does NOT and should NOT cover H2 — different shape.
func TestScopeExpansion_NotAContradiction(t *testing.T) {
	c := scopeStaleCandidates()

	// The stale narrow-scope entry is Candidates[0] (built open, then closed by
	// Correct); the current broader entry is Candidates[1]. We test contradiction
	// against BOTH the still-open-at-query stale entry and, for completeness, note
	// that the corrected entry carries no negation either.
	for _, cand := range c.Candidates {
		got := core.DetectContradiction(c.Utterance, cand, c.AsOf)
		if got.IsContradiction {
			t.Errorf("scope-expansion must NOT register as a contradiction against %q "+
				"(no stored negation to affirm against); got signals=%v",
				cand.Content, got.Signals)
		}
	}
}

// TestScopeExpansion_PayloadExtractionSpec is the VALIDATION-BEFORE-CONSTRUCT
// artifact for the open enhancement (payload extraction on the payload-bearing
// sub-form). It does not touch production code. It:
//
//	(1) demonstrates the current detector extracts nothing from either the real
//	    meta-only H2 (correct) or a synthetic payload-bearing utterance (the gap);
//	(2) validates that a candidate "it also/now …" capture regex is PRECISE —
//	    it extracts the payload from payload-bearing expansions and extracts
//	    NOTHING from the Jul-1 innocent corpus — so IF a real payload-bearing
//	    case ever lands, a safe extractor exists to build against.
//
// It deliberately does NOT wire this regex into core/detect.go: there is no real
// payload-bearing miss in the corpus (n=0), and adding a signal for an imagined
// case is the trap. This test IS the spec; the real case is the trigger.
func TestScopeExpansion_PayloadExtractionSpec(t *testing.T) {
	// (1) Current behavior: no payload extracted, either way.
	meta := "LeanCTX does more than that now, the note undersells its current scope"
	payload := "LeanCTX does more than that now — it also does full-context assembly and prompt caching"

	if d := core.DetectCorrection(meta); d.Content != "" {
		t.Errorf("meta-only H2 unexpectedly extracted content %q (should be empty)", d.Content)
	}
	if d := core.DetectCorrection(payload); d.Content != "" {
		t.Logf("NOTE: payload-bearing scope-expansion currently extracts %q via existing signals; "+
			"if non-empty this is a bonus, not a requirement", d.Content)
	} else {
		t.Logf("as expected: payload-bearing scope-expansion currently extracts NOTHING " +
			"(the open enhancement is to extract the 'it also …' clause)")
	}

	// (2) Candidate extractor precision check. This is the concrete spec the
	// future extractor must satisfy: extract the payload clause from real
	// expansions, extract nothing from innocent comparisons.
	//
	// Grounding: the clause "it (also|now|additionally) <capabilities>" is the
	// natural surface for an enumerated expansion. It requires the "it <adverb>"
	// lead so a bare comparison ("does more than the old one") does not match.
	candidate := regexp.MustCompile(`(?i)\bit (?:also|now|can also|additionally)\s+(.+)$`)

	type probe struct {
		text    string
		wantHit bool
	}
	probes := []probe{
		// payload-bearing expansions: MUST extract the capability clause.
		{"LeanCTX does more than that now — it also does full-context assembly and prompt caching", true},
		{"the note undersells its scope; it now also handles retrieval and ranking", true},
		// meta-only real H2: no payload clause, extracts nothing (and that's fine).
		{"LeanCTX does more than that now, the note undersells its current scope", false},
		// INNOCENT corpus (subset of the Jul-1 seam-#0 FP set + comparisons):
		// none may yield a payload extraction.
		{"This service does more than the old one, which is why it's slower.", false},
		{"The dashboard does more than that already, we're fine.", false},
		{"Honestly the README undersells how good this tool is.", false},
		{"We have more than enough time.", false},
		{"That coupon is still valid until Friday.", false},
	}

	for _, p := range probes {
		m := candidate.FindStringSubmatch(p.text)
		hit := m != nil
		if hit != p.wantHit {
			t.Errorf("payload-extractor precision: %q => hit=%v, want %v (extracted=%q)",
				p.text, hit, p.wantHit, submatch(m))
		}
	}
}

func submatch(m []string) string {
	if m == nil || len(m) < 2 {
		return ""
	}
	return m[1]
}
