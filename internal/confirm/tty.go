// Package confirm — TTYOperator: a concrete Operator that presents proposals
// on a terminal and reads decisions from stdin. This is the first real
// Operator impl; tests use scripted OperatorFunc fixtures instead.
//
// Design constraints inherited from the confirm surface:
//   - NewLabel is REQUIRED. The sensor cannot name the new entry; the human
//     does. The TTY operator prompts for it and retries on empty.
//   - Content override is OPTIONAL. Empty → use the detection's extracted
//     content. The prompt shows the extracted content as the default so the
//     operator can accept it with a bare return or retype to clean it up.
//   - TargetIndex is only prompted when the proposal is ambiguous (>1
//     candidate). Unambiguous proposals skip the step entirely.
//   - Abort / reject are distinct: Ctrl-D / EOF returns error (hard abort);
//     answering "n" at the confirmation gate returns Decision{Confirm:false}
//     (clean reject). This matches the Operator contract exactly.
package confirm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// TTYOperator presents a Proposal on a terminal (w) and reads a Decision from
// a reader (r). Inject os.Stdin/os.Stdout for the real terminal; inject
// bytes.Buffer / strings.Reader for tests that need a scripted TTY without
// touching the real OperatorFunc path.
//
// Zero value is not usable. Construct via NewTTYOperator.
type TTYOperator struct {
	in  *bufio.Reader
	out io.Writer
}

// NewTTYOperator returns a TTYOperator reading from r and writing to w.
// For the interactive terminal case: NewTTYOperator(os.Stdin, os.Stdout).
func NewTTYOperator(r io.Reader, w io.Writer) *TTYOperator {
	return &TTYOperator{in: bufio.NewReader(r), out: w}
}

// DefaultTTYOperator returns a TTYOperator wired to os.Stdin / os.Stdout.
// Convenience for the common case; exported so callers outside the package
// can construct one without importing os.
func DefaultTTYOperator() *TTYOperator {
	return NewTTYOperator(os.Stdin, os.Stdout)
}

// Decide implements Operator. It writes the proposal summary to tty.out and
// reads the decision from tty.in. ctx cancellation is checked between prompts
// and treated as a hard abort (returns ctx.Err()).
func (t *TTYOperator) Decide(ctx context.Context, p Proposal) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	// ── Present the detection ────────────────────────────────────────────────
	fmt.Fprintf(t.out, "\n┌─ Ensō: correction detected ───────────────────────────────────\n")
	fmt.Fprintf(t.out, "│  Kind:       %s\n", p.Detection.Kind)
	fmt.Fprintf(t.out, "│  Confidence: %s\n", p.Detection.Confidence)
	fmt.Fprintf(t.out, "│  Signals:    %s\n", strings.Join(p.Detection.Signals, ", "))
	if p.Detection.Content != "" {
		fmt.Fprintf(t.out, "│  Extracted:  %q\n", p.Detection.Content)
	} else {
		fmt.Fprintf(t.out, "│  Extracted:  (none — you must supply content)\n")
	}

	// ── Present candidates ───────────────────────────────────────────────────
	fmt.Fprintf(t.out, "│\n│  Candidate entries to supersede:\n")
	for i, c := range p.Candidates {
		age := ""
		if !c.EncodedTime.IsZero() {
			age = fmt.Sprintf(", %s old", roundAge(time.Since(c.EncodedTime)))
		}
		snip := c.Content
		if len(snip) > 72 {
			snip = snip[:69] + "…"
		}
		fmt.Fprintf(t.out, "│    [%d] %s%s\n│        %q\n", i, c.ID, age, snip)
	}
	fmt.Fprintf(t.out, "└───────────────────────────────────────────────────────────────\n")

	// ── Gate: proceed? ───────────────────────────────────────────────────────
	proceed, err := t.prompt(ctx, "Apply correction? [y/N] ")
	if err != nil {
		return Decision{}, err
	}
	if !strings.EqualFold(proceed, "y") && !strings.EqualFold(proceed, "yes") {
		return Decision{Confirm: false}, nil
	}

	var dec Decision
	dec.Confirm = true

	// ── Target index (only when ambiguous) ───────────────────────────────────
	if !p.Unambiguous() {
		for {
			if err := ctx.Err(); err != nil {
				return Decision{}, err
			}
			raw, err := t.prompt(ctx, fmt.Sprintf("Which candidate to supersede? [0-%d] ", len(p.Candidates)-1))
			if err != nil {
				return Decision{}, err
			}
			idx, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || idx < 0 || idx >= len(p.Candidates) {
				fmt.Fprintf(t.out, "  Please enter a number between 0 and %d.\n", len(p.Candidates)-1)
				continue
			}
			dec.TargetIndex = idx
			break
		}
	}

	// ── NewLabel (required) ───────────────────────────────────────────────────
	for {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}
		label, err := t.prompt(ctx, "Label for new entry (required, e.g. \"adam-headcount-jun25\"): ")
		if err != nil {
			return Decision{}, err
		}
		label = strings.TrimSpace(label)
		if label == "" {
			fmt.Fprintf(t.out, "  Label is required — the sensor cannot name this entry.\n")
			continue
		}
		dec.NewLabel = label
		break
	}

	// ── Content override (optional) ───────────────────────────────────────────
	defaultContent := p.Detection.Content
	promptContent := "Content override (return to accept extracted): "
	if defaultContent == "" {
		promptContent = "Content (required — no extraction available): "
	}
	for {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}
		raw, err := t.prompt(ctx, promptContent)
		if err != nil {
			return Decision{}, err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" && defaultContent == "" {
			fmt.Fprintf(t.out, "  Content is required when nothing was extracted.\n")
			continue
		}
		dec.Content = raw // empty → confirm.go uses detection.Content; that's fine
		break
	}

	return dec, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// prompt writes the prompt string and reads one line from tty.in. EOF/Ctrl-D
// returns io.EOF, which callers should surface as a hard abort.
func (t *TTYOperator) prompt(ctx context.Context, text string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fmt.Fprint(t.out, text)
	line, err := t.in.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			fmt.Fprintln(t.out) // tidy newline after Ctrl-D
			return "", io.EOF
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// roundAge returns a human-readable age string rounded to the nearest natural
// unit (minutes, hours, days). Used in the candidate listing.
func roundAge(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < 2*time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Compile-time proof that *TTYOperator satisfies Operator.
var _ Operator = (*TTYOperator)(nil)

// ── EventTime helper ─────────────────────────────────────────────────────────

// promptEventTime optionally asks for an event time ("when did this become
// true?"). Used for the reframe class where the real-world time often predates
// the stale belief. It is exported so callers that want richer prompting can
// call it after Decide returns and patch the EventTime field before passing
// the Decision to Confirm.
//
// Returns nil if the operator enters nothing or declines. Format: YYYY-MM-DD
// or "YYYY-MM-DD HH:MM" (local time). Keeps the TTYOperator surface small by
// not baking this into Decide — it is opt-in from the call site.
func (t *TTYOperator) PromptEventTime(ctx context.Context, kind core.CorrectionKind) (*time.Time, error) {
	if kind != core.CorrectReframe {
		return nil, nil // only reframes have a meaningful world-event time
	}
	raw, err := t.prompt(ctx, "When did this become true (YYYY-MM-DD or blank to skip)? ")
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04"} {
		if ts, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return &ts, nil
		}
	}
	fmt.Fprintf(t.out, "  Could not parse %q — skipping EventTime.\n", raw)
	return nil, nil
}
