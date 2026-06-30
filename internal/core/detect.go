package core

import (
	"regexp"
	"strings"
	"time"
)

// detect.go is the REFLEX side of the staleness loop — the sensor that turns a
// free-form line of conversation into a *candidate* correction.
//
// correction.go (Correct) is the capture chokepoint: given a structured
// Correction, it builds the canonical supersession triple. But Correct only
// fires if something first recognized "this utterance is a correction." That
// recognition is the missing live-validation layer. Without it the loop is
// inert: the primitive exists but nothing pulls the trigger, so STALE entries
// keep winning exactly as they did before any of this was built.
//
// DESIGN STANCE — detect, don't decide. DetectCorrection deliberately returns
// a Detection (a proposal), not a committed Correction, and never mutates
// anything. The neurological-grounding analysis (2026-06-23) is blunt about the
// stakes: written corrections are this architecture's ONLY update path, and
// there is no reconsolidation to walk a mistake back. A false-positive that
// auto-rewrites a true memory would therefore be uniquely costly — the bad
// edit becomes the new ground truth with no corrective pressure. So the sensor
// is intentionally a SUGGESTER: it scores its confidence, names the signals it
// fired on, and hands a ready-to-confirm proposal to a human (or a higher-
// confidence policy) who completes AsOf/NewLabel and calls Entry.Correct. The
// chokepoint stays the single committed path; this just feeds it.
//
// SCOPE — Phase-1 heuristic, not NLP. This is lexical signal matching over the
// real correction vocabulary observed in the live miss log ("actually…",
// "that's stale", "whose court", "never mind / not true"). It is intentionally
// small and explainable: every fired signal is reported, so a low-confidence
// or wrong detection is auditable rather than a black box. Semantic detection
// is a later (Stage 5+) concern, the same way specificity-aware ranking is.

// DetectionConfidence is the sensor's self-rated confidence that a span is a
// correction. It is distinct from core.Confidence (which is a property of a
// stored memory): this rates the *detection*, not the resulting entry.
type DetectionConfidence string

const (
	// DetectStrong: an explicit supersession marker fired ("actually it's now",
	// "that's stale/outdated", "scratch that"). High precision; safe to surface
	// as a likely correction.
	DetectStrong DetectionConfidence = "strong"

	// DetectWeak: a softer or ambiguous signal fired (a bare "actually", a
	// whose-court reframe cue) that often but not always marks a correction.
	// Surface for confirmation; do not act unattended.
	DetectWeak DetectionConfidence = "weak"

	// DetectNone: no correction signal. Not a correction.
	DetectNone DetectionConfidence = "none"
)

// Detection is the sensor's proposal: what it thinks it saw, why, and how sure
// it is. It carries everything Entry.Correct needs EXCEPT the caller-owned
// fields (AsOf — the capture instant — and NewLabel — the id slug source),
// which the confirming layer supplies. This split is deliberate: the sensor
// proposes the semantic content (kind + corrected statement); the operator owns
// when it was captured and how it is named.
type Detection struct {
	// IsCorrection is true iff Confidence != DetectNone.
	IsCorrection bool

	// Kind is the inferred CorrectionKind. Only meaningful when IsCorrection.
	Kind CorrectionKind

	// Confidence is the sensor's self-rating (strong/weak/none).
	Confidence DetectionConfidence

	// Signals lists the human-readable cues that fired, in priority order. This
	// is the audit trail: a detection is only as trustworthy as the signals it
	// names, and reporting them keeps the heuristic explainable.
	Signals []string

	// Content is the extracted corrected statement — the text AFTER the
	// correction marker ("actually, X is now Y" → "X is now Y"), trimmed and
	// normalized. It is the proposed Content for the resulting Correction. May
	// be empty if the marker fired but no trailing statement was found, in which
	// case the caller must supply Content before calling Correct.
	Content string
}

// ToCorrection assembles a Correction from this Detection plus the caller-owned
// capture fields, ready to feed Entry.Correct. It does NOT validate or apply —
// Correct does that. asOf is the capture instant; newLabel seeds the new
// entry's id slug. Inheritance fields (Type/Tags/About) are intentionally left
// zero so Correct inherits them from the superseded entry, which is almost
// always right for a correction. Confidence is left empty so Correct defaults
// it to ConfHigh (an operator-confirmed correction is high-confidence).
//
// content overrides the detected Content when non-empty (so the operator can
// clean up an imperfect extraction); otherwise the detected Content is used.
func (d Detection) ToCorrection(asOf time.Time, newLabel, content string) Correction {
	c := strings.TrimSpace(content)
	if c == "" {
		c = d.Content
	}
	return Correction{
		Kind:     d.Kind,
		Content:  c,
		NewLabel: newLabel,
		AsOf:     asOf,
	}
}

// A correction signal: a compiled marker plus the kind it implies and the
// confidence it carries. captureRe, when set, also extracts the corrected
// statement that follows the marker.
type correctionSignal struct {
	name       string
	re         *regexp.Regexp
	captureRe  *regexp.Regexp // optional: group 1 = the corrected statement
	kind       CorrectionKind
	confidence DetectionConfidence
}

// correctionSignals are evaluated in order; the FIRST strong match wins its
// kind/confidence, but ALL fired signal names are collected for the audit
// trail. Ordering puts the most specific / highest-precision markers first.
//
// The vocabulary is grounded in the real miss log, not invented:
//   - restate markers: explicit content-change cues ("actually it's now",
//     "scratch that", "that's stale/outdated/no longer", "update:").
//   - retract markers: withdrawal cues ("never mind", "that's wrong/not true",
//     "I was wrong", "ignore that").
//   - reframe markers: whose-court / interpretation cues ("actually the ball is
//     in X's court", "it's on X's side", "X owes", "that's on X not Y"). These
//     are weaker because the same words appear in non-correction discussion.
var correctionSignals = []correctionSignal{
	// --- RETRACT (check before restate: "that's not true" must not be eaten by
	// a generic "actually") ---
	{
		name:       "retract:withdrawal",
		re:         regexp.MustCompile(`(?i)\b(never ?mind|ignore (that|this|the last)|i was wrong|that('?s| is) (wrong|incorrect|not true|false)|disregard)\b`),
		captureRe:  regexp.MustCompile(`(?i)(?:i was wrong[,;:\s]+|that('?s| is) (?:wrong|incorrect|not true|false)[,;:\.\s]+)(.+)$`),
		kind:       CorrectRetract,
		confidence: DetectStrong,
	},

	// --- RESTATE (explicit content-change) ---
	{
		name:       "restate:actually-now",
		re:         regexp.MustCompile(`(?i)\bactually\b.*\b(it'?s now|it is now|now it'?s|that'?s now|is now)\b`),
		captureRe:  regexp.MustCompile(`(?i)\bactually[,:\s]+(.+)$`),
		kind:       CorrectRestate,
		confidence: DetectStrong,
	},
	{
		name:       "restate:stale-marker",
		re:         regexp.MustCompile(`(?i)\b((is|are|was|were|'?s)? ?(stale|outdated|out of date)|no longer (true|the case|current|accurate)|scratch that|correction[,:\s])\b`),
		captureRe:  regexp.MustCompile(`(?i)(?:scratch that|correction)[,:\.\s]+(.+)$`),
		kind:       CorrectRestate,
		confidence: DetectStrong,
	},
	{
		name:       "restate:update-prefix",
		re:         regexp.MustCompile(`(?i)^\s*update[,:\s]`),
		captureRe:  regexp.MustCompile(`(?i)^\s*update[,:\s]+(.+)$`),
		kind:       CorrectRestate,
		confidence: DetectWeak,
	},

	// --- RESTATE: still-affirmative (bare "still works/applies/..." with no
	// explicit supersession marker — the Granola-ban class of STALE miss where
	// the correction is a reaffirmation that the thing is still operative,
	// typically against a stored belief that it was banned/changed/removed).
	// Verb list is deliberately narrow: "still" is high-frequency; only
	// affirmative state-verbs create the correction-reaffirmation pattern.
	// no captureRe — the corrected statement spans the whole sentence;
	// the operator supplies Content from context when confirming.
	{
		name:       "restate:still-affirmative",
		re:         regexp.MustCompile(`(?i)\bstill\s+(works?|applies?|holds?|runs?|active|valid|live|enabled|available|correct)\b`),
		kind:       CorrectRestate,
		confidence: DetectWeak,
	},

	// --- RESTATE: scope-expansion ("does more than that now", "undersells") ---
	// Catches the LeanCTX class: a description "undersells" the current scope,
	// or "X does more than that now" signals the stored scope is too narrow.
	// Two distinct sub-patterns: (a) <subject> does/is/has more than
	// <that/before/previously>; (b) the bare verb "undersells".
	// no captureRe — the corrected description is always operator-supplied.
	{
		name:       "restate:scope-expansion",
		re:         regexp.MustCompile(`(?i)\b(does|is|has)\s+more\s+than\s+(that|before|previously|what\s+was)|\bundersells?\b`),
		kind:       CorrectRestate,
		confidence: DetectWeak,
	},

	// --- REFRAME (whose-court / interpretation; weaker by nature) ---
	{
		name:       "reframe:whose-court",
		re:         regexp.MustCompile(`(?i)\b(ball('?s| is)? in [a-z][\w'-]*'?s? court|(it'?s|is) on [a-z][\w'-]*'?s (side|court|plate)|[a-z][\w'-]*'?s? court not|that'?s on [a-z][\w'-]* not [a-z])\b`),
		kind:       CorrectReframe,
		confidence: DetectWeak,
	},
	{
		name:       "reframe:owes",
		re:         regexp.MustCompile(`(?i)\b([a-z][\w'-]* owes|owes (us|me|the)|waiting on [a-z][\w'-]*|punted to)\b`),
		kind:       CorrectReframe,
		confidence: DetectWeak,
	},

	// --- bare "actually" (weakest restate hint; only fires if nothing stronger
	// already classified the span) ---
	{
		name:       "restate:bare-actually",
		re:         regexp.MustCompile(`(?i)\bactually[,:\s]`),
		captureRe:  regexp.MustCompile(`(?i)\bactually[,:\s]+(.+)$`),
		kind:       CorrectRestate,
		confidence: DetectWeak,
	},
}

// confidenceRank orders detection confidence for "strongest wins" selection.
func confidenceRank(c DetectionConfidence) int {
	switch c {
	case DetectStrong:
		return 2
	case DetectWeak:
		return 1
	default:
		return 0
	}
}

// DetectCorrection scans a free-form line and returns a Detection proposing
// whether it is a correction, what kind, and the extracted corrected statement.
//
// Selection rule: the winning (kind, confidence) comes from the
// HIGHEST-CONFIDENCE fired signal; ties break by signal order (most specific
// first). ALL fired signal names are reported regardless of which won, so a
// detection that fired on several cues is fully auditable. The Content is
// extracted from the winning signal's captureRe when it has one; otherwise from
// the first fired signal that can extract, so a strong-but-non-extracting
// marker (e.g. a whose-court reframe) can still borrow a statement from a
// co-firing "actually" clause.
//
// It never panics and never mutates. Empty / whitespace input → DetectNone.
func DetectCorrection(text string) Detection {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Detection{Confidence: DetectNone}
	}

	var (
		fired    []string
		bestIdx  = -1
		bestRank = 0
		content  string
	)

	for i, sig := range correctionSignals {
		if !sig.re.MatchString(trimmed) {
			continue
		}
		fired = append(fired, sig.name)

		// Track the strongest classifier (first one wins a tie by order).
		if r := confidenceRank(sig.confidence); r > bestRank {
			bestRank = r
			bestIdx = i
		}

		// Opportunistically extract a corrected statement. Prefer the winning
		// signal's extraction, but accept the first available so a non-
		// extracting winner can still borrow content.
		if content == "" && sig.captureRe != nil {
			if m := sig.captureRe.FindStringSubmatch(trimmed); m != nil {
				content = strings.TrimSpace(m[len(m)-1])
			}
		}
	}

	if bestIdx < 0 {
		return Detection{Confidence: DetectNone}
	}

	win := correctionSignals[bestIdx]

	// If the winner itself can extract and produced something, prefer that over
	// a borrowed earlier extraction, so the content matches the classified kind.
	if win.captureRe != nil {
		if m := win.captureRe.FindStringSubmatch(trimmed); m != nil {
			if c := strings.TrimSpace(m[len(m)-1]); c != "" {
				content = c
			}
		}
	}

	return Detection{
		IsCorrection: true,
		Kind:         win.kind,
		Confidence:   win.confidence,
		Signals:      fired,
		Content:      stripTrailingPunct(content),
	}
}

// stripTrailingPunct trims a single trailing sentence terminator and surrounding
// quotes/space from an extracted statement, leaving the substance.
func stripTrailingPunct(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimRight(s, " .!")
	return strings.TrimSpace(s)
}
