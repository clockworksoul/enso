package core

import (
	"strings"
	"testing"
	"time"
)

// hasSignal reports whether name is among the fired signals.
func hasSignal(sigs []string, name string) bool {
	for _, s := range sigs {
		if s == name {
			return true
		}
	}
	return false
}

// TestDetectCorrection_Table exercises the sensor over a corpus mixing real
// correction vocabulary (drawn from the live miss log) with adversarial
// non-corrections that share surface words. Each row asserts the classification
// and confidence; content extraction is checked separately where it matters.
func TestDetectCorrection_Table(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantIs   bool
		wantKind CorrectionKind
		wantConf DetectionConfidence
	}{
		// --- RESTATE (explicit content change) ---
		{
			name:     "actually-its-now",
			text:     "Actually, the Adam headcount ask is now resolved — it landed at the Jun 18 1:1.",
			wantIs:   true,
			wantKind: CorrectRestate,
			wantConf: DetectStrong,
		},
		{
			name:     "stale-marker",
			text:     "That Jun 16 TODO line is stale; the ask already went through.",
			wantIs:   true,
			wantKind: CorrectRestate,
			wantConf: DetectStrong,
		},
		{
			name:     "no-longer-true",
			text:     "That's no longer true, the rollout finished last week.",
			wantIs:   true,
			wantKind: CorrectRestate,
			wantConf: DetectStrong,
		},
		{
			name:     "scratch-that",
			text:     "Scratch that: the meeting moved to Thursday.",
			wantIs:   true,
			wantKind: CorrectRestate,
			wantConf: DetectStrong,
		},
		{
			name:     "update-prefix",
			text:     "Update: PLR2 migration is now org-wide.",
			wantIs:   true,
			wantKind: CorrectRestate,
			wantConf: DetectWeak,
		},
		{
			name:     "bare-actually",
			text:     "Actually I think we should reconsider the schema.",
			wantIs:   true,
			wantKind: CorrectRestate,
			wantConf: DetectWeak,
		},

		// --- RETRACT (withdrawal) ---
		{
			name:     "never-mind",
			text:     "Never mind, that ticket was already closed.",
			wantIs:   true,
			wantKind: CorrectRetract,
			wantConf: DetectStrong,
		},
		{
			name:     "that-is-wrong",
			text:     "That's wrong, Tipa is remote this week not next.",
			wantIs:   true,
			wantKind: CorrectRetract,
			wantConf: DetectStrong,
		},
		{
			name:     "i-was-wrong",
			text:     "I was wrong, the cron pins its own model.",
			wantIs:   true,
			wantKind: CorrectRetract,
			wantConf: DetectStrong,
		},

		// --- REFRAME (whose-court / interpretation) ---
		{
			// Bare "actually" is only a weak restate hint; the whose-court reframe
			// cue (listed earlier, same weak rank) is the honest classification.
			name:     "ball-in-court",
			text:     "Actually the ball is in Ed's court, not mine.",
			wantIs:   true,
			wantKind: CorrectReframe,
			wantConf: DetectWeak,
		},
		{
			name:     "its-on-his-side",
			text:     "The open dependency is on Ed's side now.",
			wantIs:   true,
			wantKind: CorrectReframe,
			wantConf: DetectWeak,
		},
		{
			name:     "ed-owes",
			text:     "Ed owes us the guest-post submission terms.",
			wantIs:   true,
			wantKind: CorrectReframe,
			wantConf: DetectWeak,
		},

		// --- NON-corrections (must NOT fire) ---
		{
			name:   "plain-statement",
			text:   "The deploy went out at 3pm and looks healthy.",
			wantIs: false,
		},
		{
			name:   "empty",
			text:   "",
			wantIs: false,
		},
		{
			name:   "whitespace",
			text:   "   \n\t  ",
			wantIs: false,
		},
		{
			name:   "question-not-correction",
			text:   "Whose court is the Neo4j blog post in?",
			wantIs: false,
		},
		{
			name:   "actually-as-adverb-midword",
			text:   "The factually correct number is 42.", // 'factually' must not match \bactually\b
			wantIs: false,
		},

		// --- seam #0 regression: bare corrective assertions (held-out H1 + H2) ---
		// These are the real Jun-25 STALE utterances that fired=false on the
		// Jun-23 vocabulary. Seam #0 (Jun 30 Dross Hour) added the two signals
		// that catch them. They are now pinned as regressions so any future
		// detector edit cannot silently re-break these classes.
		{
			// H1: Granola-ban STALE. The correction is a bare reaffirmation:
			// the thing "still works", implying the stored ban/removal is stale.
			// No "actually", no "stale", no "scratch that" — just "still works".
			name:     "still-works-bare-reaffirmation",
			text:     "Granola still works and is the transcript source of record",
			wantIs:   true,
			wantConf: DetectWeak,
			wantKind: CorrectRestate,
		},
		{
			// H2: LeanCTX scope STALE. The correction is a scope-expansion:
			// "does more than that now" + "undersells". No explicit marker.
			name:     "scope-expansion-undersells",
			text:     "LeanCTX does more than that now, the note undersells its current scope",
			wantIs:   true,
			wantConf: DetectWeak,
			wantKind: CorrectRestate,
		},

		// --- adversarials for the two new signals ---
		// "still" must not fire on non-affirmative-state verbs.
		{
			name:   "still-need-not-correction",
			text:   "I still need to get around to that refactor.",
			wantIs: false,
		},
		// "more than" without an (does|is|has) subject must not fire.
		{
			name:   "more-than-no-subject",
			text:   "That bug affects more than one service.",
			wantIs: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectCorrection(tc.text)
			if got.IsCorrection != tc.wantIs {
				t.Fatalf("IsCorrection = %v, want %v (signals=%v)", got.IsCorrection, tc.wantIs, got.Signals)
			}
			if !tc.wantIs {
				if got.Confidence != DetectNone {
					t.Errorf("non-correction got confidence %q, want none", got.Confidence)
				}
				return
			}
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q (signals=%v)", got.Kind, tc.wantKind, got.Signals)
			}
			if got.Confidence != tc.wantConf {
				t.Errorf("Confidence = %q, want %q (signals=%v)", got.Confidence, tc.wantConf, got.Signals)
			}
			if len(got.Signals) == 0 {
				t.Errorf("a correction must report at least one signal")
			}
		})
	}
}

// TestDetectCorrection_ExtractsContent verifies the corrected statement is
// pulled out after the marker and normalized (trailing punctuation/quotes gone).
func TestDetectCorrection_ExtractsContent(t *testing.T) {
	d := DetectCorrection("Actually, the headcount ask landed at the Jun 18 1:1.")
	if !d.IsCorrection {
		t.Fatal("expected a correction")
	}
	want := "the headcount ask landed at the Jun 18 1:1"
	if d.Content != want {
		t.Errorf("Content = %q, want %q", d.Content, want)
	}
}

// TestDetectCorrection_ReportsAllFiredSignals proves the audit trail: when more
// than one cue fires, all are reported even though only the strongest classifies.
func TestDetectCorrection_ReportsAllFiredSignals(t *testing.T) {
	// A strong restate marker ("that's no longer true") + a weak reframe cue
	// ("owes") both fire. The strong one classifies; both are reported.
	d := DetectCorrection("That's no longer true — Ed owes us the terms now.")
	if d.Kind != CorrectRestate || d.Confidence != DetectStrong {
		t.Fatalf("expected strong restate to win, got kind=%q conf=%q", d.Kind, d.Confidence)
	}
	if !hasSignal(d.Signals, "reframe:owes") {
		t.Errorf("expected reframe:owes in fired signals for the audit trail, got %v", d.Signals)
	}
}

// TestDetection_ToCorrection_FeedsCorrect is the loop-closing wiring proof:
// a detected correction, completed with caller-owned fields, flows through the
// real Correct chokepoint and produces a valid supersession triple. This is the
// reflex calling the primitive end-to-end.
func TestDetection_ToCorrection_FeedsCorrect(t *testing.T) {
	old := correctableEntry(t) // from correction_test.go (the Adam TODO)
	asOf := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)

	d := DetectCorrection("Actually, the headcount ask landed at the Jun 18 1:1.")
	if !d.IsCorrection || d.Kind != CorrectRestate {
		t.Fatalf("detector failed to recognize the correction: %+v", d)
	}

	corr := d.ToCorrection(asOf, "adam headcount landed", "")
	if corr.Content != d.Content {
		t.Errorf("ToCorrection should carry the detected content; got %q", corr.Content)
	}

	closed, newer, edge, err := old.Correct(corr)
	if err != nil {
		t.Fatalf("Correct on a detected correction failed: %v", err)
	}
	// Triple shape: old closed at asOf, new current, SUPERSEDES edge new->old.
	if closed.ValidUntil == nil || !closed.ValidUntil.Equal(asOf) {
		t.Errorf("closed entry not closed at asOf: %+v", closed.ValidUntil)
	}
	if !newer.IsCurrent(asOf.Add(time.Hour)) {
		t.Error("new entry should be current")
	}
	if edge.Type != EdgeSupersedes || edge.From != newer.ID || edge.To != string(old.ID) {
		t.Errorf("edge not a SUPERSEDES from newer to old: %+v", edge)
	}
	// Provenance: kind stamped from the detection.
	if newer.Extra[ExtraCorrectionKind] != string(CorrectRestate) {
		t.Errorf("correction kind provenance = %q, want restate", newer.Extra[ExtraCorrectionKind])
	}
	// Content inherited the corrected statement, not the stale one.
	if strings.Contains(newer.Content, "Target Jun 16") {
		t.Errorf("new entry leaked stale content: %q", newer.Content)
	}
}

// TestDetection_ToCorrection_ContentOverride proves the operator can override an
// imperfect extraction while still using the detected kind.
func TestDetection_ToCorrection_ContentOverride(t *testing.T) {
	d := DetectCorrection("Scratch that: details follow.")
	corr := d.ToCorrection(time.Now(), "x", "the cleaned-up corrected statement")
	if corr.Content != "the cleaned-up corrected statement" {
		t.Errorf("override not applied: %q", corr.Content)
	}
	if corr.Kind != CorrectRestate {
		t.Errorf("kind should survive override: %q", corr.Kind)
	}
}
