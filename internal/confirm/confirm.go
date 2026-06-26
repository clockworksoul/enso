// Package confirm is the CONFIRM SURFACE of the staleness loop — the operator
// shell that sits between the pure-core sensor (core.DetectCorrection) and the
// pure-core commit path (core.CommitCorrection), and supplies the one thing
// neither of them is allowed to: a human decision.
//
// WHY THIS LIVES OUTSIDE core. core is deliberately decision-free. The sensor
// detects, the chokepoint commits, but nothing in core may choose to apply a
// correction, because the neurological-grounding analysis (2026-06-23) is blunt
// about the asymmetry: this architecture has no reconsolidation, so a written
// correction is the ONLY update path and a false-positive that rewrites a TRUE
// memory is permanent, uncorrectable corruption. The missed-vs-wrong asymmetry
// (a missed correction merely leaves a known-stale entry to lose later; a wrong
// auto-applied correction silently poisons ground truth) mandates a human in
// the loop. That human-in-the-loop POLICY is exactly what does not belong in a
// pure domain core — so it lives here, in the application layer, behind seams
// that keep it testable.
//
// THE LOOP THIS COMPLETES:
//
//	core.DetectCorrection(text) → Detection      (the reflex; sensor)
//	    │  confirm.Confirmer.Propose: gate by policy, resolve the target entry
//	    ▼
//	confirm.Proposal  ──present──▶  Operator      (the human seam)
//	    │  operator confirms + supplies AsOf / NewLabel / cleaned Content
//	    ▼
//	Detection.ToCorrection → Entry.Correct → core.CommitCorrection  (persist)
//
// Before this package the loop was inert end-to-end: the primitive existed but
// nothing pulled the trigger, so STALE entries kept winning. confirm is the
// trigger — under explicit human control.
package confirm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrNotACorrection is returned by Propose when the input text carries no
	// correction signal (Detection.IsCorrection == false). It is not a failure;
	// it is the common case (most lines are not corrections) and callers should
	// treat it as "nothing to do."
	ErrNotACorrection = errors.New("confirm: input is not a correction")

	// ErrBelowThreshold is returned by Propose when a correction was detected but
	// its confidence is below the policy's surfacing threshold. The detection is
	// preserved on the returned Proposal for logging/audit, but it is not
	// presented to the operator. This is the policy knob that keeps weak,
	// noisy signals from nagging a human on every "actually".
	ErrBelowThreshold = errors.New("confirm: detection below surfacing threshold")

	// ErrNoTarget is returned when a correction was detected and surfaced but no
	// stale entry could be resolved as the thing being corrected. A correction
	// with nothing to supersede cannot be committed (Correct needs an old entry);
	// the caller must either widen the candidate set or capture the corrected
	// statement as a fresh entry instead.
	ErrNoTarget = errors.New("confirm: no supersedable target entry resolved")

	// ErrRejected is returned by Confirm when the operator declines the proposal.
	// Nothing is written. This is a normal outcome, not an error condition in the
	// failure sense — the human looked and said no, which is the whole point of
	// the surface.
	ErrRejected = errors.New("confirm: operator rejected the proposal")
)

// ---------------------------------------------------------------------------
// Policy — the surfacing/auto-accept knobs (the part that must NOT be in core)
// ---------------------------------------------------------------------------

// Policy holds the operator-loop policy: which detections are worth a human's
// attention, and whether any may bypass the human. It is the deliberate home
// for the decisions core refuses to make.
type Policy struct {
	// MinConfidence is the lowest Detection confidence that gets surfaced to the
	// operator. Detections below it are dropped with ErrBelowThreshold. Default
	// (zero value) is DetectWeak, i.e. surface weak AND strong — when a human is
	// confirming everything anyway, a false alarm costs one keystroke, while a
	// dropped weak-but-real correction silently leaves a stale entry to win.
	MinConfidence core.DetectionConfidence

	// AutoAcceptStrong, if true, lets a STRONG detection be committed WITHOUT an
	// operator confirmation when (and only when) the target entry is
	// unambiguous (exactly one supersedable candidate) AND the operator-owned
	// fields can be derived without a human (NewLabel + Content non-empty).
	//
	// This is OFF by default and should stay off in any setting where a wrong
	// write is costlier than a missed one — which, per the no-reconsolidation
	// analysis, is the normal setting. It exists only for closed-loop pipelines
	// where an upstream system already vouched for the correction (e.g. an
	// explicit "/correct" operator command whose text IS the confirmation).
	// Even then it never fires on weak detections or ambiguous targets.
	AutoAcceptStrong bool
}

// DefaultPolicy returns the conservative default: surface weak-and-up to a
// human, never auto-accept. This is the policy the no-reconsolidation
// constraint argues for.
func DefaultPolicy() Policy {
	return Policy{MinConfidence: core.DetectWeak, AutoAcceptStrong: false}
}

// surfaces reports whether a detection at confidence c clears this policy's
// threshold. DetectNone never surfaces.
func (p Policy) surfaces(c core.DetectionConfidence) bool {
	min := p.MinConfidence
	if min == "" {
		min = core.DetectWeak
	}
	return confidenceRank(c) >= confidenceRank(min) && c != core.DetectNone
}

// confidenceRank mirrors core's internal ranking (strong > weak > none) so the
// policy can compare without reaching into core's unexported helper.
func confidenceRank(c core.DetectionConfidence) int {
	switch c {
	case core.DetectStrong:
		return 2
	case core.DetectWeak:
		return 1
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Target resolution — finding WHAT a correction supersedes (policy, not core)
// ---------------------------------------------------------------------------

// TargetResolver finds the entry a detected correction is about: the stale
// thing to be superseded. It is a seam, not core logic, because "which held
// memory does this utterance correct?" is a retrieval/policy question (it may
// consult a ranker, an embedding index, an explicit operator-supplied id, or
// just the single obvious candidate) and different deployments answer it
// differently. The confirm loop only requires that it return zero or more
// CURRENT candidates, best first.
//
// Returning more than one candidate is allowed and expected: ambiguity is a
// reason to involve the human, not to guess. Returning none yields ErrNoTarget.
type TargetResolver interface {
	// Resolve returns supersedable candidate entries for the detection, ordered
	// best-first. Implementations should return only entries that are CURRENT at
	// `now` (you cannot supersede an already-closed entry) and should never
	// return an error for "found nothing" — return an empty slice instead, so
	// ErrNoTarget is raised at one place in the loop.
	Resolve(ctx context.Context, d core.Detection, now time.Time) ([]core.Entry, error)
}

// StoreResolver is the default TargetResolver: it loads the corpus from a
// core.Store, keeps only entries current at `now`, ranks them by Ensō
// retrieval strength (core.Rank), and returns the top candidates. It does NOT
// do content matching against the detection text — that is the NEIGHBOR-class
// concern deferred to a later stage — so callers in ambiguous corpora should
// expect multiple candidates and lean on the operator to choose.
//
// MaxCandidates caps how many are surfaced (0 → DefaultMaxCandidates). Capping
// keeps the operator prompt readable; the human picks from the strongest few.
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
// candidates. Useful for explicit-target flows (the operator already named the
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
// Proposal — what gets presented to the human
// ---------------------------------------------------------------------------

// Proposal is the fully-assembled, ready-to-confirm correction the surface
// presents to an Operator. It bundles the sensor's detection with the resolved
// supersession target(s) and the AsOf instant, leaving only the human-owned
// choices (yes/no, which target, the new label, content cleanup) outstanding.
//
// A Proposal is a pure value — building one writes nothing. Only Confirm, after
// the operator says yes, touches the store.
type Proposal struct {
	// Detection is the sensor's output (kind, confidence, signals, extracted
	// content). Carried verbatim for audit and for ToCorrection.
	Detection core.Detection

	// Candidates are the supersedable targets, best-first. Len ≥ 1 (a Proposal
	// is never built with zero targets — that path returns ErrNoTarget instead).
	Candidates []core.Entry

	// AsOf is the capture instant the loop will stamp on the correction. Fixed
	// at proposal time so the closed-old / new-current handoff is atomic on a
	// single, known instant rather than drifting to whenever the operator
	// happens to hit enter.
	AsOf time.Time
}

// Unambiguous reports whether exactly one target was resolved. Used by the
// auto-accept guard and to let an Operator skip the "which one?" question.
func (p Proposal) Unambiguous() bool { return len(p.Candidates) == 1 }

// Summary renders a short, human-readable description of the proposal for a
// prompt or log line. It names the kind, confidence, the fired signals, the
// proposed new content, and the best target. It is presentation only.
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
		b.WriteString("  proposed: (none extracted — operator must supply content)\n")
	}
	if len(p.Candidates) == 1 {
		fmt.Fprintf(&b, "  supersedes: %s — %q\n", p.Candidates[0].ID, truncate(p.Candidates[0].Content, 80))
	} else {
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

// ---------------------------------------------------------------------------
// Operator — the human seam
// ---------------------------------------------------------------------------

// Decision is the operator's answer to a Proposal. It is the human-owned half
// of the correction that core deliberately refuses to synthesize.
type Decision struct {
	// Confirm is the gate. If false, nothing is written (Confirm returns
	// ErrRejected). Everything below is ignored when Confirm is false.
	Confirm bool

	// TargetIndex selects which Proposal.Candidate is being superseded. Ignored
	// when the proposal is unambiguous. Out-of-range is a confirm-time error.
	TargetIndex int

	// NewLabel seeds the new entry's ID slug (core.NewID). REQUIRED on confirm —
	// the sensor cannot name the entry; the human does. Empty → confirm-time
	// error, so we never mint a junk-labelled correction.
	NewLabel string

	// Content optionally overrides the detection's extracted content (lets the
	// operator clean up an imperfect extraction). Empty → the detection's
	// Content is used. If BOTH are empty, Confirm fails: a correction with no
	// content cannot supersede anything meaningfully.
	Content string

	// EventTime optionally records when the corrected fact became true in the
	// world (distinct from AsOf = when we captured it). Important for the
	// reframe class, where the corrected fact is often older than the stale
	// belief by world-time. nil leaves it unset.
	EventTime *time.Time
}

// Operator is the human (or UI, or scripted-test) seam: it is shown a Proposal
// and returns a Decision. This is the ONLY interface in the loop that embodies
// a choice, which is exactly why it is an injected dependency and not core
// logic. Real deployments back it with a TTY prompt, a chat confirmation
// button, or a slash-command handler; tests back it with a scripted decision.
type Operator interface {
	// Decide presents the proposal and returns the operator's decision. Returning
	// an error aborts the confirmation (distinct from a Decision{Confirm:false},
	// which is a clean "no"); use the error path for I/O failures or operator
	// abort, the Confirm:false path for "I looked and declined."
	Decide(ctx context.Context, p Proposal) (Decision, error)
}

// OperatorFunc adapts a plain function to the Operator interface.
type OperatorFunc func(ctx context.Context, p Proposal) (Decision, error)

// Decide implements Operator.
func (f OperatorFunc) Decide(ctx context.Context, p Proposal) (Decision, error) {
	return f(ctx, p)
}

// ---------------------------------------------------------------------------
// Confirmer — the loop driver
// ---------------------------------------------------------------------------

// Confirmer drives the confirm surface end to end. It wires the sensor output
// to target resolution, applies policy, presents to the operator, and commits
// through the core chokepoint. It holds the dependencies (store, resolver,
// operator, policy, clock) and no mutable per-call state, so a single Confirmer
// is reusable and safe to share.
type Confirmer struct {
	Store    core.Store
	Resolver TargetResolver
	Operator Operator
	Policy   Policy

	// Now supplies the capture instant. Injected for deterministic tests; nil →
	// time.Now. The SAME instant is used for staleness filtering, ranking, and
	// the AsOf stamp within one HandleText call, so the proposal is internally
	// consistent.
	Now func() time.Time
}

// New builds a Confirmer with the default policy and a StoreResolver over the
// given store, committing through that same store. This is the common wiring;
// callers needing a custom resolver/policy/clock can construct the struct
// directly.
func New(store core.Store, op Operator) *Confirmer {
	return &Confirmer{
		Store:    store,
		Resolver: StoreResolver{Store: store},
		Operator: op,
		Policy:   DefaultPolicy(),
	}
}

func (c *Confirmer) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Result reports the outcome of a HandleText call. Exactly one of the three
// states holds: Committed (a correction was written, NewHead/Superseded set),
// or not committed with a reason in Err (ErrNotACorrection / ErrBelowThreshold
// / ErrNoTarget / ErrRejected), or a hard failure in Err. Detection is always
// populated (even when no correction) so callers can log every scanned line.
type Result struct {
	// Detection is the sensor output for the input line, always set.
	Detection core.Detection

	// Proposal is the assembled proposal, set whenever one was built (i.e. the
	// detection surfaced and a target resolved), regardless of the operator's
	// answer. Nil when the loop short-circuited before building one.
	Proposal *Proposal

	// Committed is true iff a correction was persisted.
	Committed bool

	// NewHead is the new current entry, set iff Committed.
	NewHead core.Entry

	// Superseded is the entry that was closed, set iff Committed.
	Superseded core.Entry

	// AutoAccepted is true iff the commit bypassed the operator via policy.
	AutoAccepted bool
}

// HandleText is the whole surface in one call: scan a free-form line, and if it
// is a surfaceable correction with a resolvable target, present it for
// confirmation and (on yes) commit it. It is safe to call on every line of a
// conversation; non-corrections and below-threshold lines return cheaply with a
// sentinel error and write nothing.
//
// Control flow, in order:
//  1. DetectCorrection. Not a correction → ErrNotACorrection.
//  2. Policy gate. Below threshold → ErrBelowThreshold (detection preserved).
//  3. Resolve targets. None → ErrNoTarget.
//  4. Build the Proposal (pure; nothing written yet).
//  5. Auto-accept guard (off by default; strong + unambiguous + derivable only).
//  6. Otherwise present to the Operator; on Confirm:false → ErrRejected.
//  7. Commit via core.CommitCorrection (the single persist chokepoint).
//
// The returned Result always carries the Detection; Err mirrors the sentinel
// for the non-committed outcomes so callers can branch on errors.Is.
func (c *Confirmer) HandleText(ctx context.Context, text string) (Result, error) {
	now := c.now()

	det := core.DetectCorrection(text)
	res := Result{Detection: det}

	// (1) Not a correction at all.
	if !det.IsCorrection {
		return res, ErrNotACorrection
	}

	// (2) Policy surfacing gate.
	if !c.Policy.surfaces(det.Confidence) {
		return res, ErrBelowThreshold
	}

	// (3) Resolve supersession targets.
	cands, err := c.Resolver.Resolve(ctx, det, now)
	if err != nil {
		return res, err
	}
	if len(cands) == 0 {
		return res, ErrNoTarget
	}

	// (4) Build the proposal (pure).
	prop := Proposal{Detection: det, Candidates: cands, AsOf: now}
	res.Proposal = &prop

	// (5) Auto-accept guard. Tightly constrained: strong confidence, exactly one
	// target, and the operator-owned fields derivable without a human (a label
	// from the detected content + non-empty content). Off unless policy opts in.
	if c.Policy.AutoAcceptStrong && det.Confidence == core.DetectStrong && prop.Unambiguous() {
		if label, content, ok := deriveAutoFields(det); ok {
			dec := Decision{Confirm: true, NewLabel: label, Content: content}
			out, err := c.commit(ctx, prop, dec)
			if err != nil {
				return res, err
			}
			out.AutoAccepted = true
			out.Detection = det
			out.Proposal = &prop
			return out, nil
		}
		// Not derivable → fall through to the human. Never guess a label.
	}

	// (6) Present to the operator.
	dec, err := c.Operator.Decide(ctx, prop)
	if err != nil {
		return res, fmt.Errorf("confirm: operator: %w", err)
	}
	if !dec.Confirm {
		return res, ErrRejected
	}

	// (7) Commit.
	out, err := c.commit(ctx, prop, dec)
	if err != nil {
		return res, err
	}
	out.Detection = det
	out.Proposal = &prop
	return out, nil
}

// commit validates the operator decision against the proposal, assembles the
// Correction via the core seam, and persists it through core.CommitCorrection.
// It is the one place that turns a confirmed Decision into a write.
func (c *Confirmer) commit(ctx context.Context, p Proposal, d Decision) (Result, error) {
	idx := d.TargetIndex
	if p.Unambiguous() {
		idx = 0
	}
	if idx < 0 || idx >= len(p.Candidates) {
		return Result{}, fmt.Errorf("confirm: target index %d out of range [0,%d)", idx, len(p.Candidates))
	}
	target := p.Candidates[idx]

	if strings.TrimSpace(d.NewLabel) == "" {
		return Result{}, fmt.Errorf("confirm: NewLabel is required to commit a correction")
	}
	// Content must come from somewhere: the operator's override or the detection.
	if strings.TrimSpace(d.Content) == "" && strings.TrimSpace(p.Detection.Content) == "" {
		return Result{}, fmt.Errorf("confirm: no content (detection extracted none and operator supplied none)")
	}

	corr := p.Detection.ToCorrection(p.AsOf, d.NewLabel, d.Content)
	if d.EventTime != nil {
		corr.EventTime = d.EventTime
	}

	newHead, err := core.CommitCorrection(ctx, c.Store, target, corr)
	if err != nil {
		return Result{}, err // already wrapped by core
	}
	// Reconstruct the closed view of the target for the Result (Correct closes it
	// at AsOf; we mirror that here for reporting without re-reading the store).
	closed := target
	closed.ValidUntil = &p.AsOf

	return Result{
		Committed:  true,
		NewHead:    newHead,
		Superseded: closed,
	}, nil
}

// deriveAutoFields produces a (label, content) pair for the auto-accept path
// from a detection alone, returning ok=false if it cannot do so safely. It
// never invents content: if the detection extracted none, auto-accept is
// refused (the loop falls back to the human). The label is a conservative slug
// seed derived from the content's leading words; core.NewID does the real
// slugification and will reject anything unusable, which is the final guard.
func deriveAutoFields(d core.Detection) (label, content string, ok bool) {
	content = strings.TrimSpace(d.Content)
	if content == "" {
		return "", "", false
	}
	// Seed the label from the first few words of the corrected statement.
	words := strings.Fields(content)
	if len(words) > 6 {
		words = words[:6]
	}
	label = strings.Join(words, " ")
	return label, content, true
}
