// Package core is the innermost ring of the Ensō hexagon: the pure domain
// model. It has NO outward dependencies — no I/O, no storage, no framework, no
// serialization format. Per the unified spec (§3) and the portability invariant
// (PORT-INV), nothing in this package may import an adapter, a storage engine,
// or any host (OpenClaw, MCP, etc.). Adapters depend on core; core never
// depends on an adapter.
//
// The types here mirror the Phase-1 Markdown grammar (tech spec §3.1) and the
// consolidated schema reference (tech spec §6) one-to-one, but they are
// format-agnostic: the same Entry can be serialized to Markdown today and to a
// graph node later, behind the Store port, without changing this file.
package core

import (
	"fmt"
	"strings"
	"time"
)

// NodeType enumerates the memory node types (tech spec §6).
type NodeType string

const (
	TypeFact     NodeType = "Fact"
	TypeDecision NodeType = "Decision"
	TypeInsight  NodeType = "Insight"
	TypePerson   NodeType = "Person"
	TypeProject  NodeType = "Project"
	TypeTask     NodeType = "Task"
)

// ValidNodeTypes is the closed set of node types. Used for validation.
var ValidNodeTypes = map[NodeType]bool{
	TypeFact: true, TypeDecision: true, TypeInsight: true,
	TypePerson: true, TypeProject: true, TypeTask: true,
}

func (t NodeType) Valid() bool { return ValidNodeTypes[t] }

// Confidence is the confidence level of a memory (tech spec §3.1).
type Confidence string

const (
	ConfHigh   Confidence = "high"
	ConfMedium Confidence = "medium"
	ConfLow    Confidence = "low"
)

var validConfidence = map[Confidence]bool{
	ConfHigh: true, ConfMedium: true, ConfLow: true,
}

func (c Confidence) Valid() bool { return validConfidence[c] }

// EdgeType enumerates the relationship types (tech spec §6).
type EdgeType string

const (
	EdgeSupersedes EdgeType = "SUPERSEDES"
	EdgeRelatesTo  EdgeType = "RELATES_TO"
	EdgeOwns       EdgeType = "OWNS"
	EdgeAbout      EdgeType = "ABOUT"
)

var validEdgeTypes = map[EdgeType]bool{
	EdgeSupersedes: true, EdgeRelatesTo: true, EdgeOwns: true, EdgeAbout: true,
}

func (e EdgeType) Valid() bool { return validEdgeTypes[e] }

// Temporal holds the Phase-3 reserved decay fields. Per the single most
// important schema instruction (tech spec §3.2/§6), these are WRITTEN from
// Phase 1 with sane inits and sit inert until Phase 3 reads them. Writing them
// now means no backfill migration later.
//
// The math that will eventually consume these (tech spec §5.1) is:
//
//	S(t) = S_floor + (S_last − S_floor) · e^(−λ · Δt)
//
// where Δt = now − LastRefTime. Nothing in core computes this yet; these are
// pure data until the Phase-3 Texture layer lands.
type Temporal struct {
	LastRefTime time.Time // init = EncodedTime; updated every *material* recall (RECALL-DEF)
	SLast       float64   // vividness bump (decays); init = SCap
	SFloor      float64   // permanent importance (accumulates); low default
	Lambda      float64   // decay rate; per-type init
	SCap        float64   // consolidation ceiling; init 1.0 (TBD tuning)
}

// DefaultTemporal returns Phase-1 init values for the reserved decay fields
// (tech spec §6 "Phase-1 init" column). Exact floats are a Phase-3 tuning
// decision; these are defensible placeholders so no backfill is needed.
//
// LastRefTime is initialized to encodedTime by the caller (NewEntry), since it
// equals EncodedTime at creation.
func DefaultTemporal(nt NodeType) Temporal {
	const sCap = 1.0
	return Temporal{
		SLast:  sCap,              // init = S_cap (tech spec §6)
		SFloor: defaultSFloor(nt), // low default, type-aware
		Lambda: defaultLambda(nt), // per-type decay rate
		SCap:   sCap,
	}
}

// defaultSFloor: permanent-importance floor. Identity/relationship-ish types
// "matter forever" more than volatile operational detail (tech spec §5.2).
// These are placeholders; real values emerge from use / Phase-3 tuning.
func defaultSFloor(nt NodeType) float64 {
	switch nt {
	case TypePerson, TypeProject:
		return 0.2
	case TypeDecision, TypeInsight:
		return 0.1
	default: // Fact, Task
		return 0.05
	}
}

// defaultLambda: how fast a node goes stale if untouched (tech spec §5.2).
// High for volatile statuses (Task), low for stable facts. Placeholders.
func defaultLambda(nt NodeType) float64 {
	switch nt {
	case TypeTask:
		return 0.10
	case TypeProject:
		return 0.02
	default:
		return 0.05
	}
}

// Entry is one memory node — the in-memory representation of a Phase-1
// structured entry (tech spec §3.1). It is the durable unit of memory.
//
// Pointer fields (EventTime, ValidFrom, ValidUntil) encode the explicit
// known-unknown distinction the parser contract requires (tech spec §3.2/§3.4):
// a nil pointer means an explicit `null` (a *known* unknown), which is distinct
// from a missing key (a format error caught at the adapter boundary, not here).
//
// Extra preserves unknown keys for forward-compat (tech spec §3.4: "Unknown
// keys are preserved, not dropped"). The domain doesn't interpret them, but it
// must not lose them on round-trip.
type Entry struct {
	ID          ID
	Type        NodeType
	Content     string
	EncodedTime time.Time  // REQUIRED — when I recorded it
	EventTime   *time.Time // when it became true in the world; nil = null
	ValidFrom   *time.Time // validity start; nil = null
	ValidUntil  *time.Time // validity end; nil = null = still current
	Confidence  Confidence
	Tags        []string
	About       []string // entity-refs, e.g. "project:omega", "person:matt"
	Temporal    Temporal // Phase-3 reserved fields (written, inert until P3)

	// Extra preserves unknown keys verbatim for lossless round-trip (INV-1).
	Extra map[string]string
}

// IsCurrent reports whether the entry is still valid (not superseded/closed).
// A nil ValidUntil means "still true" (tech spec §3.1). This is the text-layer
// staleness signal (fix #4) before any graph exists.
func (e Entry) IsCurrent(now time.Time) bool {
	if e.ValidUntil == nil {
		return true
	}
	return now.Before(*e.ValidUntil)
}

// Validate enforces the Phase-1 required-field rules (tech spec §3.2).
// Required at Phase 1: id, type, content, encoded_time, confidence, tags.
// (tags may be empty, but the slice must be non-nil so the contract "key
// present" holds; NewEntry guarantees this.)
func (e Entry) Validate() error {
	if err := e.ID.Validate(); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if !e.Type.Valid() {
		return fmt.Errorf("type: invalid node type %q", e.Type)
	}
	if strings.TrimSpace(e.Content) == "" {
		return fmt.Errorf("content: must not be empty")
	}
	if e.EncodedTime.IsZero() {
		return fmt.Errorf("encoded_time: required, must not be zero")
	}
	if !e.Confidence.Valid() {
		return fmt.Errorf("confidence: invalid value %q", e.Confidence)
	}
	if e.Tags == nil {
		return fmt.Errorf("tags: key must be present (non-nil slice; empty is ok)")
	}
	return nil
}

// Edge is a typed relationship between a memory node and another node or
// entity-ref (tech spec §3.1 edge block).
//
// From is always a memory ID. To may be a memory ID or an entity-ref
// (e.g. "person:matt"), so it is a plain string — the domain does not force
// the target to be a known node, matching the grammar.
type Edge struct {
	From  ID
	Type  EdgeType
	To    string
	Extra map[string]string // forward-compat, same rationale as Entry.Extra
}

// Validate enforces edge well-formedness.
func (e Edge) Validate() error {
	if err := e.From.Validate(); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if !e.Type.Valid() {
		return fmt.Errorf("type: invalid edge type %q", e.Type)
	}
	if strings.TrimSpace(e.To) == "" {
		return fmt.Errorf("to: must not be empty")
	}
	return nil
}
