package core

import (
	"context"
	"fmt"
	"time"
)

// correction.go is the CAPTURE side of the staleness loop.
//
// The rest of core knows how to *consume* a correction once it exists:
// Entry.Supersede closes a stale entry, Entry.IsCurrent filters it out, and the
// ranker (Rank/StrengthAt) surfaces the survivor. But none of that fires unless
// a correction was captured as the canonical (closed-old, new, SUPERSEDES-edge)
// triple in the first place.
//
// That capture step is the single load-bearing operation for the dominant
// real-world failure mode. In the live miss log, STALE is the early-dominant
// class: the model held a fact that was correct-as-of-an-old-state and never
// updated it. The neurological-grounding analysis (2026-06-23) made the reason
// explicit: this architecture has retrieval-augmented recall over a frozen
// learner — there is no reconsolidation — so an *explicitly written correction
// is the only update path*. If a correction is observed but not captured, the
// stale entry wins forever, because to a recency/salience heuristic a standing
// stale item that keeps getting re-scanned looks perpetually fresh.
//
// Correct is therefore the chokepoint every correction must flow through. It
// turns a free-form "actually, it's now X" signal into exactly the triple the
// consumption side already understands, so capture and consumption can never
// drift apart. Keeping it in core (pure, no I/O) means the same capture logic
// is shared by every adapter and by the offline benchmark — the benchmark can
// build its STALE cases by *exercising the real capture path* rather than
// hand-assembling edges, which is what makes the loop trustworthy end-to-end.

// CorrectionKind classifies *why* an entry is being superseded. It does not
// change the mechanics (every kind produces the same supersession triple); it
// is provenance, preserved on the new entry's Extra so the corpus and any later
// audit can see what sort of update happened. The taxonomy mirrors the live
// miss classes that motivate capture.
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

var validCorrectionKinds = map[CorrectionKind]bool{
	CorrectRestate: true, CorrectReframe: true, CorrectRetract: true,
}

func (k CorrectionKind) Valid() bool { return validCorrectionKinds[k] }

// Extra keys written onto the NEW entry to record correction provenance. These
// live in Entry.Extra so they survive the lossless round-trip (INV-1) without
// requiring a schema change to the typed fields.
const (
	ExtraCorrectionKind = "correction_kind"  // the CorrectionKind value
	ExtraSupersededID   = "supersedes"       // the closed old entry's ID
	ExtraCorrectionAsOf = "correction_as_of" // RFC3339 instant the correction was captured
)

// Correction is the input to Correct: the free-form signal that a held entry is
// now wrong, plus what the corrected understanding is. The caller supplies the
// human-meaningful content of the new entry and a label used to mint its ID;
// everything structural (validity windows, the edge, provenance) is derived so
// the caller cannot produce a malformed or half-captured correction.
type Correction struct {
	// Kind is why the supersession is happening (provenance only).
	Kind CorrectionKind

	// Content is the corrected statement — the content of the NEW entry that
	// becomes the current head of the supersession chain.
	Content string

	// NewLabel is slugified into the new entry's ID (see NewID). Required.
	NewLabel string

	// AsOf is the instant the correction is captured. The new entry is encoded
	// at AsOf and the old entry is closed (ValidUntil) at AsOf, so the handoff
	// is atomic on the timeline: the instant the old stops being current is the
	// instant the new starts.
	AsOf time.Time

	// Type/Confidence for the new entry. If Type is empty it inherits the
	// superseded entry's Type (a correction is almost always the same kind of
	// thing). If Confidence is empty it defaults to ConfHigh — an explicit human
	// correction is high-confidence by construction.
	Type       NodeType
	Confidence Confidence

	// Tags/About for the new entry. If nil, they are inherited from the
	// superseded entry so the correction stays attached to the same person /
	// project / topic and remains co-retrievable with what it replaced. Pass an
	// empty (non-nil) slice to deliberately clear them.
	Tags  []string
	About []string

	// EventTime optionally records when the corrected fact became true in the
	// world (distinct from AsOf, when we learned/recorded it). Critical for the
	// reframe class: the corrected fact is often OLDER than the stale belief by
	// world-time even though it is captured now. nil leaves it unset.
	EventTime *time.Time
}

// CommitCorrection is the I/O companion to Entry.Correct: it captures a
// correction against old, produces the canonical supersession triple via
// Entry.Correct, and atomically persists all three via store.Append. It is the
// persist path that completes the capture loop:
//
//	DetectCorrection → Detection.ToCorrection → Entry.Correct → CommitCorrection
//
// The whole triple (closed, newer, edge) lands in one Append call, so the
// store receives them atomically: either all three persist or none do. This
// matches INV-2 (append-only) — CommitCorrection never splits the write across
// two Append calls and never reads back via Load.
//
// On success it returns the new head entry (newer) so callers can log or
// display what was written without reconstructing it. Callers that need to
// inspect closed or edge before writing should call Entry.Correct directly,
// then Append the triple themselves.
//
// Errors from Correct (invalid Correction input) are returned before the store
// is touched. Store errors are wrapped and returned; the triple was produced in
// memory but not persisted.
func CommitCorrection(ctx context.Context, store Store, old Entry, c Correction) (newer Entry, err error) {
	closed, newer, edge, err := old.Correct(c)
	if err != nil {
		return Entry{}, fmt.Errorf("commit correction: %w", err)
	}
	if err := store.Append(ctx, []Entry{closed, newer}, []Edge{edge}); err != nil {
		return Entry{}, fmt.Errorf("commit correction: persist: %w", err)
	}
	return newer, nil
}

// Correct captures a correction against an existing entry, producing the
// canonical supersession triple the consumption side already understands:
//
//   - closed: the old entry with ValidUntil = AsOf (flagged stale, content
//     untouched — INV-2 append-only; nothing is destroyed),
//   - newer:  a freshly minted current entry carrying the corrected content and
//     correction provenance in Extra, and
//   - edge:   a SUPERSEDES edge from newer to old.
//
// It is the inverse-companion of Entry.Supersede: Supersede is the low-level
// "wire these two together" primitive, while Correct is the high-level "I
// observed a correction, build everything" entry point that mints the new
// entry, derives inheritance/defaults, stamps provenance, and then delegates
// the wiring to Supersede so there is exactly one place that defines what a
// supersession looks like.
//
// The caller is responsible only for persisting all three returns (append the
// closed old entry, append newer, append the edge) via the Store port. Correct
// performs no I/O.
//
// Errors are returned (never panics) when the correction cannot be captured
// well-formed: a bad kind, empty content, an unslugifiable label, or a new
// entry that fails Validate. A correction that cannot be captured cleanly must
// fail loudly rather than silently leaving the stale entry to win.
func (old Entry) Correct(c Correction) (closed Entry, newer Entry, edge Edge, err error) {
	if !c.Kind.Valid() {
		return Entry{}, Entry{}, Edge{}, fmt.Errorf("correction kind: invalid %q", c.Kind)
	}
	if c.AsOf.IsZero() {
		return Entry{}, Entry{}, Edge{}, fmt.Errorf("correction: AsOf is required (must not be zero)")
	}
	// A correction must move forward in time relative to the entry it closes:
	// you cannot close an entry before it was encoded. This guards against
	// backwards captures that would make IsCurrent nonsensical.
	if c.AsOf.Before(old.EncodedTime) {
		return Entry{}, Entry{}, Edge{}, fmt.Errorf(
			"correction: AsOf %s precedes superseded entry's EncodedTime %s",
			c.AsOf.Format(time.RFC3339), old.EncodedTime.Format(time.RFC3339))
	}

	// Derive the new entry's metadata, inheriting from the superseded entry
	// where the caller left fields zero. A correction is, by default, the same
	// kind of thing about the same subjects as what it replaces.
	nt := c.Type
	if nt == "" {
		nt = old.Type
	}
	conf := c.Confidence
	if conf == "" {
		conf = ConfHigh // an explicit correction is high-confidence by construction
	}
	tags := c.Tags
	if tags == nil {
		tags = append([]string(nil), old.Tags...) // copy: don't alias old's slice
	}
	about := c.About
	if about == nil {
		about = append([]string(nil), old.About...)
	}

	id, err := NewID(c.AsOf, c.NewLabel)
	if err != nil {
		return Entry{}, Entry{}, Edge{}, fmt.Errorf("correction: new id: %w", err)
	}

	newer, err = NewEntry(NewEntryParams{
		ID:          id,
		Type:        nt,
		Content:     c.Content,
		EncodedTime: c.AsOf,
		EventTime:   c.EventTime,
		Confidence:  conf,
		ValidUntil:  nil, // the new entry is current (head of chain)
		Tags:        tags,
		About:       about,
	})
	if err != nil {
		return Entry{}, Entry{}, Edge{}, fmt.Errorf("correction: new entry: %w", err)
	}

	// Stamp provenance on the new entry so the correction is auditable and the
	// corpus can replay *why* the supersession happened. Written to Extra to
	// stay within the lossless round-trip contract without a schema change.
	newer.Extra[ExtraCorrectionKind] = string(c.Kind)
	newer.Extra[ExtraSupersededID] = string(old.ID)
	newer.Extra[ExtraCorrectionAsOf] = c.AsOf.Format(time.RFC3339)

	// Delegate the actual wiring to Supersede — the single source of truth for
	// what a supersession looks like (closes old at AsOf + emits the edge).
	closed, edge = old.Supersede(newer.ID, c.AsOf)

	return closed, newer, edge, nil
}
