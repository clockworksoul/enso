package mdstore

import (
	"strings"
	"testing"
)

// TestParse_SkipsProse verifies the inline requirement (§3.5a): structured
// blocks interleaved with prose daily-note text parse correctly, prose ignored.
func TestParse_SkipsProse(t *testing.T) {
	doc := `# 2026-06-20 Daily Notes

Some freeform prose about the day. Worked on Ensō Stage 2.

### mem:2026-06-20-stage2
- type: Decision
- content: ratified inline structured blocks
- encoded_time: 2026-06-20T18:30:00Z
- event_time: null
- valid_from: null
- valid_until: null
- confidence: high
- tags: [enso, architecture]
- about: [project:enso]
- last_ref_time: 2026-06-20T18:30:00Z
- S_last: 1
- S_floor: 0.1
- lambda: 0.05
- S_cap: 1

More prose afterward. Then dinner.

## Research
- a bullet that is NOT a structured block (no ### header above it)
`
	entries, edges, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if len(edges) != 0 {
		t.Fatalf("want 0 edges, got %d", len(edges))
	}
	if entries[0].ID != "mem:2026-06-20-stage2" {
		t.Errorf("unexpected id %q", entries[0].ID)
	}
}

func TestParse_LoudOnMissingRequiredKey(t *testing.T) {
	// Missing `confidence` (required, §3.2).
	doc := `### mem:2026-06-20-bad
- type: Fact
- content: incomplete
- encoded_time: 2026-06-20T18:30:00Z
- tags: []
`
	_, _, err := Parse(doc)
	if err == nil {
		t.Fatal("expected loud error for missing required key")
	}
	if !strings.Contains(err.Error(), "confidence") {
		t.Errorf("error should name the missing key: %v", err)
	}
}

func TestParse_LoudOnMalformedLine(t *testing.T) {
	doc := `### mem:2026-06-20-bad
- type: Fact
this line is not a property and not blank
- content: x
`
	_, _, err := Parse(doc)
	if err == nil {
		t.Fatal("expected loud error for malformed line inside block")
	}
	var pe *ParseError
	if !asParseError(err, &pe) {
		t.Fatalf("expected *ParseError, got %T", err)
	}
	if pe.Line == 0 {
		t.Error("ParseError should carry a line number")
	}
}

// TestParse_ProseHeadersSkipped verifies that any `###` heading that is not
// `mem:<id>` or `edge` is treated as prose and silently ignored, so structured
// entries can coexist with ordinary daily-note section headings.
func TestParse_ProseHeadersSkipped(t *testing.T) {
	doc := `### Morning Notes
- did some work

### widget:nope
- foo: bar

### mem:2026-07-12-real-entry
- type: Fact
- content: the real entry
- encoded_time: 2026-07-12T12:00:00Z
- event_time: null
- valid_from: null
- valid_until: null
- confidence: high
- tags: []
- about: []
- last_ref_time: 2026-07-12T12:00:00Z
- S_last: 1
- S_floor: 0.05
- lambda: 0.05
- S_cap: 1
`
	entries, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("prose headers should be silently skipped, got error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content != "the real entry" {
		t.Errorf("wrong entry parsed: %q", entries[0].Content)
	}
}

func TestParse_LoudOnDuplicateKey(t *testing.T) {
	doc := `### mem:2026-06-20-dup
- type: Fact
- type: Decision
- content: x
- encoded_time: 2026-06-20T18:30:00Z
- confidence: high
- tags: []
`
	_, _, err := Parse(doc)
	if err == nil {
		t.Fatal("expected loud error for duplicate key")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate: %v", err)
	}
}

func TestParse_LoudOnBadTimestamp(t *testing.T) {
	doc := `### mem:2026-06-20-badtime
- type: Fact
- content: x
- encoded_time: not-a-time
- confidence: high
- tags: []
`
	_, _, err := Parse(doc)
	if err == nil {
		t.Fatal("expected loud error for bad timestamp")
	}
}

func TestParse_EmptyDoc(t *testing.T) {
	entries, edges, err := Parse("")
	if err != nil {
		t.Fatalf("empty doc should parse cleanly: %v", err)
	}
	if len(entries) != 0 || len(edges) != 0 {
		t.Errorf("empty doc should yield nothing, got %d/%d", len(entries), len(edges))
	}
}

func TestParse_UnknownKeyToExtra(t *testing.T) {
	doc := `### mem:2026-06-20-fwd
- type: Fact
- content: forward compat
- encoded_time: 2026-06-20T18:30:00Z
- confidence: high
- tags: []
- future_field: some-value-a-new-version-added
`
	entries, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("unknown keys should be preserved, not fail: %v", err)
	}
	if got := entries[0].Extra["future_field"]; got != "some-value-a-new-version-added" {
		t.Errorf("unknown key not preserved in Extra: %v", entries[0].Extra)
	}
}

func TestParseList(t *testing.T) {
	cases := map[string][]string{
		"[]":          {},
		"[a]":         {"a"},
		"[a, b, c]":   {"a", "b", "c"},
		"[a,b,c]":     {"a", "b", "c"},
		"[ x ,  y  ]": {"x", "y"},
	}
	for in, want := range cases {
		got := parseList(in)
		if len(got) != len(want) {
			t.Errorf("parseList(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("parseList(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

// asParseError is a tiny errors.As helper kept local to avoid importing errors
// in the test for one call.
func asParseError(err error, target **ParseError) bool {
	for err != nil {
		if pe, ok := err.(*ParseError); ok {
			*target = pe
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
