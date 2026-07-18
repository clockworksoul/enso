package core

import (
	"strings"
	"time"
)

// propose.go — WP-6: the corpus-level capture proposal.
//
// detect.go answers "does this utterance look like a correction?" from the
// text alone. contradict.go answers "does this utterance contradict THIS
// stored entry?" for one candidate. ProposeSupersession composes them over a
// loaded corpus and answers the operator's actual question:
//
//	Which currently-held belief does this utterance supersede, and on what
//	evidence?
//
// It is a PURE FUNCTION: it never writes, never mutates its inputs, and there
// is deliberately NO auto-commit path from a proposal — the operator (a human,
// or a host policy that has confirmed material intent) authors the corrected
// entry and executes the surviving supersession ceremony (`NewEntry` +
// `Entry.Supersede` via a Store, or `FSStore.Supersede`). Under the
// no-reconsolidation rule a wrong automatic write is permanent corruption;
// proposing and committing stay separated by design.

// TargetEvidence names which sensor produced a proposal's target.
type TargetEvidence string

const (
	// EvidenceContradiction: the target was found by the resolver-side
	// contradiction check — the utterance affirms what the stored entry
	// negates about a shared subject. The strongest targeting evidence,
	// because the corpus supplied half of it.
	EvidenceContradiction TargetEvidence = "contradiction"

	// EvidenceLexical: a lexical correction marker fired and the target is
	// the current entry most specific to the utterance's vocabulary. Correct
	// target selection is likelier when the utterance names its subject.
	EvidenceLexical TargetEvidence = "lexical"

	// EvidenceNone: no correction signal at all — not a correction.
	EvidenceNone TargetEvidence = "none"
)

// SupersessionProposal is the capture layer's output: whether an utterance is
// a correction, which current entry it most plausibly supersedes, and the
// complete evidence trail. A proposal with no Target can still be a genuine
// detection (the operator locates the belief); a proposal with EvidenceNone is
// a non-event.
type SupersessionProposal struct {
	Utterance string

	// Detection is the lexical sensor's verdict (may be a non-detection when
	// the target came from a contradiction — the bare-reaffirmation class has
	// no lexical marker by design).
	Detection Detection

	// Evidence says which sensor targeted the proposal.
	Evidence TargetEvidence

	// Target is the current entry proposed as superseded. Nil when no current
	// entry could be tied to the utterance.
	Target *Entry

	// Contradiction carries the three-evidence trail when Evidence ==
	// EvidenceContradiction.
	Contradiction *Contradiction

	// Candidates are the current entries considered for lexical targeting,
	// ranked by specificity against the utterance (audit trail: why THIS
	// target and not those).
	Candidates []ScoredEntry

	// ProposedContent is the extracted corrected statement, when the lexical
	// sensor could extract one ("" = operator supplies it at confirmation).
	ProposedContent string
}

// Actionable reports whether the proposal warrants surfacing to an operator:
// some sensor fired. (A fired sensor with no resolved target is still
// actionable — the operator may know the belief the corpus search missed.)
func (p SupersessionProposal) Actionable() bool { return p.Evidence != EvidenceNone }

// ProposeSupersession runs both sensors over the corpus as of now and returns
// the composed proposal. Superseded and expired entries are never candidates
// (you cannot supersede a belief that is not currently held); records are
// resolved to the latest state per id first, mirroring the recall pipeline.
func ProposeSupersession(utterance string, entries []Entry, edges []Edge, now time.Time) SupersessionProposal {
	p := SupersessionProposal{Utterance: utterance, Evidence: EvidenceNone}
	if strings.TrimSpace(utterance) == "" {
		return p
	}

	// Current beliefs only, latest record per id (same resolution the recall
	// pipeline applies; duplicated here because core cannot import adapters).
	superseded := map[ID]bool{}
	for _, ed := range edges {
		if ed.Type == EdgeSupersedes {
			superseded[ID(ed.To)] = true
		}
	}
	latest := map[ID]int{}
	var current []Entry
	for _, e := range entries {
		if i, seen := latest[e.ID]; seen {
			current[i] = e
			continue
		}
		latest[e.ID] = len(current)
		current = append(current, e)
	}
	kept := current[:0:0]
	for _, e := range current {
		if superseded[e.ID] || !e.IsCurrent(now) {
			continue
		}
		kept = append(kept, e)
	}

	// Sensor 1 — resolver-side contradiction (checked first: when it fires,
	// the corpus itself supplied half the evidence, which is stronger
	// targeting than vocabulary overlap). Best hit = most shared subjects;
	// ties break by corpus order (stable).
	var (
		bestIdx = -1
		bestCon Contradiction
	)
	for i, e := range kept {
		c := DetectContradiction(utterance, e, now)
		if !c.IsContradiction {
			continue
		}
		if bestIdx < 0 || len(c.SubjectTerms) > len(bestCon.SubjectTerms) {
			bestIdx, bestCon = i, c
		}
	}

	// Sensor 2 — the lexical marker sensor, always run (its Detection is part
	// of the audit trail even when the contradiction targeted first).
	p.Detection = DetectCorrection(utterance)
	p.ProposedContent = p.Detection.Content

	// Lexical candidate ranking over the utterance's vocabulary (reported for
	// audit even when contradiction wins the targeting).
	terms := Tokenize(utterance)
	p.Candidates = RankBySpecificity(kept, terms, now)

	switch {
	case bestIdx >= 0:
		t := kept[bestIdx]
		p.Evidence = EvidenceContradiction
		p.Target = &t
		p.Contradiction = &bestCon
		// A contradiction implies a restate even when no lexical marker fired
		// (the stored status is no longer true); adopt its Detection shape so
		// downstream consumers see one coherent verdict.
		if !p.Detection.IsCorrection {
			p.Detection = bestCon.ToDetection()
		}
	case p.Detection.IsCorrection:
		p.Evidence = EvidenceLexical
		// Target = the most specific current entry, if anything matched at
		// all. Specificity 0 means the utterance shares no vocabulary with
		// any current belief — propose no target rather than a random one.
		if len(p.Candidates) > 0 && p.Candidates[0].Specificity > 0 {
			t := p.Candidates[0].Entry
			p.Target = &t
		}
	}
	return p
}
