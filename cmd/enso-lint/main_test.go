package main

import (
	"os"
	"path/filepath"
	"testing"
)

const validBlock = `# Daily note 2026-07-17

Some prose about the day.

### mem:2026-07-17-enso-lint-shipped
- type: Fact
- content: enso-lint validates hand-authored mem blocks at write time
- encoded_time: 2026-07-17T06:00:00Z
- confidence: high
- tags: [enso, tooling]
- about: [project:enso]

More prose after the block.
`

const proseOnly = `# Daily note

Just prose here. No structured blocks at all.

### Not A Block
This is a plain markdown section header, ignored by the parser.
`

// missing the required encoded_time key — the exact Jul-15 failure class.
const missingEncodedTime = `### mem:2026-07-17-broken
- type: Fact
- content: this block omits encoded_time
- confidence: high
- tags: [enso]
`

// malformed uppercase ID — the exact Jul-13 failure class. The ID must be a
// lowercase date-prefixed slug; an uppercase segment fails core.ID validation.
const uppercaseID = `### mem:2026-07-17-BadID
- type: Fact
- content: this block has an invalid uppercase id
- encoded_time: 2026-07-17T06:00:00Z
- confidence: high
- tags: [enso]
`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp %s: %v", name, err)
	}
	return p
}

func TestLintFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantErr     bool
		wantEntries int
	}{
		{"valid block", validBlock, false, 1},
		{"prose only", proseOnly, false, 0},
		{"missing encoded_time", missingEncodedTime, true, 0},
		{"uppercase id", uppercaseID, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTemp(t, "daily.md", tc.content)
			entries, _, err := lintFile(p)
			if tc.wantErr && err == nil {
				t.Fatalf("expected parse error, got none")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && entries != tc.wantEntries {
				t.Fatalf("entries = %d, want %d", entries, tc.wantEntries)
			}
		})
	}
}

func TestLintFileMissingFile(t *testing.T) {
	_, _, err := lintFile(filepath.Join(t.TempDir(), "does-not-exist.md"))
	if err == nil {
		t.Fatal("expected IO error for missing file, got none")
	}
}

func TestTargets(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"2026-07-01.md", "2026-07-02.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// -dir expands to sorted *.md only.
	got, err := targets(dir, nil)
	if err != nil {
		t.Fatalf("targets(-dir): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d md files, want 2 (%v)", len(got), got)
	}
	if filepath.Base(got[0]) != "2026-07-01.md" || filepath.Base(got[1]) != "2026-07-02.md" {
		t.Fatalf("unsorted or wrong files: %v", got)
	}

	// positional args pass through.
	got, err = targets("", []string{"a.md", "b.md"})
	if err != nil || len(got) != 2 {
		t.Fatalf("positional targets: got %v err %v", got, err)
	}

	// both -dir and args = usage error.
	if _, err := targets(dir, []string{"a.md"}); err == nil {
		t.Fatal("expected usage error for both -dir and args")
	}

	// neither = usage error.
	if _, err := targets("", nil); err == nil {
		t.Fatal("expected usage error for no targets")
	}

	// unreadable dir = error.
	if _, err := targets(filepath.Join(dir, "nope"), nil); err == nil {
		t.Fatal("expected error for unreadable dir")
	}
}
