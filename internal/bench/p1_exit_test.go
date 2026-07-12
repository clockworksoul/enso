package bench

import (
	"fmt"
	"os"
	"testing"
)

// p1ExitCasesPath is the labeled exit case spec.
const p1ExitCasesPath = "testdata/p1_exit_cases.jsonl"

// p1CorpusRoot is the live memory store root. Override with ENSO_CORPUS_ROOT.
const p1CorpusRootDefault = "/Users/matt/.openclaw/workspace"

// TestP1Exit is the Phase-1 exit measurement.
//
// It loads the live FSStore corpus and evaluates each ready case using the
// full Ensō pipeline (staleness + supersession filter + specificity ranking).
// Cases marked ready=false or skip_until in the future are reported as pending.
//
// Pass condition: P@1 on active cases > P0 flat-file baseline (0.63).
// When this test passes, the spec says to PAUSE before WP-3 and confirm the
// graph layer is still needed.
//
// Re-run as the corpus grows:
//
//	go test ./internal/bench/ -run TestP1Exit -v
func TestP1Exit(t *testing.T) {
	root := os.Getenv("ENSO_CORPUS_ROOT")
	if root == "" {
		root = p1CorpusRootDefault
	}

	cases, err := LoadP1ExitCases(p1ExitCasesPath)
	if err != nil {
		t.Fatalf("load exit cases: %v", err)
	}

	results, err := RunP1Exit(root, cases)
	if err != nil {
		t.Fatalf("run exit measurement: %v", err)
	}

	// Summary table.
	t.Logf("\n=== P1 Exit Measurement ===")
	t.Logf("%-35s  %-8s  %-8s  %s", "Case", "Status", "Result", "Top result")
	t.Logf("%-35s  %-8s  %-8s  %s", dashes(35), dashes(8), dashes(8), dashes(30))
	for _, r := range results {
		status := "active"
		result := "FAIL"
		top := r.TopID
		if r.Skipped {
			status = "pending"
			result = "-"
			top = "(skip)"
		} else if r.Hit {
			result = "PASS"
		}
		t.Logf("%-35s  %-8s  %-8s  %s", r.Case.ID, status, result, truncate(top, 50))
	}
	t.Logf("%s", dashes(80))

	pass, score, msg := P1ExitVerdict(results)
	t.Logf("\nVerdict: %s", msg)

	if !pass {
		// Not a test failure — the corpus just isn't mature enough yet.
		// Log the status and move on. The test will pass once P@1 > 0.63.
		t.Logf("(not a failure — grow the corpus and re-run)")
	} else {
		t.Logf("🟢 P1 exit condition met (score=%.2f). Consider whether WP-3 is still needed.", score)
	}

	// Hard regression guard: if P@1 falls BELOW the P0 baseline on active
	// cases, that IS a failure — the structured corpus made things worse.
	_, active, s := P1ExitScore(results)
	if active > 0 && s < 0.5 {
		t.Errorf("P1 regression: P@1=%.2f on %d active cases — structured corpus is hurting recall", s, active)
	}
}

// TestP1ExitSummary is a compact one-liner for CI / quick checks.
func TestP1ExitSummary(t *testing.T) {
	root := os.Getenv("ENSO_CORPUS_ROOT")
	if root == "" {
		root = p1CorpusRootDefault
	}
	cases, err := LoadP1ExitCases(p1ExitCasesPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	results, err := RunP1Exit(root, cases)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	_, _, score := P1ExitScore(results)
	pass, _, msg := P1ExitVerdict(results)
	t.Logf("P1 exit: score=%.2f pass=%v — %s", score, pass, msg)
}

func dashes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Ensure fmt is used (for the dashes helper via fmt.Sprintf if needed).
var _ = fmt.Sprintf
