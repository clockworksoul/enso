package core

import (
	"testing"
	"time"
)

// contradict_test.go guards the resolver-side contradiction check on BOTH sides
// of the precision fence, mirroring the Jul 1 seam-#0 discipline for the lexical
// detector:
//
//   - MUST FIRE: the real Granola-ban H1 miss and analogous
//     affirmation-vs-stored-negation contradictions the lexical detector could
//     not catch.
//   - MUST NOT FIRE: innocent status remarks with no contradicting stored
//     belief, affirmations about a DIFFERENT subject, and affirmations against
//     an already-superseded (non-current) entry.
//
// Under the no-reconsolidation rule a false-positive contradiction that gets
// confirmed is permanent corruption, so the must-NOT-fire set is as important
// as the must-fire set.

func now2026() time.Time { return time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC) }

// storedNegation builds a CURRENT entry asserting a negative/removal status,
// the kind of stale belief a contradiction should fire against.
func storedNegation(t *testing.T, label, content string, tags, about []string) Entry {
	t.Helper()
	id, err := NewID(time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC), label)
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	e, err := NewEntry(NewEntryParams{
		ID:          id,
		Type:        TypeFact,
		Content:     content,
		EncodedTime: time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC),
		Confidence:  ConfHigh,
		Tags:        tags,
		About:       about,
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

// --- MUST FIRE ---------------------------------------------------------------

func TestDetectContradiction_GranolaBanH1(t *testing.T) {
	// The canonical held-out STALE miss. The stored belief says Granola is
	// banned; the utterance affirms it still works. The lexical detector cannot
	// catch the BARE form ("Granola still works") — this layer must.
	stored := storedNegation(t,
		"granola-banned",
		"Granola is banned per Yext policy (Jun 22). Default to the Zoom -> yext/transcripts replacement workflow.",
		[]string{"work", "tools", "granola", "transcripts"},
		[]string{"tool:granola", "policy:yext"},
	)

	fireCases := []string{
		"Granola still works",
		"Granola still works fine, use it",
		"actually Granola still works and is the source of record", // also has a marker; must still fire here
		"the Granola ban never took effect",
		"Granola isn't banned",
		"Granola works again",
	}
	for _, u := range fireCases {
		c := DetectContradiction(u, stored, now2026())
		if !c.IsContradiction {
			t.Errorf("expected contradiction for %q, got none (signals=%v)", u, c.Signals)
			continue
		}
		if c.Confidence != DetectWeak {
			t.Errorf("%q: want DetectWeak, got %s", u, c.Confidence)
		}
		if !containsStr(c.SubjectTerms, "granola") {
			t.Errorf("%q: expected 'granola' among subject terms, got %v", u, c.SubjectTerms)
		}
		if len(c.Signals) != 3 {
			t.Errorf("%q: expected 3 audit signals (affirm/negation/subject), got %v", u, c.Signals)
		}
	}
}

func TestDetectContradiction_OtherStoredNegations(t *testing.T) {
	// The vocabulary generalizes across the negation words a real note might use.
	tests := []struct {
		name     string
		content  string
		utter    string
		wantSubj string
	}{
		{"deprecated-api", "The legacy billing API is deprecated; do not call it.", "the legacy billing API still works", "billing"},
		{"removed-flag", "The beta feature flag was removed in the last release.", "that beta feature flag is still active", "beta"},
		{"disabled-account", "The staging account is disabled and cannot log in.", "the staging account is fine, it isn't disabled", "staging"},
		{"blocked-domain", "Outbound to example.net is blocked by policy.", "outbound to example.net works again", "example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := storedNegation(t, tt.name, tt.content, []string{"work"}, nil)
			c := DetectContradiction(tt.utter, stored, now2026())
			if !c.IsContradiction {
				t.Fatalf("expected contradiction for %q vs %q, got none", tt.utter, tt.content)
			}
			if !containsStr(c.SubjectTerms, tt.wantSubj) {
				t.Errorf("expected subject %q, got %v", tt.wantSubj, c.SubjectTerms)
			}
		})
	}
}

// --- MUST NOT FIRE -----------------------------------------------------------

// TestDetectContradiction_NoStoredNegation is the primary false-positive guard:
// an operative-status affirmation with NO contradicting stored belief is an
// ordinary status remark and must be silent. These are the exact sentences that
// tripped the bare lexical form 13/18 on Jul 1 — here they must be 0/N because
// the STORED half of the evidence is absent.
func TestDetectContradiction_NoStoredNegation(t *testing.T) {
	// A perfectly ordinary CURRENT note that asserts NO negation.
	ordinary := storedNegation(t,
		"staging-note",
		"The staging environment mirrors production for the checkout flow.",
		[]string{"work", "staging"}, nil,
	)
	innocents := []string{
		"the staging link is still live",
		"that coupon is still valid",
		"the API key still works",
		"the staging environment is fine",
		"the checkout flow still works",
		"the dashboard still runs fine",
		"the nightly job is still active",
		"the docs are still available",
		"the feature still applies",
		"the cache still holds the value",
	}
	for _, u := range innocents {
		c := DetectContradiction(u, ordinary, now2026())
		if c.IsContradiction {
			t.Errorf("false positive: %q contradicted a note with no negation (signals=%v)", u, c.Signals)
		}
	}
}

// TestDetectContradiction_DifferentSubject: the utterance affirms and the stored
// entry negates, but they are about DIFFERENT things — must not fire.
func TestDetectContradiction_DifferentSubject(t *testing.T) {
	stored := storedNegation(t,
		"granola-banned",
		"Granola is banned per Yext policy.",
		[]string{"tools", "granola"}, []string{"tool:granola"},
	)
	// Affirmation is about Zoom / a coupon / the VPN — not Granola.
	notAboutGranola := []string{
		"Zoom still works for recording meetings",
		"that coupon is still valid",
		"the VPN still works fine",
		"the API key works again",
	}
	for _, u := range notAboutGranola {
		c := DetectContradiction(u, stored, now2026())
		if c.IsContradiction {
			t.Errorf("false positive: %q fired against a granola-ban note (subjects=%v)", u, c.SubjectTerms)
		}
	}
}

// TestDetectContradiction_StatusWordOnlyOverlap: overlap consisting ONLY of
// operative-status words ("still", "works", "active") must NOT count as
// same-subject evidence. The stored negation is about a totally different topic.
func TestDetectContradiction_StatusWordOnlyOverlap(t *testing.T) {
	// Stored negation whose ONLY tokens shared with a generic affirmation would
	// be status words if they weren't excluded.
	stored := storedNegation(t,
		"printer-removed",
		"The office printer was removed and is no longer available.",
		[]string{"office"}, nil,
	)
	// Shares "still"/"works"/"active" style words only, not "printer".
	u := "the build still works and the tests are still active"
	c := DetectContradiction(u, stored, now2026())
	if c.IsContradiction {
		t.Errorf("false positive on status-word-only overlap: subjects=%v signals=%v", c.SubjectTerms, c.Signals)
	}
}

// TestDetectContradiction_SupersededTargetIgnored: you cannot contradict a
// belief that is no longer current. If the negation entry has already been
// superseded (ValidUntil in the past), affirming against it is silent.
func TestDetectContradiction_SupersededTargetIgnored(t *testing.T) {
	stored := storedNegation(t,
		"granola-banned",
		"Granola is banned per Yext policy.",
		[]string{"tools", "granola"}, []string{"tool:granola"},
	)
	// Close it before the query instant.
	closed := now2026().Add(-1 * time.Hour)
	stored.ValidUntil = &closed

	c := DetectContradiction("Granola still works", stored, now2026())
	if c.IsContradiction {
		t.Errorf("false positive: contradicted an already-superseded (non-current) entry")
	}
}

// TestDetectContradiction_EmptyUtterance: whitespace / empty in → silent.
func TestDetectContradiction_EmptyUtterance(t *testing.T) {
	stored := storedNegation(t, "granola-banned", "Granola is banned.", []string{"granola"}, nil)
	for _, u := range []string{"", "   ", "\t\n"} {
		if c := DetectContradiction(u, stored, now2026()); c.IsContradiction {
			t.Errorf("empty utterance %q should not contradict", u)
		}
	}
}

// TestDetectContradiction_ToDetection: the adapter feeds the shared committed
// chokepoint. A contradiction is always a restate, weak confidence, and carries
// NO content (operator must supply it — the reframe-class invariant).
func TestDetectContradiction_ToDetection(t *testing.T) {
	stored := storedNegation(t,
		"granola-banned",
		"Granola is banned per Yext policy.",
		[]string{"tools", "granola"}, []string{"tool:granola"},
	)
	c := DetectContradiction("Granola still works", stored, now2026())
	if !c.IsContradiction {
		t.Fatal("precondition: expected contradiction")
	}
	d := c.ToDetection()
	if !d.IsCorrection {
		t.Error("ToDetection: expected IsCorrection=true")
	}
	if d.Kind != CorrectRestate {
		t.Errorf("ToDetection: want CorrectRestate, got %s", d.Kind)
	}
	if d.Confidence != DetectWeak {
		t.Errorf("ToDetection: want DetectWeak, got %s", d.Confidence)
	}
	if d.Content != "" {
		t.Errorf("ToDetection: content must be empty (operator-supplied), got %q", d.Content)
	}
	if len(d.Signals) != 3 {
		t.Errorf("ToDetection: expected 3 signals carried through, got %v", d.Signals)
	}
}

// TestDetectContradiction_EndToEndBarredWithoutContent proves the safety
// invariant end to end: a contradiction detected from the utterance alone
// carries no corrected content, so feeding it straight through
// ToDetection→ToCorrection→Correct is REFUSED (empty content). The operator
// must supply the new statement — the riskiest inference stays off the
// unattended path, exactly like the reframe class.
func TestDetectContradiction_EndToEndBarredWithoutContent(t *testing.T) {
	stored := storedNegation(t,
		"granola-banned",
		"Granola is banned per Yext policy.",
		[]string{"tools", "granola"}, []string{"tool:granola"},
	)
	c := DetectContradiction("Granola still works", stored, now2026())
	if !c.IsContradiction {
		t.Fatal("precondition: expected contradiction")
	}
	// Detection-only path: no operator content.
	corr := c.ToDetection().ToCorrection(now2026(), "granola still works", "")
	_, _, _, err := stored.Correct(corr)
	if err == nil {
		t.Fatal("expected Correct to REFUSE a contradiction with empty content, got nil error")
	}

	// With operator-supplied content, the same contradiction commits cleanly.
	corr2 := c.ToDetection().ToCorrection(now2026(), "granola still works",
		"Granola still works and is the transcript source of record; the Jun-22 ban policy is not operative.")
	stale, current, edge, err := stored.Correct(corr2)
	if err != nil {
		t.Fatalf("expected clean commit with operator content, got %v", err)
	}
	if stale.IsCurrent(now2026()) {
		t.Error("stale entry should be closed after Correct")
	}
	if !current.IsCurrent(now2026()) {
		t.Error("new entry should be current after Correct")
	}
	if edge.Type != EdgeSupersedes {
		t.Errorf("expected SUPERSEDES edge, got %s", edge.Type)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
