// Package confirm assembles a detected correction into a ready-to-act
// Proposal: it pairs the pure-core sensor (core.DetectCorrection) with target
// resolution ("which held entry does this utterance supersede?") and produces a
// pure Proposal value. It does NOT decide whether to apply the correction and
// it does NOT present anything to a human.
//
// HISTORY / YAGNI NOTE. This package once contained a full synchronous
// human-in-the-loop approval surface: an Operator interface, a Decision type, a
// Confirmer loop driver (HandleText), a surfacing/auto-accept Policy, and a
// concrete TTYOperator that ran a 4-prompt terminal interview per correction.
// That machinery was removed (2026-06-26) after we concluded it solved a
// problem we do not actually have: corrections are already human-gated by the
// fact that a human states them in conversation, so a separate "approve each
// write at a terminal" ceremony was pure overhead. The real workflow is
// capture-then-notify with reversible writes (INV-2 makes every correction
// undoable via another supersession), which needs no synchronous approval seam.
// The deleted spine lives in git history and can be restored if a future
// validation step (replaying the real miss log through DetectCorrection) shows
// a confirmation gate is actually wanted. Until then: YAGNI.
//
// WHAT SURVIVES, and why it is load-bearing for any future path:
//
//	core.DetectCorrection(text) → core.Detection   (the reflex; sensor — in core)
//	    │  confirm.TargetResolver: find the stale entry/entries to supersede
//	    ▼
//	confirm.Proposal                                (a pure value: detection + targets + AsOf)
//
// A consumer of a Proposal (a capture-and-notify path, or an offline validation
// harness) turns it into a write via core.CommitCorrection. That consumer is a
// deliberate later decision, informed by validation, not built here on spec.
package confirm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// ---------------------------------------------------------------------------
// Target resolution — finding WHAT a correction supersedes
// ---------------------------------------------------------------------------

// TargetResolver finds the entry a detected correction is about: the stale
// thing to be superseded. It is a seam because "which held memory does this
// utterance correct?" is a retrieval question (it may consult a ranker, an
// embedding index, an explicit operator-supplied id, or just the single obvious
// candidate) and different deployments answer it differently. A resolver
// returns zero or more CURRENT candidates, best first.
//
// Returning more than one candidate is allowed and expected: ambiguity is a
// fact about the corpus, surfaced for a downstream consumer to handle, not a
// reason to guess here. Returning none means there is nothing to supersede.
type TargetResolver interface {
	// Resolve returns supersedable candidate entries for the detection, ordered
	// best-first. Implementations should return only entries that are CURRENT at
	// `now` (you cannot supersede an already-closed entry) and should never
	// return an error for "found nothing" — return an empty slice instead.
	Resolve(ctx context.Context, d core.Detection, now time.Time) ([]core.Entry, error)
}

// StoreResolver is the default TargetResolver: it loads the corpus from a
// core.Store, keeps only entries current at `now`, ranks them by Ensō
// retrieval strength (core.Rank), and returns the top candidates. It does NOT
// do content matching against the detection text — that is the NEIGHBOR-class
// concern deferred to a later stage — so callers in ambiguous corpora should
// expect multiple candidates.
//
// MaxCandidates caps how many are returned (0 → DefaultMaxCandidates). Capping
// keeps any downstream presentation readable; the strongest few are kept.
type StoreResolver struct {
	Store         core.Store
	MaxCandidates int
}

// DefaultMaxCandidates is the cap StoreResolver uses when MaxCandidates is 0.
const DefaultMaxCandidates = 5

// Resolve implements TargetResolver against a core.Store.
func (r StoreResolver) Resolve(ctx context.Context, _ core.Detection, now time.Time) ([]core.Entry, error) {
	entries, _, err := r.Store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("confirm: resolve target: load: %w", err)
	}
	current := make([]core.Entry, 0, len(entries))
	for _, e := range entries {
		if e.IsCurrent(now) {
			current = append(current, e)
		}
	}
	ranked := core.Rank(current, now)
	max := r.MaxCandidates
	if max <= 0 {
		max = DefaultMaxCandidates
	}
	out := make([]core.Entry, 0, max)
	for i := 0; i < len(ranked) && i < max; i++ {
		out = append(out, ranked[i].Entry)
	}
	return out, nil
}

// FixedResolver is a trivial TargetResolver that always returns the same
// candidates. Useful for explicit-target flows (the caller already named the
// entry) and for tests. It filters to current entries at `now` like any
// resolver should.
type FixedResolver struct {
	Candidates []core.Entry
}

// Resolve implements TargetResolver.
func (r FixedResolver) Resolve(_ context.Context, _ core.Detection, now time.Time) ([]core.Entry, error) {
	out := make([]core.Entry, 0, len(r.Candidates))
	for _, e := range r.Candidates {
		if e.IsCurrent(now) {
			out = append(out, e)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Proposal — the assembled, ready-to-act correction (a pure value)
// ---------------------------------------------------------------------------

// Proposal is a fully-assembled correction candidate: the sensor's detection
// paired with the resolved supersession target(s) and the AsOf instant. It is a
// pure value — building one writes nothing. A downstream consumer turns it into
// a write via core.CommitCorrection (supplying the human-owned label/content),
// or replays it offline to measure detection quality.
type Proposal struct {
	// Detection is the sensor's output (kind, confidence, signals, extracted
	// content). Carried verbatim for audit and for core.Detection.ToCorrection.
	Detection core.Detection

	// Candidates are the supersedable targets, best-first. Building a Proposal
	// with zero candidates is allowed (the resolver found nothing); consumers
	// check len(Candidates) and treat zero as "nothing to supersede."
	Candidates []core.Entry

	// AsOf is the capture instant a consumer should stamp on the correction.
	// Fixed at proposal time so the closed-old / new-current handoff is atomic
	// on a single known instant rather than drifting to whenever a consumer acts.
	AsOf time.Time
}

// Unambiguous reports whether exactly one target was resolved. A consumer can
// use this to skip a "which one?" disambiguation step.
func (p Proposal) Unambiguous() bool { return len(p.Candidates) == 1 }

// Summary renders a short, human-readable description of the proposal for a
// notification or log line. It names the kind, confidence, the fired signals,
// the proposed new content, and the target(s). Presentation only.
func (p Proposal) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Detected %s correction (confidence=%s)",
		p.Detection.Kind, p.Detection.Confidence)
	if len(p.Detection.Signals) > 0 {
		fmt.Fprintf(&b, " [signals: %s]", strings.Join(p.Detection.Signals, ", "))
	}
	b.WriteString("\n")
	if c := strings.TrimSpace(p.Detection.Content); c != "" {
		fmt.Fprintf(&b, "  proposed: %q\n", c)
	} else {
		b.WriteString("  proposed: (none extracted — a consumer must supply content)\n")
	}
	switch len(p.Candidates) {
	case 0:
		b.WriteString("  supersedes: (no target resolved)\n")
	case 1:
		fmt.Fprintf(&b, "  supersedes: %s — %q\n", p.Candidates[0].ID, truncate(p.Candidates[0].Content, 80))
	default:
		fmt.Fprintf(&b, "  supersedes one of %d candidates:\n", len(p.Candidates))
		for i, e := range p.Candidates {
			fmt.Fprintf(&b, "    [%d] %s — %q\n", i, e.ID, truncate(e.Content, 70))
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// Propose is the convenience that ties the surviving pieces together: detect a
// correction in text, resolve its supersession targets, and return a Proposal.
// It writes nothing. ok is false when the text carries no correction signal, so
// callers can cheaply skip non-corrections:
//
//	if prop, ok, err := confirm.Propose(ctx, resolver, line, now); ok && err == nil {
//	    // hand prop to a consumer (notify / commit / measure)
//	}
//
// A correction with zero resolved candidates still returns ok=true with an
// empty Candidates slice: detection succeeded, target resolution did not, and
// that distinction is exactly what a validation harness wants to measure.
func Propose(ctx context.Context, r TargetResolver, text string, now time.Time) (Proposal, bool, error) {
	det := core.DetectCorrection(text)
	if !det.IsCorrection {
		return Proposal{Detection: det}, false, nil
	}
	cands, err := r.Resolve(ctx, det, now)
	if err != nil {
		return Proposal{Detection: det}, true, err
	}
	return Proposal{Detection: det, Candidates: cands, AsOf: now}, true, nil
}
