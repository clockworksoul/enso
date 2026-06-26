package confirm

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// makeProposal builds a minimal Proposal with n candidates for TTY tests.
func makeProposal(n int) Proposal {
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	cands := make([]core.Entry, n)
	for i := range cands {
		t := base.Add(-time.Duration(i+1) * 24 * time.Hour)
		id, _ := core.NewID(t, "candidate")
		cands[i] = core.Entry{
			ID:          id,
			Content:     "Adam is aligned on net-new hire for Axon.",
			EncodedTime: t,
		}
	}
	return Proposal{
		Detection: core.Detection{
			IsCorrection: true,
			Kind:         core.CorrectRestate,
			Confidence:   core.DetectStrong,
			Signals:      []string{"actually-now"},
			Content:      "Adam prefers an internal move, not net-new.",
		},
		Candidates: cands,
	}
}

// lines joins strings with newlines so it reads like the human's keystrokes.
func lines(ss ...string) io.Reader {
	return strings.NewReader(strings.Join(ss, "\n") + "\n")
}

// TestTTYOperator_HappyPath_Unambiguous exercises the common path: one
// candidate, operator answers y, provides a label, accepts extracted content.
func TestTTYOperator_HappyPath_Unambiguous(t *testing.T) {
	p := makeProposal(1) // unambiguous → no TargetIndex prompt
	input := lines(
		"y",                         // proceed?
		"adam-headcount-internal",   // NewLabel
		"",                          // content override: accept extracted
	)
	var out bytes.Buffer
	op := NewTTYOperator(input, &out)

	dec, err := op.Decide(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Confirm {
		t.Fatal("expected Confirm=true")
	}
	if dec.NewLabel != "adam-headcount-internal" {
		t.Errorf("NewLabel = %q, want %q", dec.NewLabel, "adam-headcount-internal")
	}
	if dec.Content != "" {
		// empty → confirm.go uses detection.Content, which is what we want
		t.Errorf("Content = %q, want empty (use extracted)", dec.Content)
	}
	if dec.TargetIndex != 0 {
		t.Errorf("TargetIndex = %d, want 0", dec.TargetIndex)
	}

	// output should mention the detection kind and extracted content
	rendered := out.String()
	if !strings.Contains(rendered, string(core.CorrectRestate)) {
		t.Errorf("output missing kind %q", core.CorrectRestate)
	}
	if !strings.Contains(rendered, "Adam prefers an internal move") {
		t.Errorf("output missing extracted content")
	}
}

// TestTTYOperator_Reject verifies that answering "n" at the gate produces
// Decision{Confirm:false} with no error (clean reject, not hard abort).
func TestTTYOperator_Reject(t *testing.T) {
	p := makeProposal(1)
	input := lines("n")
	var out bytes.Buffer
	op := NewTTYOperator(input, &out)

	dec, err := op.Decide(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error on reject: %v", err)
	}
	if dec.Confirm {
		t.Fatal("expected Confirm=false after 'n'")
	}
}

// TestTTYOperator_EOF_IsHardAbort verifies that Ctrl-D (EOF) surfaces as an
// error (hard abort), not as a clean Decision{Confirm:false}.
func TestTTYOperator_EOF_IsHardAbort(t *testing.T) {
	p := makeProposal(1)
	input := strings.NewReader("") // immediate EOF
	var out bytes.Buffer
	op := NewTTYOperator(input, &out)

	_, err := op.Decide(context.Background(), p)
	if err == nil {
		t.Fatal("expected error on EOF, got nil")
	}
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

// TestTTYOperator_Ambiguous_TargetIndex verifies that ambiguous proposals
// prompt for a target index and record the choice.
func TestTTYOperator_Ambiguous_TargetIndex(t *testing.T) {
	p := makeProposal(3) // 3 candidates → ambiguous
	input := lines(
		"y",              // proceed?
		"1",              // pick candidate index 1
		"adam-jun25",     // NewLabel
		"Overridden.",    // content override
	)
	var out bytes.Buffer
	op := NewTTYOperator(input, &out)

	dec, err := op.Decide(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Confirm {
		t.Fatal("expected Confirm=true")
	}
	if dec.TargetIndex != 1 {
		t.Errorf("TargetIndex = %d, want 1", dec.TargetIndex)
	}
	if dec.NewLabel != "adam-jun25" {
		t.Errorf("NewLabel = %q, want %q", dec.NewLabel, "adam-jun25")
	}
	if dec.Content != "Overridden." {
		t.Errorf("Content = %q, want %q", dec.Content, "Overridden.")
	}
}

// TestTTYOperator_EmptyLabelRetried verifies that an empty label triggers a
// re-prompt (operator must supply a non-empty label).
func TestTTYOperator_EmptyLabelRetried(t *testing.T) {
	p := makeProposal(1)
	input := lines(
		"y",          // proceed?
		"",           // first label attempt: empty → retry
		"real-label", // second attempt: accepted
		"",           // content override: accept extracted
	)
	var out bytes.Buffer
	op := NewTTYOperator(input, &out)

	dec, err := op.Decide(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.NewLabel != "real-label" {
		t.Errorf("NewLabel = %q, want %q", dec.NewLabel, "real-label")
	}
	if !strings.Contains(out.String(), "Label is required") {
		t.Error("expected 'Label is required' in output after empty label")
	}
}

// TestTTYOperator_ContextCancelled verifies that a cancelled context aborts
// the operator between prompts.
func TestTTYOperator_ContextCancelled(t *testing.T) {
	p := makeProposal(1)
	// Provide enough input that we'd reach the label prompt without cancelling,
	// but cancel the context before calling Decide.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	input := lines("y", "some-label", "")
	var out bytes.Buffer
	op := NewTTYOperator(input, &out)

	_, err := op.Decide(ctx, p)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// TestRoundAge spot-checks the age formatter.
func TestRoundAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "90s"},
		{2 * time.Minute, "2m"},
		{90 * time.Minute, "90m"},
		{2 * time.Hour, "2h"},
		{47 * time.Hour, "47h"},
		{48 * time.Hour, "2d"},
		{72 * time.Hour, "3d"},
	}
	for _, tc := range cases {
		got := roundAge(tc.d)
		if got != tc.want {
			t.Errorf("roundAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
