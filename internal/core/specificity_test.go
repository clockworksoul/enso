package core

import (
	"testing"
	"time"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"the of and", nil}, // all stop-words
		{"Enso", []string{"enso"}},
		{"where does the enso repo live locally?", []string{"enso", "repo"}},
		// Path splitting: the component "enso" must fall out of a path.
		{"~/workspace/clockworksoul/enso", []string{"workspace", "clockworksoul", "enso"}},
		// Mixed punctuation and case.
		{"github.com:clockworksoul/enso", []string{"github", "com", "clockworksoul", "enso"}},
		// Digits survive.
		{"Go 1.26", []string{"go", "1", "26"}},
	}
	for _, c := range cases {
		got := Tokenize(c.in)
		if !equalStrings(got, c.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSpecificity_EmptyQueryIsZero is the load-bearing invariant: no query
// terms => zero specificity for every entry => RankBySpecificity degrades to
// pure decay. This is what keeps all STALE cases green.
func TestSpecificity_EmptyQueryIsZero(t *testing.T) {
	e := mkEntry(t, "x", TypeFact, "anything at all", []string{"tag"}, []string{"project:x"})
	if got := Specificity(e, nil); got != 0 {
		t.Errorf("Specificity with nil query = %v, want 0", got)
	}
	if got := Specificity(e, []string{}); got != 0 {
		t.Errorf("Specificity with empty query = %v, want 0", got)
	}
}

// TestSpecificity_StructuralBeatsContent: a term in Tags/About scores higher
// than a term only in Content — the core asymmetry that lets the specific child
// (which carries "enso" as a tag) outscore a parent that merely mentions it.
func TestSpecificity_StructuralBeatsContent(t *testing.T) {
	structural := mkEntry(t, "s", TypeFact, "some prose", []string{"enso"}, nil)
	contentOnly := mkEntry(t, "c", TypeFact, "this mentions enso in passing", nil, nil)
	none := mkEntry(t, "n", TypeFact, "unrelated prose", []string{"other"}, nil)

	q := []string{"enso"}
	sStruct := Specificity(structural, q)
	sContent := Specificity(contentOnly, q)
	sNone := Specificity(none, q)

	if !(sStruct > sContent) {
		t.Errorf("structural match (%v) should beat content-only match (%v)", sStruct, sContent)
	}
	if !(sContent > sNone) {
		t.Errorf("content match (%v) should beat no match (%v)", sContent, sNone)
	}
	if sNone != 0 {
		t.Errorf("no-match specificity = %v, want 0", sNone)
	}
	if sStruct != specStructuralWeight { // single term, fully structural
		t.Errorf("single structural term specificity = %v, want %v", sStruct, specStructuralWeight)
	}
}

// TestSpecificity_DistinctTermsOnly: repeating a generic word cannot inflate an
// entry's score; only distinct query terms count.
func TestSpecificity_DistinctTermsOnly(t *testing.T) {
	e := mkEntry(t, "e", TypeFact, "repo repo repo", []string{"repo"}, nil)
	// Query repeats "repo"; distinct-term normalization means score == one
	// structural match, not three.
	got := Specificity(e, []string{"repo", "repo", "repo"})
	if got != specStructuralWeight {
		t.Errorf("repeated-term specificity = %v, want %v (distinct terms only)", got, specStructuralWeight)
	}
}

// TestSpecificity_Normalized: score is normalized by distinct-query-term count,
// so a 1-of-2 structural match scores half of a 1-of-1 structural match.
func TestSpecificity_Normalized(t *testing.T) {
	e := mkEntry(t, "e", TypeFact, "prose", []string{"enso"}, nil)
	oneOfOne := Specificity(e, []string{"enso"})
	oneOfTwo := Specificity(e, []string{"enso", "missing"})
	if oneOfOne != specStructuralWeight {
		t.Errorf("1/1 = %v, want %v", oneOfOne, specStructuralWeight)
	}
	if oneOfTwo != specStructuralWeight/2 {
		t.Errorf("1/2 = %v, want %v", oneOfTwo, specStructuralWeight/2)
	}
}

// TestRankBySpecificity_EmptyQueryEqualsDecay proves the safety invariant at
// the ranker level: with an empty query, RankBySpecificity produces the SAME
// order as Rank (pure decay). This is why STALE cases cannot regress.
func TestRankBySpecificity_EmptyQueryEqualsDecay(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	old := mkEntryAt(t, "old", TypeFact, "old", nil, nil, now.Add(-30*24*time.Hour))
	fresh := mkEntryAt(t, "fresh", TypeFact, "fresh", nil, nil, now.Add(-1*time.Hour))

	entries := []Entry{old, fresh}
	decay := Rank(entries, now)
	spec := RankBySpecificity(entries, nil, now)

	if len(decay) != len(spec) {
		t.Fatalf("length mismatch: decay=%d spec=%d", len(decay), len(spec))
	}
	for i := range decay {
		if decay[i].Entry.ID != spec[i].Entry.ID {
			t.Errorf("order diverged at %d: decay=%s spec=%s", i, decay[i].Entry.ID, spec[i].Entry.ID)
		}
	}
}

// TestRankBySpecificity_SpecificBeatsFresherVague is the NEIGHBOR/path fix in
// miniature: a specific child (matches the query term in its tags) outranks a
// vaguer parent that is FRESHER by decay. Specificity is the primary key; decay
// only breaks ties among equally-specific entries.
func TestRankBySpecificity_SpecificBeatsFresherVague(t *testing.T) {
	now := time.Date(2026, 6, 23, 13, 50, 0, 0, time.UTC)

	// Vague parent: no "enso" anywhere, but touched 5 minutes ago (freshest).
	parent := mkEntryAt(t, "parent", TypeFact,
		"clockworksoul repos live under ~/workspace/clockworksoul/",
		[]string{"workspace", "clockworksoul"}, nil, now.Add(-5*time.Minute))

	// Specific child: carries "enso" as a tag, but written weeks ago (staler).
	child := mkEntryAt(t, "child", TypeFact,
		"the enso repo is at ~/workspace/clockworksoul/enso",
		[]string{"clockworksoul", "enso"}, nil, now.Add(-26*24*time.Hour))

	query := Tokenize("where does the enso repo live locally?")
	out := RankBySpecificity([]Entry{parent, child}, query, now)

	if out[0].Entry.ID != child.ID {
		t.Errorf("specific child should rank first, got %s (specs: child=%v parent=%v)",
			out[0].Entry.ID, Specificity(child, query), Specificity(parent, query))
	}

	// And confirm the parent really IS fresher — i.e. pure decay would have
	// gotten this wrong, so specificity is doing the work.
	if !(StrengthAt(parent, now) > StrengthAt(child, now)) {
		t.Fatalf("test precondition broken: parent should be fresher than child by decay")
	}
}

// --- local test helpers -------------------------------------------------------

func mkEntry(t *testing.T, label string, nt NodeType, content string, tags, about []string) Entry {
	t.Helper()
	return mkEntryAt(t, label, nt, content, tags, about, time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
}

func mkEntryAt(t *testing.T, label string, nt NodeType, content string, tags, about []string, encodedAt time.Time) Entry {
	t.Helper()
	id, err := NewID(encodedAt, label)
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	e, err := NewEntry(NewEntryParams{
		ID:          id,
		Type:        nt,
		Content:     content,
		EncodedTime: encodedAt,
		Confidence:  ConfHigh,
		Tags:        tags,
		About:       about,
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}
