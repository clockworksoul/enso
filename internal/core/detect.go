package core

import (
	"regexp"
	"strings"
)

// detect.go is the REFLEX side of the staleness loop — the sensor that turns a
// free-form line of conversation into a *candidate* correction.
//
// RESTORED 2026-07-18 (WP-6, ADR-001 corollary b′ / RH-9): this file was
// deleted in `cd8e1a2` with the rest of the detection layer and returns by the
// prescribed route — real misses first (the four preserved correction
// utterances on the real-miss cases), restoration second, after WP-4's gate
// closed. It is the post-Jul-1 PRECISION-HARDENED version, restored verbatim
// where possible; only the plumbing changed: the deleted `Correction` /
// `Entry.Correct` chokepoint does NOT return. The surviving supersession
// ceremony (`NewEntry` + `Entry.Supersede`, executed via a Store) is the only
// committed path, and `ProposeSupersession` (propose.go) is how a Detection
// reaches it.
//
// The supersession ceremony is the capture chokepoint: it builds the canonical
// triple. But it only fires if something first recognized "this utterance is a
// correction." That recognition is this layer. Without it the loop is inert:
// the primitive exists but nothing pulls the trigger, so STALE entries keep
// winning exactly as they did before any of this was built.
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
// confidence policy) who authors the corrected entry and executes the
// supersession ceremony. The ceremony stays the single committed path; this
// just feeds it.
//
// CorrectionKind classifies *why* an entry is being superseded. It does not
// change the mechanics (every kind produces the same supersession triple); it
// is provenance, preserved so the corpus and any later audit can see what sort
// of update happened. The taxonomy mirrors the live miss classes that motivate
// capture. (Restored from the deleted correction.go — the kinds survive; the
// Correction struct does not.)
type CorrectionKind string

const (
	// CorrectRestate: the fact's *content* changed — the old statement is no
	// longer true and a new value replaces it (e.g. "headcount ask is overdue"
	// → "headcount ask landed at the Jun 18 1:1"). The classic STALE fix.
	CorrectRestate CorrectionKind = "restate"

	// CorrectReframe: the underlying facts did not change but the *interpretation*
	// was wrong — typically ownership / whose-court / who-owes-whom (e.g. "ball
	// is on Matt's side" → "open dependency is on Ed's side"). Same facts,
	// corrected frame. Distinguished from Restate because it is the subtle miss
	// class where recency is most dangerous: nothing looked obviously outdated.
	CorrectReframe CorrectionKind = "reframe"

	// CorrectRetract: the old statement was simply wrong / never true and is
	// withdrawn, with the new entry recording the corrected understanding (which
	// may be "this was a misconception"). Mechanically identical; provenance
	// distinct so retractions are auditable.
	CorrectRetract CorrectionKind = "retract"
)

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
// it is. It carries the semantic half of a correction (kind + extracted
// corrected statement); the confirming layer owns the rest — the capture
// instant, the new entry's id, and the decision to execute the supersession
// ceremony at all. This split is deliberate: the sensor proposes, the operator
// commits.
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
	// normalized. It is the proposed Content for the corrected entry. May be
	// empty if the marker fired but no trailing statement was found, in which
	// case the operator supplies the content when confirming.
	Content string
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
		// "correction:" / "correction," as a sentence-initial label. Kept
		// separate from restate:stale-marker because the trailing \b in that
		// signal fires AFTER a non-word character (colon/space), which makes
		// \b fail: \b after punctuation needs a word-char next, but the space
		// after 'correction: ' satisfies neither side of a word boundary. A
		// sentence-initial anchor (^\s*) sidesteps the \b issue entirely and
		// also correctly restricts the signal to the lead-marker case, where
		// it is unambiguous (a mid-sentence 'correction:' is rarer and can
		// share the stale-marker path once this fires first by strength).
		name:       "restate:correction-prefix",
		re:         regexp.MustCompile(`(?i)^\s*correction[,:\s]`),
		captureRe:  regexp.MustCompile(`(?i)^\s*correction[,:\s]+(.+)$`),
		kind:       CorrectRestate,
		confidence: DetectStrong,
	},
	{
		name:       "restate:stale-marker",
		re:         regexp.MustCompile(`(?i)\b((is|are|was|were|'?s)? ?(stale|outdated|out of date)|no longer (true|the case|current|accurate)|scratch that)\b`),
		captureRe:  regexp.MustCompile(`(?i)(?:scratch that)[,:\.\s]+(.+)$`),
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
	// "let me update/correct/revise that" — a spoken correction preamble that
	// names the corrective act before stating the fix. Weak because the same
	// phrase appears in ordinary editing intent ("let me update that doc");
	// the resolver will confirm whether a live stale entry exists to act on.
	{
		name:       "restate:let-me-update",
		re:         regexp.MustCompile(`(?i)\blet me (update|correct|revise|amend) that\b`),
		captureRe:  regexp.MustCompile(`(?i)\blet me (?:update|correct|revise|amend) that[,:\s]+(.+)$`),
		kind:       CorrectRestate,
		confidence: DetectWeak,
	},
	// "revised X: ..." / "revised number:" as a sentence-initial label, same
	// anchor trick as restate:correction-prefix.
	{
		name:       "restate:revised-prefix",
		re:         regexp.MustCompile(`(?i)^\s*revised\b`),
		captureRe:  regexp.MustCompile(`(?i)^\s*revised\b[^:]*:\s*(.+)$`),
		kind:       CorrectRestate,
		confidence: DetectWeak,
	},
	// "I misspoke" — an explicit self-correction of a prior statement.
	{
		name:       "retract:misspoke",
		re:         regexp.MustCompile(`(?i)\bi (misspoke|mis-spoke|said that wrong|stated that incorrectly)\b`),
		captureRe:  regexp.MustCompile(`(?i)\bi (?:misspoke|mis-spoke|said that wrong|stated that incorrectly)[,;:\s—]+(.+)$`),
		kind:       CorrectRetract,
		confidence: DetectStrong,
	},

	// --- RESTATE: still-affirmative, CONTRAST-GATED (the Granola-ban class of
	// STALE miss: a reaffirmation that a thing is still operative, standing
	// AGAINST a stored belief that it was banned/removed/deprecated).
	//
	// PRECISION LESSON (Jul 1 Dross Hour). The original Jun-30 form was a bare
	// `\bstill\s+(works?|applies?|...)\b`. An FP probe over 18 innocent status
	// sentences ("the staging link is still live", "that coupon is still valid",
	// "the API key still works") found it fired on 13/18 — a false-positive rate
	// that is unacceptable under this architecture's no-reconsolidation rule
	// (a wrong auto-surfaced correction is uniquely costly). A bare
	// "X still works" is genuinely ambiguous on the utterance ALONE: only the
	// resolver, which knows the currently-stored belief, can tell a
	// reaffirmation-against-a-stale-note from an ordinary status remark.
	//
	// So the LEXICAL detector now fires ONLY when the utterance itself carries a
	// contrastive or canonical-status cue that betrays it is pushing back on a
	// prior claim: an explicit contrast conjunction ("despite", "even though"),
	// a canonical-status assertion ("source of record", "still the canonical"),
	// or an explicit "no longer/not banned|blocked|removed|deprecated". Bare
	// "X still works" with no such cue is DELIBERATELY left below the detector's
	// reach — catching it is the resolver's job (does this utterance contradict
	// a current entry?), not the sensor's. Recall on the real H1 utterance is
	// preserved because it says "...is the transcript source of record".
	// no captureRe — the corrected statement is operator-supplied from context.
	{
		name:       "restate:still-affirmative",
		re:         regexp.MustCompile(`(?i)\bstill\s+(works?|applies?|holds?|runs?|active|valid|live|enabled|available|correct|the\s+(source|canonical|default|standard))\b.*\b(despite|even though|source of record|canonical|no longer (banned|blocked|removed|deprecated)|not (banned|blocked|removed|deprecated))\b|\b(despite|even though)\b.*\bstill\s+(works?|applies?|holds?|runs?|active|valid|live|enabled|available|correct)\b`),
		kind:       CorrectRestate,
		confidence: DetectWeak,
	},

	// --- RESTATE: scope-expansion, ARTIFACT-GATED (the LeanCTX class: a stored
	// note/description "undersells" the current scope, or the tool "does more
	// than that now").
	//
	// PRECISION LESSON (Jul 1 Dross Hour). The Jun-30 form `\bundersells?\b`
	// (or a bare "does more than that") also over-fired: "the README undersells
	// how good this tool is" and "the dashboard does more than that already" are
	// ordinary remarks, not corrections of a stored note. The fix requires the
	// undersells/understates verb to (a) name a stored ARTIFACT
	// (note/doc/description/record/memory/entry/readme) AND (b) target that
	// artifact's SCOPE/CAPABILITY/CURRENCY — i.e. the note is wrong about what
	// the thing DOES, which is the correction — not a subjective quality ("how
	// good"). The "does/is/has more than that" branch likewise requires a
	// note/now/current anchor so it reads as a stored-scope correction, not a
	// throwaway comparison. This took the combined seam-#0 FP rate from 13/18
	// to 0/18 on the probe while keeping the real H2 utterance a hit.
	// no captureRe — the corrected description is always operator-supplied.
	{
		name:       "restate:scope-expansion",
		re:         regexp.MustCompile(`(?i)\b(note|doc|description|record|memory|entry|readme)\b[^.]*\b(undersells?|understates?|too narrow|out of date|outdated)\b[^.]*\b(scope|capabilit\w+|current|coverage|range|breadth|what it (does|can))\b|\b(does|is|has)\s+more\s+than\s+(that|before|previously|what\s+was)\b[^.]*\b(note|now|current)\b`),
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
