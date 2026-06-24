package core

import (
	"testing"
	"time"
)

// correctableEntry builds a valid "old" entry to be corrected, encoded on a fixed day.
func correctableEntry(t *testing.T) Entry {
	t.Helper()
	encoded := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	id, err := NewID(encoded, "adam headcount todo")
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	e, err := NewEntry(NewEntryParams{
		ID:          id,
		Type:        TypeTask,
		Content:     "Message Adam re: headcount. Target Jun 16; prep slipped.",
		EncodedTime: encoded,
		Confidence:  ConfMedium,
		Tags:        []string{"work", "team"},
		About:       []string{"person:adam", "project:axon"},
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

func TestCorrect_ProducesCanonicalTriple(t *testing.T) {
	old := correctableEntry(t)
	asOf := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)

	closed, newer, edge, err := old.Correct(Correction{
		Kind:     CorrectRestate,
		Content:  "Adam headcount ask landed at the Jun 18 1:1. Adam aligned.",
		NewLabel: "adam headcount landed",
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}

	// closed = old with ValidUntil = asOf; content untouched (INV-2).
	if closed.ValidUntil == nil || !closed.ValidUntil.Equal(asOf) {
		t.Errorf("closed.ValidUntil = %v, want %v", closed.ValidUntil, asOf)
	}
	if closed.Content != old.Content {
		t.Errorf("closed content was mutated: %q", closed.Content)
	}
	if closed.IsCurrent(asOf.Add(time.Second)) {
		t.Errorf("closed entry must not be current after asOf")
	}

	// newer is current and carries corrected content.
	if !newer.IsCurrent(asOf.Add(time.Hour)) {
		t.Errorf("newer entry must be current")
	}
	if newer.ValidUntil != nil {
		t.Errorf("newer.ValidUntil = %v, want nil (head of chain)", newer.ValidUntil)
	}
	if newer.EncodedTime != asOf {
		t.Errorf("newer.EncodedTime = %v, want asOf %v", newer.EncodedTime, asOf)
	}

	// edge: SUPERSEDES from newer -> old.
	if edge.Type != EdgeSupersedes {
		t.Errorf("edge.Type = %q, want SUPERSEDES", edge.Type)
	}
	if edge.From != newer.ID {
		t.Errorf("edge.From = %q, want newer %q", edge.From, newer.ID)
	}
	if edge.To != string(old.ID) {
		t.Errorf("edge.To = %q, want old %q", edge.To, old.ID)
	}
	if err := edge.Validate(); err != nil {
		t.Errorf("edge invalid: %v", err)
	}
}

func TestCorrect_Inheritance(t *testing.T) {
	old := correctableEntry(t)
	asOf := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)

	// Leave Type/Confidence/Tags/About zero → inherit from old (conf defaults high).
	_, newer, _, err := old.Correct(Correction{
		Kind:     CorrectRestate,
		Content:  "corrected",
		NewLabel: "adam corrected",
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if newer.Type != old.Type {
		t.Errorf("Type = %q, want inherited %q", newer.Type, old.Type)
	}
	if newer.Confidence != ConfHigh {
		t.Errorf("Confidence = %q, want default ConfHigh", newer.Confidence)
	}
	if len(newer.Tags) != len(old.Tags) || newer.Tags[0] != old.Tags[0] {
		t.Errorf("Tags = %v, want inherited %v", newer.Tags, old.Tags)
	}
	if len(newer.About) != len(old.About) {
		t.Errorf("About = %v, want inherited %v", newer.About, old.About)
	}

	// Inheritance must COPY, not alias: mutating newer's slice must not touch old.
	newer.Tags[0] = "MUTATED"
	if old.Tags[0] == "MUTATED" {
		t.Errorf("inherited Tags aliased old's slice (mutation leaked)")
	}
}

func TestCorrect_ExplicitOverrides(t *testing.T) {
	old := correctableEntry(t)
	asOf := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)

	_, newer, _, err := old.Correct(Correction{
		Kind:       CorrectReframe,
		Content:    "reframed",
		NewLabel:   "adam reframed",
		AsOf:       asOf,
		Type:       TypeDecision,
		Confidence: ConfLow,
		Tags:       []string{},        // explicit clear
		About:      []string{"person:adam"},
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if newer.Type != TypeDecision {
		t.Errorf("Type = %q, want explicit Decision", newer.Type)
	}
	if newer.Confidence != ConfLow {
		t.Errorf("Confidence = %q, want explicit Low", newer.Confidence)
	}
	if len(newer.Tags) != 0 {
		t.Errorf("Tags = %v, want explicitly cleared (empty)", newer.Tags)
	}
	if len(newer.About) != 1 {
		t.Errorf("About = %v, want explicit single", newer.About)
	}
}

func TestCorrect_Provenance(t *testing.T) {
	old := correctableEntry(t)
	asOf := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)

	_, newer, _, err := old.Correct(Correction{
		Kind:     CorrectReframe,
		Content:  "reframed",
		NewLabel: "adam prov",
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if got := newer.Extra[ExtraCorrectionKind]; got != string(CorrectReframe) {
		t.Errorf("provenance kind = %q, want %q", got, CorrectReframe)
	}
	if got := newer.Extra[ExtraSupersededID]; got != string(old.ID) {
		t.Errorf("provenance supersedes = %q, want %q", got, old.ID)
	}
	if got := newer.Extra[ExtraCorrectionAsOf]; got != asOf.Format(time.RFC3339) {
		t.Errorf("provenance as_of = %q, want %q", got, asOf.Format(time.RFC3339))
	}
}

func TestCorrect_EventTimeOlderThanCapture(t *testing.T) {
	// Reframe shape: the corrected fact became true (EventTime) BEFORE it was
	// captured (AsOf). Both must be preserved and distinct.
	old := correctableEntry(t)
	asOf := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	eventTime := time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC)

	_, newer, _, err := old.Correct(Correction{
		Kind:      CorrectReframe,
		Content:   "open dependency is on Ed's side since May 26",
		NewLabel:  "ed owes terms",
		AsOf:      asOf,
		EventTime: &eventTime,
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if newer.EncodedTime != asOf {
		t.Errorf("EncodedTime = %v, want capture time %v", newer.EncodedTime, asOf)
	}
	if newer.EventTime == nil || !newer.EventTime.Equal(eventTime) {
		t.Errorf("EventTime = %v, want world-time %v", newer.EventTime, eventTime)
	}
}

func TestCorrect_Errors(t *testing.T) {
	old := correctableEntry(t)
	good := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		c    Correction
	}{
		{"bad kind", Correction{Kind: "bogus", Content: "x", NewLabel: "l", AsOf: good}},
		{"empty content", Correction{Kind: CorrectRestate, Content: "  ", NewLabel: "l", AsOf: good}},
		{"unslugifiable label", Correction{Kind: CorrectRestate, Content: "x", NewLabel: "!!!", AsOf: good}},
		{"zero asof", Correction{Kind: CorrectRestate, Content: "x", NewLabel: "l"}},
		{"asof before encoded", Correction{Kind: CorrectRestate, Content: "x", NewLabel: "l",
			AsOf: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := old.Correct(tc.c)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

// TestCorrect_RoundTripLoop is the end-to-end proof: a correction captured via
// Correct, when filtered by IsCurrent + the emitted SUPERSEDES edge, surfaces
// the new entry over the (re-surfaced, recency-fresh) closed entry. This is the
// whole point — capture feeds consumption with no hand-wiring.
func TestCorrect_RoundTripLoop(t *testing.T) {
	old := correctableEntry(t)
	asOf := time.Date(2026, 6, 18, 15, 0, 0, 0, time.UTC)
	query := time.Date(2026, 6, 23, 21, 0, 0, 0, time.UTC)

	closed, newer, edge, err := old.Correct(Correction{
		Kind:     CorrectRestate,
		Content:  "landed at Jun 18 1:1",
		NewLabel: "adam landed",
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}

	// Simulate the stale item being re-scanned today so it looks recency-fresh,
	// exactly like the live failure dynamic. (We bump LastRefTime, not content.)
	closed.Temporal.LastRefTime = query.Add(-time.Hour)

	// Consumption side: drop superseded + non-current, then trust what remains.
	superseded := map[ID]bool{ID(edge.To): true}
	candidates := []Entry{closed, newer}
	var survivors []Entry
	for _, c := range candidates {
		if !c.IsCurrent(query) || superseded[c.ID] {
			continue
		}
		survivors = append(survivors, c)
	}
	if len(survivors) != 1 {
		t.Fatalf("survivors = %d, want 1 (only the corrected entry)", len(survivors))
	}
	if survivors[0].ID != newer.ID {
		t.Errorf("survivor = %q, want corrected entry %q", survivors[0].ID, newer.ID)
	}
}
