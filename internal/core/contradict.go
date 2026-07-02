package core

import (
	"regexp"
	"strings"
	"time"
)

// contradict.go — SEAM #0 completion: the RESOLVER-SIDE contradiction check.
//
// # The gap this closes
//
// detect.go (DetectCorrection) is a LEXICAL sensor: it decides whether a line
// of conversation is a correction from the utterance text ALONE. The Jul 1
// precision-hardening session proved that a whole class of real STALE
// corrections cannot be caught that way without unacceptable false positives:
// a BARE corrective reaffirmation — "Granola still works", "we still use X",
// "that ban never took effect" — is genuinely ambiguous on the utterance alone.
// "X still works" is, out of context, an ordinary status remark; only knowledge
// of the CURRENTLY STORED BELIEF distinguishes a reaffirmation-against-a-stale-
// note from a throwaway status update. So those signals were DELIBERATELY left
// below the lexical detector's reach (see the restate:still-affirmative /
// restate:scope-expansion contrast-gates in detect.go).
//
// The rightful home for that class is HERE, on the resolver side, where we DO
// know the stored corpus. The question this file answers is not "does this
// utterance look like a correction?" but the sharper, corpus-relative one:
//
//	Does this utterance AFFIRM the operative status of something that a CURRENT
//	stored entry asserts is banned / removed / deprecated / disabled?
//
// That is a CONTRADICTION between the utterance and a held belief — and a
// contradiction against a current entry is exactly a STALE-correction signal
// that no marker appeared for. The Granola-ban H1 miss is the canonical case:
// the utterance "Granola still works" contradicts the stored "Granola is
// banned per Yext policy," and that contradiction — not any lexical marker — is
// what should surface the correction.
//
// # DESIGN STANCE — detect, don't decide (unchanged from detect.go)
//
// This is still a SUGGESTER, never an applier. It returns a Contradiction
// proposal (which stored entry, why, how sure) and NEVER mutates anything. The
// no-reconsolidation rule (2026-06-23 neurological-grounding analysis) is
// unchanged: a false-positive that auto-rewrites a true memory is permanent
// corruption, so a human (or a higher-confidence policy) completes the loop by
// calling Entry.Correct. The contradiction check only *finds a target the bare
// lexical detector could not*; the committed path stays the single chokepoint.
//
// # WHY IT LIVES IN CORE, not in confirm
//
// This is pure text-vs-text domain logic (does an affirmation of subject S
// contradict a stored negation of subject S?). It has no I/O and no storage
// dependency — it takes an utterance and an already-loaded candidate Entry. The
// resolver (confirm.TargetResolver, an adapter-ish seam) will CALL this over its
// candidate set, but the contradiction judgment itself is domain math and
// belongs with DetectCorrection in core, reusing the same Tokenize primitive.
//
// # PRECISION DISCIPLINE (the Jul 1 lesson, applied up front)
//
// A contradiction requires THREE independent pieces of evidence, all of which
// must hold; any one alone is not enough:
//
//  1. The UTTERANCE affirms operative status ("still works", "still the source
//     of record", "is fine", "never took effect", "works again").
//  2. The stored CANDIDATE asserts a negative/removal status ("banned",
//     "blocked", "removed", "deprecated", "disabled", "no longer …").
//  3. They share SUBJECT tokens (the affirmation and the negation are about the
//     SAME thing), measured with the shared Tokenize primitive.
//
// Requiring the negative status to come from the STORED entry (not the
// utterance) is what makes this precise where the lexical detector cannot be:
// "the staging link is still live" has no stored "staging link is removed" note
// to contradict, so it does not fire. The corpus supplies half the evidence.

// negationStatusRe matches a stored entry that asserts a subject is NOT
// operative: it was banned, blocked, removed, deprecated, disabled, killed,
// dropped, retired, sunset, or "no longer <x>". This is the vocabulary a STALE
// negative-status belief is written in (grounded in the real Granola-ban note:
// "Granola is banned per Yext policy"). Word-boundaried to avoid substring
// accidents ("disabled" must not match inside a larger token).
var negationStatusRe = regexp.MustCompile(`(?i)\b(banned|blocked|removed|deprecated|disabled|discontinued|killed|dropped|retired|sunset|forbidden|prohibited|off-?limits|no longer (available|allowed|permitted|supported|used|active|in use|the case)|not (allowed|permitted|available|supported))\b`)

// affirmationStatusRe matches an utterance that affirms a subject IS operative,
// standing against a prior negation. Two shapes:
//   - a "still / again / fine" operative affirmation ("still works", "works
//     again", "is fine", "still the source of record", "still active");
//   - an explicit denial that the negation took effect ("the ban never took
//     effect", "was never banned", "isn't blocked", "not deprecated").
//
// This is intentionally BROADER than the lexical detector's contrast-gated
// restate:still-affirmative signal, precisely because here the STORED negation
// supplies the contrast the utterance is allowed to lack. A bare "still works"
// is fine to match at THIS layer because it only fires when a contradicting
// stored belief exists.
var affirmationStatusRe = regexp.MustCompile(`(?i)\b(still\s+(works?|working|applies|apply|holds?|runs?|running|active|valid|live|enabled|available|used|in use|the\s+\w+)|works?\s+again|working\s+again|back\s+(up|online|in use)|is\s+fine|are\s+fine|(never|not)\s+(banned|blocked|removed|deprecated|disabled|forbidden|prohibited)|ban\s+never\s+(took effect|happened|landed|applied)|(isn'?t|aren'?t|is not|are not)\s+(banned|blocked|removed|deprecated|disabled))\b`)

// Contradiction is the resolver-side sensor's proposal that an utterance
// contradicts a specific stored entry: the utterance affirms the operative
// status of a subject the entry says is defunct. Like Detection it is a pure
// value — building one writes nothing — and it names its evidence so the
// judgment is auditable, not a black box.
type Contradiction struct {
	// IsContradiction is true iff all three evidence conditions held.
	IsContradiction bool

	// Confidence rates the contradiction detection. It reuses
	// DetectionConfidence so a downstream consumer can treat contradiction and
	// lexical detection on the same scale. A contradiction is at most DetectWeak
	// by policy: it is inferred from corpus disagreement, never from an explicit
	// supersession marker, so it must be confirmed before it commits.
	Confidence DetectionConfidence

	// Signals lists the human-readable cues that fired (the affirmation phrase,
	// the stored negation phrase, and the overlapping subject term). This is the
	// audit trail — a contradiction is only as trustworthy as the evidence it
	// can name.
	Signals []string

	// SubjectTerms are the tokens shared between the utterance and the stored
	// entry that establish they are about the same thing. Reported so a human
	// can sanity-check the match ("granola", not a coincidental stopword).
	SubjectTerms []string
}

// ToDetection adapts a Contradiction into the Detection shape that the rest of
// the loop (Detection.ToCorrection → Entry.Correct) already consumes, so a
// resolver-side contradiction feeds the SAME committed chokepoint as a lexical
// detection with no parallel code path.
//
// It is always a CorrectRestate (a contradiction says the stored status is no
// longer true — a content change, not a whose-court reframe). Content is left
// EMPTY on purpose: a contradiction establishes THAT the stored belief is stale
// but not the full corrected statement, so — exactly like the reframe class —
// the operator must supply the new content before Correct will accept it. This
// keeps the riskiest inference (auto-writing invented content) barred from the
// unattended path, consistent with detect-don't-decide.
func (c Contradiction) ToDetection() Detection {
	return Detection{
		IsCorrection: c.IsContradiction,
		Kind:         CorrectRestate,
		Confidence:   c.Confidence,
		Signals:      c.Signals,
		Content:      "", // operator-supplied; a contradiction carries no corrected statement
	}
}

// DetectContradiction reports whether `utterance` contradicts the stored
// `candidate` entry: the utterance affirms operative status, the candidate
// asserts a negative/removal status, and the two share subject tokens. It is
// the resolver-side companion to DetectCorrection for the bare-reaffirmation
// STALE class the lexical detector deliberately cannot catch.
//
// All three conditions are required. Missing any one → not a contradiction
// (returns a zero-ish Contradiction with DetectNone). It never panics and never
// mutates. Empty utterance or a non-current candidate → not a contradiction
// (you cannot contradict a belief that is not currently held).
func DetectContradiction(utterance string, candidate Entry, now time.Time) Contradiction {
	u := strings.TrimSpace(utterance)
	if u == "" {
		return Contradiction{Confidence: DetectNone}
	}
	// Only a CURRENTLY-HELD belief can be contradicted. An already-superseded
	// entry is not the stored ground truth, so affirming against it is not a
	// correction signal — it may even be re-affirming the correct new state.
	if !candidate.IsCurrent(now) {
		return Contradiction{Confidence: DetectNone}
	}

	// (1) utterance affirms operative status.
	affirm := affirmationStatusRe.FindString(u)
	if affirm == "" {
		return Contradiction{Confidence: DetectNone}
	}
	// (2) stored candidate asserts a negative/removal status.
	neg := negationStatusRe.FindString(candidate.Content)
	if neg == "" {
		return Contradiction{Confidence: DetectNone}
	}
	// (3) they are about the same subject: shared content-bearing tokens.
	subjects := sharedSubjectTerms(u, candidate)
	if len(subjects) == 0 {
		return Contradiction{Confidence: DetectNone}
	}

	signals := []string{
		"contradict:affirm[" + normalizeSpace(affirm) + "]",
		"contradict:stored-negation[" + normalizeSpace(neg) + "]",
		"contradict:subject[" + strings.Join(subjects, ",") + "]",
	}
	return Contradiction{
		IsContradiction: true,
		Confidence:      DetectWeak, // inferred from corpus disagreement — confirm before commit
		Signals:         signals,
		SubjectTerms:    subjects,
	}
}

// sharedSubjectTerms returns the content-bearing tokens that appear in BOTH the
// utterance and the candidate's subject surface (Tags + About + Content),
// established with the shared Tokenize primitive so the utterance and entry
// tokenize identically. This is the "same thing" evidence: an affirmation and a
// stored negation only contradict if they are about the same subject.
//
// The candidate's Tags/About are the strongest subject signal (the entry is
// curated to be *about* those), but we also allow a Content-token match so a
// subject named only in free text ("Granola is banned…") still overlaps a
// bare-subject utterance ("Granola still works"). Order follows the utterance
// so the reported subjects read naturally; duplicates are dropped.
func sharedSubjectTerms(utterance string, candidate Entry) []string {
	cand := map[string]bool{}
	for _, t := range candidate.Tags {
		for _, tok := range Tokenize(t) {
			cand[tok] = true
		}
	}
	for _, a := range candidate.About {
		for _, tok := range Tokenize(a) {
			cand[tok] = true
		}
	}
	for _, tok := range Tokenize(candidate.Content) {
		cand[tok] = true
	}

	seen := map[string]bool{}
	var out []string
	for _, tok := range Tokenize(utterance) {
		if !cand[tok] || seen[tok] {
			continue
		}
		// Reject pure operative-status words as "subjects": "still", "works",
		// "active" etc. co-occur in both sides but are not what the belief is
		// ABOUT. Only non-status tokens count toward same-subject evidence.
		if statusWords[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// statusWords are operative-status tokens that must NOT count as subject
// evidence: they legitimately appear on both the affirmation and (sometimes)
// the stored side, but a match on them proves nothing about same-subject. Kept
// tight and grounded in the two status regexes above (YAGNI — expand only when
// a real case shows a status word leaking through as a false subject).
var statusWords = map[string]bool{
	"still": true, "works": true, "work": true, "working": true,
	"active": true, "valid": true, "live": true, "enabled": true,
	"available": true, "used": true, "use": true, "fine": true,
	"again": true, "back": true, "online": true, "runs": true,
	"running": true, "applies": true, "holds": true, "hold": true,
	"banned": true, "blocked": true, "removed": true, "deprecated": true,
	"disabled": true, "forbidden": true, "prohibited": true,
	"never": true, "not": true, "no": true, "longer": true,
	"source": true, "record": true, "canonical": true, "default": true,
}

// normalizeSpace collapses internal whitespace runs to single spaces and trims,
// so a signal label is stable regardless of how the matched span was spaced.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
