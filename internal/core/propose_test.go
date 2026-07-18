package core

import (
	"testing"
	"time"
)

// propose_test.go — WP-6: the corpus-level proposal layer.
//
// The end-to-end replay of the four REAL utterances over their case corpora
// lives in internal/bench (wp6_capture_test.go), where the real cases are.
// Here we pin the proposal mechanics: purity (proposing writes nothing),
// evidence routing (contradiction vs lexical vs none), targeting rules, and
// the Jul-1 precision fence at the PROPOSAL level.

func proposeCorpus(t *testing.T) []Entry {
	t.Helper()
	day := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	mk := func(label, content string, tags, about []string) Entry {
		id, err := NewID(day, label)
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		e, err := NewEntry(NewEntryParams{
			ID: id, Type: TypeFact, Content: content, EncodedTime: day,
			Confidence: ConfHigh, Tags: tags, About: about,
		})
		if err != nil {
			t.Fatalf("NewEntry: %v", err)
		}
		return e
	}
	return []Entry{
		mk("granola-banned", "Granola is banned per Yext policy.",
			[]string{"tools", "granola"}, []string{"tool:granola"}),
		mk("adam-headcount-todo", "TODO: raise the Adam headcount ask at the next 1:1.",
			[]string{"adam", "headcount"}, []string{"person:adam"}),
		mk("espresso-beans", "The good espresso beans are the dark roast.",
			[]string{"espresso"}, []string{}),
	}
}

// TestProposeSupersession_ContradictionTargets: the bare-reaffirmation class —
// no lexical marker, but the corpus supplies the contradiction evidence.
func TestProposeSupersession_ContradictionTargets(t *testing.T) {
	entries := proposeCorpus(t)
	now := time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC)

	p := ProposeSupersession("Granola still works", entries, nil, now)
	if !p.Actionable() {
		t.Fatal("expected an actionable proposal")
	}
	if p.Evidence != EvidenceContradiction {
		t.Fatalf("evidence = %s, want contradiction", p.Evidence)
	}
	if p.Target == nil || p.Target.ID != entries[0].ID {
		t.Fatalf("target = %v, want the stored granola-ban entry", p.Target)
	}
	if p.Contradiction == nil || len(p.Contradiction.SubjectTerms) == 0 {
		t.Fatal("contradiction evidence must be carried with its subject terms")
	}
	// The riskiest inference stays barred: a contradiction proposal carries no
	// invented content — the operator supplies it at confirmation.
	if p.ProposedContent != "" {
		t.Fatalf("contradiction proposal must not invent content, got %q", p.ProposedContent)
	}
}

// TestProposeSupersession_LexicalTargets: a marked correction resolves to the
// most specific current entry.
func TestProposeSupersession_LexicalTargets(t *testing.T) {
	entries := proposeCorpus(t)
	now := time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC)

	p := ProposeSupersession("Actually, the Adam headcount ask already landed at the Jun 18 1:1", entries, nil, now)
	if p.Evidence != EvidenceLexical {
		t.Fatalf("evidence = %s, want lexical", p.Evidence)
	}
	if p.Target == nil || p.Target.ID != entries[1].ID {
		t.Fatalf("target = %v, want the Adam headcount TODO", p.Target)
	}
	if p.ProposedContent == "" {
		t.Fatal("lexical restate should carry the extracted corrected statement")
	}
}

// TestProposeSupersession_NoVocabularyNoTarget: a detection whose vocabulary
// matches no current belief proposes NO target rather than a random one.
func TestProposeSupersession_NoVocabularyNoTarget(t *testing.T) {
	entries := proposeCorpus(t)
	now := time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC)

	p := ProposeSupersession("Scratch that: the offsite moved to Thursday", entries, nil, now)
	if p.Evidence != EvidenceLexical {
		t.Fatalf("evidence = %s, want lexical (the marker fired)", p.Evidence)
	}
	if p.Target != nil {
		t.Fatalf("no current entry shares vocabulary; target must be nil, got %s", p.Target.ID)
	}
	if !p.Actionable() {
		t.Fatal("a fired sensor without a resolved target is still actionable (operator locates the belief)")
	}
}

// TestProposeSupersession_SupersededNeverTargeted: an already-superseded entry
// is not a currently-held belief and must never be proposed as a target.
func TestProposeSupersession_SupersededNeverTargeted(t *testing.T) {
	entries := proposeCorpus(t)
	now := time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC)

	// Supersede the granola ban (the correction already happened).
	newID, err := NewID(time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC), "granola-unbanned")
	if err != nil {
		t.Fatal(err)
	}
	closed, edge := entries[0].Supersede(newID, time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC))
	all := append(append([]Entry{}, entries...), closed)

	p := ProposeSupersession("Granola still works", all, []Edge{edge}, now)
	if p.Evidence == EvidenceContradiction {
		t.Fatal("contradiction fired against a superseded belief; only current beliefs can be contradicted")
	}
	if p.Target != nil && p.Target.ID == entries[0].ID {
		t.Fatal("superseded entry proposed as target")
	}
}

// TestProposeSupersession_PrecisionFence pins the Jul-1 innocent corpus at the
// PROPOSAL level: ordinary status remarks yield non-actionable proposals even
// with a rich current corpus present.
func TestProposeSupersession_PrecisionFence(t *testing.T) {
	entries := proposeCorpus(t)
	now := time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC)
	innocent := []string{
		"Good news, the staging link is still live.",
		"Yeah the API key still works, I just tested it.",
		"That coupon is still valid until Friday.",
		"Honestly the README undersells how good this tool is.",
		"The dashboard does more than that already, we're fine.",
		"The deploy went out at 3pm and looks healthy.",
	}
	for _, s := range innocent {
		if p := ProposeSupersession(s, entries, nil, now); p.Actionable() {
			t.Errorf("innocent sentence produced an actionable proposal: %q (evidence=%s, signals=%v)",
				s, p.Evidence, p.Detection.Signals)
		}
	}
}

// TestProposeSupersession_Pure pins the no-auto-commit invariant structurally:
// proposing mutates neither the entries slice nor anything reachable from it.
func TestProposeSupersession_Pure(t *testing.T) {
	entries := proposeCorpus(t)
	now := time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC)
	before := make([]Entry, len(entries))
	copy(before, entries)

	_ = ProposeSupersession("Granola still works", entries, nil, now)
	_ = ProposeSupersession("Actually, the Adam ask landed already", entries, nil, now)

	for i := range before {
		if entries[i].ID != before[i].ID || entries[i].Content != before[i].Content ||
			(entries[i].ValidUntil == nil) != (before[i].ValidUntil == nil) {
			t.Fatalf("ProposeSupersession mutated entry %d", i)
		}
	}
}
