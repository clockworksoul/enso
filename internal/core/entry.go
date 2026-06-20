package core

import "time"

// NewEntryParams are the inputs to NewEntry. Optional/nullable fields are
// pointers so the caller can express explicit `null` (a known-unknown) versus
// "use the default."
type NewEntryParams struct {
	ID          ID
	Type        NodeType
	Content     string
	EncodedTime time.Time
	Confidence  Confidence

	// Optional. nil means explicit null per the grammar (tech spec §3.1).
	EventTime  *time.Time
	ValidFrom  *time.Time
	ValidUntil *time.Time

	Tags  []string
	About []string
}

// NewEntry constructs a validated Entry with the Phase-3 reserved temporal
// fields initialized per the schema defaults (tech spec §6), guaranteeing the
// invariants the rest of the system relies on:
//
//   - Tags/About are non-nil (the "key must be present" contract, §3.2).
//   - The reserved decay fields are written from day one (§3.2 — no backfill).
//   - LastRefTime == EncodedTime at creation (§6 init).
//
// It returns an error if the resulting Entry fails Validate(), so a constructed
// Entry is always well-formed.
func NewEntry(p NewEntryParams) (Entry, error) {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	about := p.About
	if about == nil {
		about = []string{}
	}

	tmp := DefaultTemporal(p.Type)
	tmp.LastRefTime = p.EncodedTime // init = encoded_time (tech spec §6)

	e := Entry{
		ID:          p.ID,
		Type:        p.Type,
		Content:     p.Content,
		EncodedTime: p.EncodedTime,
		EventTime:   p.EventTime,
		ValidFrom:   p.ValidFrom,
		ValidUntil:  p.ValidUntil,
		Confidence:  p.Confidence,
		Tags:        tags,
		About:       about,
		Temporal:    tmp,
		Extra:       map[string]string{},
	}
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Supersede records that newer supersedes this entry as of supersededAt,
// following the supersession convention (tech spec §3.3) WITHOUT mutating the
// old entry's content (INV-2: append-only; nothing destroyed). It returns:
//
//   - the old entry with ValidUntil set to supersededAt (flagged stale), and
//   - a SUPERSEDES edge from the new entry to the old entry.
//
// The caller is responsible for having already written `newer` and for
// persisting both the closed old entry and the returned edge. Supersede never
// edits `content`; staleness is expressed purely via valid_until + the edge.
//
// Returning a copy (value receiver) keeps the operation non-destructive at the
// API level too: the original value the caller holds is unchanged unless they
// adopt the returned copy.
func (e Entry) Supersede(newer ID, supersededAt time.Time) (closed Entry, edge Edge) {
	closed = e
	t := supersededAt
	closed.ValidUntil = &t

	edge = Edge{
		From:  newer,
		Type:  EdgeSupersedes,
		To:    string(e.ID),
		Extra: map[string]string{},
	}
	return closed, edge
}
