package core

import (
	"testing"
	"time"
)

func TestNodeType_Valid(t *testing.T) {
	for nt := range ValidNodeTypes {
		if !nt.Valid() {
			t.Errorf("%q should be valid", nt)
		}
	}
	if NodeType("Nonsense").Valid() {
		t.Error("unknown node type should be invalid")
	}
}

func TestConfidence_Valid(t *testing.T) {
	for _, c := range []Confidence{ConfHigh, ConfMedium, ConfLow} {
		if !c.Valid() {
			t.Errorf("%q should be valid", c)
		}
	}
	if Confidence("certain").Valid() {
		t.Error("unknown confidence should be invalid")
	}
}

func TestEdgeType_Valid(t *testing.T) {
	for _, e := range []EdgeType{EdgeSupersedes, EdgeRelatesTo, EdgeOwns, EdgeAbout} {
		if !e.Valid() {
			t.Errorf("%q should be valid", e)
		}
	}
	if EdgeType("POINTS_AT").Valid() {
		t.Error("unknown edge type should be invalid")
	}
}

// baseEntry returns a minimally valid entry for mutation in tests.
func baseEntry(t *testing.T) Entry {
	t.Helper()
	e, err := NewEntry(NewEntryParams{
		ID:          "mem:2026-06-20-base",
		Type:        TypeFact,
		Content:     "the sky is blue",
		EncodedTime: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
		Confidence:  ConfHigh,
	})
	if err != nil {
		t.Fatalf("baseEntry: %v", err)
	}
	return e
}

func TestEntry_Validate(t *testing.T) {
	good := baseEntry(t)
	if err := good.Validate(); err != nil {
		t.Fatalf("base entry should validate: %v", err)
	}

	t.Run("bad id", func(t *testing.T) {
		e := baseEntry(t)
		e.ID = "not-an-id"
		if err := e.Validate(); err == nil {
			t.Error("expected error for bad id")
		}
	})
	t.Run("bad type", func(t *testing.T) {
		e := baseEntry(t)
		e.Type = "Bogus"
		if err := e.Validate(); err == nil {
			t.Error("expected error for bad type")
		}
	})
	t.Run("empty content", func(t *testing.T) {
		e := baseEntry(t)
		e.Content = "   "
		if err := e.Validate(); err == nil {
			t.Error("expected error for empty content")
		}
	})
	t.Run("zero encoded_time", func(t *testing.T) {
		e := baseEntry(t)
		e.EncodedTime = time.Time{}
		if err := e.Validate(); err == nil {
			t.Error("expected error for zero encoded_time")
		}
	})
	t.Run("bad confidence", func(t *testing.T) {
		e := baseEntry(t)
		e.Confidence = "maybe"
		if err := e.Validate(); err == nil {
			t.Error("expected error for bad confidence")
		}
	})
	t.Run("nil tags", func(t *testing.T) {
		e := baseEntry(t)
		e.Tags = nil
		if err := e.Validate(); err == nil {
			t.Error("expected error for nil tags (key must be present)")
		}
	})
}

func TestEntry_IsCurrent(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	t.Run("nil valid_until is current", func(t *testing.T) {
		e := baseEntry(t)
		if !e.IsCurrent(now) {
			t.Error("entry with nil ValidUntil should be current")
		}
	})
	t.Run("future valid_until is current", func(t *testing.T) {
		e := baseEntry(t)
		future := now.Add(time.Hour)
		e.ValidUntil = &future
		if !e.IsCurrent(now) {
			t.Error("entry with future ValidUntil should be current")
		}
	})
	t.Run("past valid_until is not current", func(t *testing.T) {
		e := baseEntry(t)
		past := now.Add(-time.Hour)
		e.ValidUntil = &past
		if e.IsCurrent(now) {
			t.Error("entry with past ValidUntil should not be current")
		}
	})
}

func TestEdge_Validate(t *testing.T) {
	good := Edge{From: "mem:2026-06-20-a", Type: EdgeRelatesTo, To: "mem:2026-06-20-b"}
	if err := good.Validate(); err != nil {
		t.Fatalf("good edge should validate: %v", err)
	}

	t.Run("entity-ref target ok", func(t *testing.T) {
		e := Edge{From: "mem:2026-06-20-a", Type: EdgeAbout, To: "person:matt"}
		if err := e.Validate(); err != nil {
			t.Errorf("entity-ref target should be allowed: %v", err)
		}
	})
	t.Run("bad from", func(t *testing.T) {
		e := Edge{From: "x", Type: EdgeOwns, To: "mem:2026-06-20-b"}
		if err := e.Validate(); err == nil {
			t.Error("expected error for bad from")
		}
	})
	t.Run("bad type", func(t *testing.T) {
		e := Edge{From: "mem:2026-06-20-a", Type: "NOPE", To: "mem:2026-06-20-b"}
		if err := e.Validate(); err == nil {
			t.Error("expected error for bad edge type")
		}
	})
	t.Run("empty to", func(t *testing.T) {
		e := Edge{From: "mem:2026-06-20-a", Type: EdgeOwns, To: ""}
		if err := e.Validate(); err == nil {
			t.Error("expected error for empty to")
		}
	})
}

func TestDefaultTemporal(t *testing.T) {
	// SCap must equal SLast init, and SFloor must be below SCap for every type
	// (the leaky integrator requires S_floor <= S_last <= S_cap to behave).
	for nt := range ValidNodeTypes {
		tmp := DefaultTemporal(nt)
		if tmp.SCap <= 0 {
			t.Errorf("%s: SCap must be positive, got %v", nt, tmp.SCap)
		}
		if tmp.SLast != tmp.SCap {
			t.Errorf("%s: SLast init should equal SCap (%v), got %v", nt, tmp.SCap, tmp.SLast)
		}
		if tmp.SFloor < 0 || tmp.SFloor > tmp.SCap {
			t.Errorf("%s: SFloor must be in [0, SCap], got %v (SCap %v)", nt, tmp.SFloor, tmp.SCap)
		}
		if tmp.Lambda <= 0 {
			t.Errorf("%s: Lambda must be positive, got %v", nt, tmp.Lambda)
		}
	}
}
