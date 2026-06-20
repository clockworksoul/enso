package core

import (
	"testing"
	"time"
)

func TestNewEntry_InitsInvariants(t *testing.T) {
	enc := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	e, err := NewEntry(NewEntryParams{
		ID:          "mem:2026-06-20-omega",
		Type:        TypeProject,
		Content:     "Omega Lite MVP",
		EncodedTime: enc,
		Confidence:  ConfHigh,
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	// Tags/About must be non-nil even when omitted (the "key must be present"
	// contract, tech spec §3.2).
	if e.Tags == nil {
		t.Error("Tags should be non-nil after NewEntry")
	}
	if e.About == nil {
		t.Error("About should be non-nil after NewEntry")
	}
	if e.Extra == nil {
		t.Error("Extra should be non-nil after NewEntry")
	}

	// Reserved temporal fields written from day one (tech spec §3.2/§6).
	if !e.Temporal.LastRefTime.Equal(enc) {
		t.Errorf("LastRefTime init = %v, want = EncodedTime %v", e.Temporal.LastRefTime, enc)
	}
	if e.Temporal.SLast != e.Temporal.SCap {
		t.Errorf("SLast init = %v, want SCap %v", e.Temporal.SLast, e.Temporal.SCap)
	}
	if e.Temporal.Lambda <= 0 {
		t.Error("Lambda should be initialized positive")
	}

	// Nullable fields default to explicit null (nil pointer).
	if e.EventTime != nil || e.ValidFrom != nil || e.ValidUntil != nil {
		t.Error("nullable time fields should default to nil (explicit null)")
	}
}

func TestNewEntry_RejectsInvalid(t *testing.T) {
	enc := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	_, err := NewEntry(NewEntryParams{
		ID:          "bad-id",
		Type:        TypeFact,
		Content:     "x",
		EncodedTime: enc,
		Confidence:  ConfHigh,
	})
	if err == nil {
		t.Fatal("NewEntry should reject an invalid id")
	}
}

func TestNewEntry_PreservesProvidedNullables(t *testing.T) {
	enc := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	evt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	e, err := NewEntry(NewEntryParams{
		ID:          "mem:2026-06-20-evt",
		Type:        TypeFact,
		Content:     "something happened on the first",
		EncodedTime: enc,
		EventTime:   &evt,
		Confidence:  ConfMedium,
		Tags:        []string{"history"},
		About:       []string{"project:omega"},
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if e.EventTime == nil || !e.EventTime.Equal(evt) {
		t.Errorf("EventTime not preserved: %v", e.EventTime)
	}
	if len(e.Tags) != 1 || e.Tags[0] != "history" {
		t.Errorf("Tags not preserved: %v", e.Tags)
	}
	if len(e.About) != 1 || e.About[0] != "project:omega" {
		t.Errorf("About not preserved: %v", e.About)
	}
}

// TestSupersede_AppendOnly is the load-bearing INV-2 test: superseding an entry
// must NOT mutate its content, must close it with valid_until, and must produce
// a SUPERSEDES edge new->old. The original value the caller holds must be
// untouched (non-destructive at the API level).
func TestSupersede_AppendOnly(t *testing.T) {
	old := baseEntry(t)
	oldContentBefore := old.Content
	newerID := ID("mem:2026-06-21-base-updated")
	at := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)

	closed, edge := old.Supersede(newerID, at)

	// INV-2: original content never rewritten.
	if closed.Content != oldContentBefore {
		t.Errorf("Supersede must not change content: got %q, want %q", closed.Content, oldContentBefore)
	}
	// The caller's original value is unchanged (Supersede returns a copy).
	if old.ValidUntil != nil {
		t.Error("Supersede must not mutate the receiver's ValidUntil in place")
	}
	// The closed copy is flagged stale.
	if closed.ValidUntil == nil || !closed.ValidUntil.Equal(at) {
		t.Errorf("closed entry ValidUntil = %v, want %v", closed.ValidUntil, at)
	}
	if closed.IsCurrent(at.Add(time.Second)) {
		t.Error("closed entry should not be current after supersession")
	}
	// The edge points new -> old, typed SUPERSEDES.
	if edge.From != newerID {
		t.Errorf("edge.From = %q, want %q", edge.From, newerID)
	}
	if edge.Type != EdgeSupersedes {
		t.Errorf("edge.Type = %q, want SUPERSEDES", edge.Type)
	}
	if edge.To != string(old.ID) {
		t.Errorf("edge.To = %q, want %q", edge.To, old.ID)
	}
	if err := edge.Validate(); err != nil {
		t.Errorf("supersession edge should validate: %v", err)
	}
}
