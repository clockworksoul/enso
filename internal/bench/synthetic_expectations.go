package bench

// synthetic_expectations.go is the loader for the SYNTHETIC detector-expectation
// corpus. It is the deliberate counterpart to the REAL corpus (cases.go,
// held_out_cases.go) and exists to unblock the corpus bottleneck WITHOUT eroding
// what makes Ensō's claims trustworthy.
//
// # The methodology (agreed with Matt, 2026-06-30)
//
// Synthetic data is a FALSIFIABLE BEHAVIORAL SPEC, never validation and never
// discovery:
//
//   - It ASSERTS expectations ("I expect the detector to fire on this paraphrase")
//     and is graded against reality, not the other way around. A passing
//     synthetic suite is a REGRESSION GUARANTEE ("the detector does what I told
//     it to"), NOT a correctness certificate ("the detector handles real
//     corrections"). The latter is only earnable from the REAL corpus.
//
//   - It does NO discovery. Every real Phase-0 miss was a surprise (FABRICATION,
//     reframe-vs-restate, the bare-assertion gap). You cannot discover failure
//     modes you can only imagine. Synthetic cases sample the LINGUISTIC SURFACE
//     of modes already CONFIRMED real (the detector / bare-corrective-assertion
//     class, seam #0) — augmentation of a known signal, not fabrication of
//     unknown ones.
//
//   - It follows a synthetic-prior → real-posterior lifecycle: seed with a
//     best-guess spec; every real example that arrives either confirms a
//     synthetic case (graduate it) or CONTRADICTS it (the high-value event —
//     reality teaching us the spec was wrong).
//
// # Hard structural separation (enforced by code, not discipline)
//
// These cases live in a SEPARATE corpus, are ALWAYS labeled synthetic, and are
// STRUCTURALLY INCAPABLE of touching the RealCases precision@1 scoreboard. The
// real metric (bench_test.go: Run/RunQueryAware over SeedCases/NeighborCases/
// HeldOutStaleCases) never sees these. They run only in
// synthetic_expectations_test.go.
//
// # Why text, not Go literals
//
// Unlike the real STALE fixtures (which CONSTRUCT themselves by invoking real
// domain logic — core.Entry.Correct(), SUPERSEDES edges, relative timestamps —
// and are therefore genuinely code-shaped), a synthetic detector expectation is
// INERT: an utterance string and a wantFire bool. Inert data belongs in data.
// The JSONL corpus is human-authored, diffable, and editable without recompiling,
// which is exactly what the steadily-growing, reconcile-as-reality-arrives
// pattern needs. (The real corpus stays in Go for now; see the 2026-06-30
// discussion. Reversible if it ever outgrows literals.)

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpectationStatus is the lifecycle stage of a synthetic expectation.
//
//	aspirational — a TARGET. The detector is expected (someday) to meet it, but
//	               whether it does today is REPORTED, not gated. Misses are a
//	               future-vocabulary to-do list, not test failures.
//	locked       — a REGRESSION LOCK. The detector already meets it, so it is now
//	               hard-asserted: any future change that un-meets it FAILS.
//
// A case graduates aspirational → locked by flipping this field once the
// vocabulary actually covers it (a one-line data edit in the JSONL).
type ExpectationStatus string

const (
	StatusAspirational ExpectationStatus = "aspirational"
	StatusLocked       ExpectationStatus = "locked"
)

// SyntheticExpectation is one authored behavioral assertion about the detector.
// It is INTENTIONALLY flat (no core.Entry / core.Edge) — that flatness is the
// whole reason it can live in text. Provenance is synthetic by construction.
type SyntheticExpectation struct {
	// Intent is a short class label, e.g. "restate:scope-expansion" or
	// "negative:hypothetical". Groups cases by the correction-intent they probe.
	Intent string `json:"intent"`

	// Utterance is the authored sentence fed to core.DetectCorrection.
	Utterance string `json:"utterance"`

	// WantFire is the asserted expectation: true = the detector SHOULD recognize
	// a correction; false = it should stay quiet (a NEGATIVE / false-positive
	// probe). Negatives are always safe to gate immediately — over-firing is
	// unambiguously a regression — so the test hard-asserts every WantFire:false
	// case regardless of Status.
	WantFire bool `json:"wantFire"`

	// Status is the aspirational/locked lifecycle stage (see ExpectationStatus).
	Status ExpectationStatus `json:"status"`

	// Note explains WHY this case exists / what boundary it probes. Authoring
	// rationale, kept in-band so the corpus is self-documenting.
	Note string `json:"note"`
}

// syntheticCorpusPath is the JSONL corpus location, relative to this package dir.
const syntheticCorpusPath = "testdata/synthetic_expectations.jsonl"

// LoadSyntheticExpectations parses the JSONL corpus. It is strict on purpose:
// a malformed line is a hard error, because a silently-dropped expectation is a
// silently-disabled assertion — the exact failure mode the whole bucket exists
// to prevent. Blank lines and #-prefixed comment lines are skipped.
func LoadSyntheticExpectations() ([]SyntheticExpectation, error) {
	f, err := os.Open(syntheticCorpusPath)
	if err != nil {
		// Fall back to an absolute path derived from this source file's dir so
		// the loader works regardless of the test's working directory. (go test
		// runs with CWD = package dir, so the relative path normally suffices;
		// this is belt-and-suspenders.)
		abs := filepath.Join("testdata", "synthetic_expectations.jsonl")
		f, err = os.Open(abs)
		if err != nil {
			return nil, fmt.Errorf("open synthetic corpus %q: %w", syntheticCorpusPath, err)
		}
	}
	defer f.Close()

	var out []SyntheticExpectation
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		var e SyntheticExpectation
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, fmt.Errorf("synthetic corpus line %d: %w", line, err)
		}
		if err := e.validate(line); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan synthetic corpus: %w", err)
	}
	return out, nil
}

// validate rejects a malformed expectation at load time, so a typo never becomes
// a silently-skipped assertion.
func (e SyntheticExpectation) validate(line int) error {
	if strings.TrimSpace(e.Utterance) == "" {
		return fmt.Errorf("synthetic corpus line %d: empty utterance", line)
	}
	if strings.TrimSpace(e.Intent) == "" {
		return fmt.Errorf("synthetic corpus line %d: empty intent", line)
	}
	switch e.Status {
	case StatusAspirational, StatusLocked:
	default:
		return fmt.Errorf("synthetic corpus line %d: invalid status %q (want %q or %q)",
			line, e.Status, StatusAspirational, StatusLocked)
	}
	return nil
}
