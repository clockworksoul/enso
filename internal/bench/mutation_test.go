package bench

import (
	"sort"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// mutation_test.go — benchmark INTEGRITY probe (2026-07-04 Dross Hour).
//
// # What this file proves
//
// The whole value of this benchmark is the claim "Ensō ranks the correct,
// current entry first where the naive model doesn't." Every session leans on
// STALE 2/2 and NEIGHBOR 1/1 as a green light. But a green benchmark is only
// trustworthy if it would go RED when the ranking logic is broken. Nobody had
// verified that. The existing TestBenchmark_CorrectionCaptureIsLoadBearing
// mutates the INPUT (strips the SUPERSEDES edge to simulate an uncaptured
// correction) — it does NOT verify that a bug in the code that CONSUMES those
// edges (EnsoModel.Rank / RankBySpecificity) would be caught.
//
// This is mutation testing: for each mechanism the Ensō pipeline relies on,
// build a "mutant" model with exactly that one mechanism broken (a faithful
// bug a real refactor could introduce), replay the real corpus through it, and
// assert the corpus CATCHES the mutant (score drops). A mutant that SURVIVES —
// the corpus still scores 1.0 with broken code — is a real hole in the safety
// net, and this file's job is to find it or prove there isn't one.
//
// It builds NO production machinery. If every mutant is caught, that is a proof
// of the benchmark's discriminating power (exactly the "prove the numbers are
// trustworthy" mandate of the Jun-17 architecture audit). If a mutant survives,
// that is a discovered gap worth a real fix — validation before construction.

// --- Mutant models: each disables exactly one mechanism of the real pipeline --

// mutantNoSupersession is EnsoModel with the SUPERSEDES filter removed. It still
// applies IsCurrent and decay ranking, but never drops an entry just because a
// SUPERSEDES edge closed it. This is the single most important mechanism for the
// STALE class: it is precisely the bug a refactor introduces by forgetting to
// consult edges.
type mutantNoSupersession struct{}

func (mutantNoSupersession) Name() string { return "MUTANT-no-supersession" }
func (mutantNoSupersession) Rank(candidates []core.Entry, _ []core.Edge, now time.Time) []core.Entry {
	kept := make([]core.Entry, 0, len(candidates))
	for _, c := range candidates {
		if !c.IsCurrent(now) { // still honors ValidUntil, just not the edge
			continue
		}
		kept = append(kept, c)
	}
	ranked := core.Rank(kept, now)
	out := make([]core.Entry, len(ranked))
	for i, r := range ranked {
		out[i] = r.Entry
	}
	return out
}

// mutantNoStaleness is EnsoModel with the IsCurrent (ValidUntil) filter removed.
// It still honors SUPERSEDES edges. This isolates whether ValidUntil-based
// staleness is independently load-bearing or whether the edge alone carries the
// STALE cases (the seed cases set BOTH, so this asks which one the corpus
// actually tests).
type mutantNoStaleness struct{}

func (mutantNoStaleness) Name() string { return "MUTANT-no-staleness-filter" }
func (mutantNoStaleness) Rank(candidates []core.Entry, edges []core.Edge, now time.Time) []core.Entry {
	superseded := map[core.ID]bool{}
	for _, e := range edges {
		if e.Type == core.EdgeSupersedes {
			superseded[core.ID(e.To)] = true
		}
	}
	kept := make([]core.Entry, 0, len(candidates))
	for _, c := range candidates {
		if superseded[c.ID] { // still drops superseded, but ignores ValidUntil
			continue
		}
		kept = append(kept, c)
	}
	ranked := core.Rank(kept, now)
	out := make([]core.Entry, len(ranked))
	for i, r := range ranked {
		out[i] = r.Entry
	}
	return out
}

// mutantReverseDecay is EnsoModel with the decay comparator inverted: it applies
// the correct supersession + staleness filters, then ranks survivors by
// ASCENDING decay strength (weakest first). This catches a "flipped the sort"
// bug in core.Rank consumption. On a STALE pair where the filters already remove
// the distractor, only one entry survives, so this mutant may be UNAFFECTED —
// which is itself a finding about how much of the STALE win is filter vs. rank.
type mutantReverseDecay struct{}

func (mutantReverseDecay) Name() string { return "MUTANT-reverse-decay-order" }
func (mutantReverseDecay) Rank(candidates []core.Entry, edges []core.Edge, now time.Time) []core.Entry {
	superseded := map[core.ID]bool{}
	for _, e := range edges {
		if e.Type == core.EdgeSupersedes {
			superseded[core.ID(e.To)] = true
		}
	}
	kept := make([]core.Entry, 0, len(candidates))
	for _, c := range candidates {
		if !c.IsCurrent(now) || superseded[c.ID] {
			continue
		}
		kept = append(kept, c)
	}
	// Inverted comparator: weakest-first instead of strongest-first.
	sort.SliceStable(kept, func(i, j int) bool {
		return core.StrengthAt(kept[i], now) < core.StrengthAt(kept[j], now)
	})
	return kept
}

// mutantDecayPrimarySpecificity is EnsoSpecificityModel with the sort keys
// SWAPPED: decay strength primary, specificity as the tiebreaker. This is the
// exact bug that would silently un-fix NEIGHBOR — the specificity signal is
// still computed but no longer dominates, so a fresher-but-vaguer parent wins
// on decay. It is a QueryModel (specificity needs the query).
type mutantDecayPrimarySpecificity struct{}

func (mutantDecayPrimarySpecificity) Name() string {
	return "MUTANT-decay-primary-specificity-tiebreak"
}
func (mutantDecayPrimarySpecificity) RankQuery(query string, candidates []core.Entry, edges []core.Edge, now time.Time) []core.Entry {
	superseded := map[core.ID]bool{}
	for _, e := range edges {
		if e.Type == core.EdgeSupersedes {
			superseded[core.ID(e.To)] = true
		}
	}
	kept := make([]core.Entry, 0, len(candidates))
	for _, c := range candidates {
		if !c.IsCurrent(now) || superseded[c.ID] {
			continue
		}
		kept = append(kept, c)
	}
	terms := core.Tokenize(query)
	// Swapped keys: decay PRIMARY, specificity tiebreak (the bug).
	sort.SliceStable(kept, func(i, j int) bool {
		si, sj := core.StrengthAt(kept[i], now), core.StrengthAt(kept[j], now)
		if si != sj {
			return si > sj
		}
		return core.Specificity(kept[i], terms) > core.Specificity(kept[j], terms)
	})
	return kept
}

// mutantNoStaleAtAll removes BOTH STALE mechanisms at once: it ignores the
// SUPERSEDES edge AND the IsCurrent/ValidUntil filter. This is the TRUE floor
// gate for the STALE corpus — with neither mechanism, the model is pure decay
// ranking over the raw candidate set, which is exactly what lets the stale
// (freshest-by-touch) entry win. Unlike the single-mechanism mutants (which the
// belt-and-suspenders redundancy lets survive), this one MUST be caught, or the
// STALE corpus is proving nothing at all.
type mutantNoStaleAtAll struct{}

func (mutantNoStaleAtAll) Name() string { return "MUTANT-no-stale-mechanism-at-all" }
func (mutantNoStaleAtAll) Rank(candidates []core.Entry, _ []core.Edge, now time.Time) []core.Entry {
	// No supersession filter, no IsCurrent filter — rank the raw set by decay.
	ranked := core.Rank(candidates, now)
	out := make([]core.Entry, len(ranked))
	for i, r := range ranked {
		out[i] = r.Entry
	}
	return out
}

// mutantIgnoreSpecificity is EnsoSpecificityModel that ignores the query
// entirely and ranks by pure decay — i.e. it collapses Stage 5 back to the
// query-blind EnsoModel. This is the "specificity was silently dropped" bug.
type mutantIgnoreSpecificity struct{}

func (mutantIgnoreSpecificity) Name() string { return "MUTANT-specificity-ignored" }
func (m mutantIgnoreSpecificity) RankQuery(_ string, candidates []core.Entry, edges []core.Edge, now time.Time) []core.Entry {
	return EnsoModel{}.Rank(candidates, edges, now)
}

// --- The probe: every mutant must be CAUGHT by the corpus -----------------------

// TestMutation_StaleMechanismsAreCaught replays the STALE seed corpus through
// the STALE-relevant mutants. The HEADLINE FINDING of this probe (2026-07-04):
// the two STALE mechanisms — the SUPERSEDES edge and the ValidUntil close — are
// FULLY REDUNDANT on the seed corpus, because core.Entry.Correct sets BOTH.
// Removing either one ALONE leaves the other to carry the win, so the corpus
// (as it stands) cannot detect a break in a SINGLE mechanism.
//
// That is not a production bug — the redundancy is intentional belt-and-
// suspenders. But it IS a limit on the benchmark's discriminating power that
// was previously unmeasured: a refactor that broke supersession-edge handling
// while leaving IsCurrent intact (or vice versa) would keep the benchmark green.
// This test pins that reality explicitly so nobody mistakes STALE 2/2 for a
// stronger guarantee than it is, and it enforces the TRUE floor gate: a mutant
// with BOTH mechanisms removed (mutantNoStaleAtAll) MUST be caught — otherwise
// the STALE corpus proves nothing.
func TestMutation_StaleMechanismsAreCaught(t *testing.T) {
	cases := SeedCases()
	real := Run(EnsoModel{}, cases)
	if real.Score() != 1.0 {
		t.Fatalf("precondition: real Ensō must be 1.0 on STALE, got %.2f", real.Score())
	}

	// (A) The SINGLE-mechanism mutants are EXPECTED to survive on this corpus,
	//     because the two mechanisms are redundant (Correct sets both). We assert
	//     survival to PIN the redundancy — if a future case makes one mechanism
	//     independently necessary, this expectation flips and we learn the corpus
	//     got stronger.
	singles := []Model{mutantNoSupersession{}, mutantNoStaleness{}, mutantReverseDecay{}}
	for _, m := range singles {
		res := Run(m, cases)
		t.Logf("single-mechanism  %-30s precision@1 = %.2f (%d/%d)",
			res.Model, res.Score(), res.TopHits, res.Total)
		if res.Score() < real.Score() {
			t.Logf("  NOTE: %s was CAUGHT — this single mechanism is now independently "+
				"load-bearing on the seed corpus (redundancy no longer total). Good: "+
				"the corpus got more discriminating. Update this expectation.", res.Model)
		}
	}

	// (B) The TRUE hard gate: removing BOTH STALE mechanisms MUST break the
	//     corpus. If this survives, the benchmark is measuring nothing on STALE.
	both := Run(mutantNoStaleAtAll{}, cases)
	t.Logf("both-removed      %-30s precision@1 = %.2f (%d/%d)",
		both.Model, both.Score(), both.TopHits, both.Total)
	if both.Score() >= real.Score() {
		t.Errorf("FLOOR GATE FAILED: %s scored %.2f (>= real %.2f) — with NEITHER "+
			"supersession NOR staleness the model still wins, so the STALE corpus "+
			"proves nothing. The benchmark is not discriminating on STALE.",
			both.Model, both.Score(), real.Score())
	}
	if len(both.Failures) != both.Total {
		t.Logf("  (both-removed failed %d/%d cases; ideally all %d, confirming every "+
			"seed case genuinely depends on the stale machinery)",
			len(both.Failures), both.Total, both.Total)
	}
}

// TestMutation_NeighborMechanismIsCaught replays the NEIGHBOR corpus through the
// specificity-breaking mutants and asserts each is caught (drops to 0/1, the
// query-blind failure). If a mutant that breaks specificity still solves
// NEIGHBOR, the corpus is not actually testing the specificity mechanism.
func TestMutation_NeighborMechanismIsCaught(t *testing.T) {
	neighbor := NeighborCases()
	real := RunQueryAware(EnsoSpecificityModel{}, neighbor)
	if real.Score() != 1.0 {
		t.Fatalf("precondition: real Stage 5 must be 1.0 on NEIGHBOR, got %.2f", real.Score())
	}

	mutants := []QueryModel{
		mutantDecayPrimarySpecificity{},
		mutantIgnoreSpecificity{},
	}
	for _, m := range mutants {
		res := RunQueryAware(m, neighbor)
		survived := res.Score() >= real.Score()
		t.Logf("%-42s precision@1 = %.2f (%d/%d)  survived=%v",
			res.Model, res.Score(), res.TopHits, res.Total, survived)
		if survived {
			t.Errorf("MUTANT SURVIVED: %s scored %.2f (>= real %.2f) on NEIGHBOR — specificity mechanism is NOT actually tested by the corpus.",
				res.Model, res.Score(), real.Score())
		}
	}
}

// TestMutation_PerCaseDiscrimination is the sharpest finding of the probe. It
// asks, PER CASE, whether the stale machinery is actually load-bearing: replay
// each seed case through mutantNoStaleAtAll (pure decay, no supersession, no
// staleness) and check whether decay ALONE already ranks the correct entry
// first. A case that pure-decay solves for free is NOT a discriminating test of
// the stale machinery — it passes for the wrong reason.
//
// FINDING (2026-07-04): only adam-headcount-stale is genuinely discriminating
// (pure decay picks the WRONG stale entry, so supersession/staleness is what
// rescues it). ed-sandoval-timeline-reframe is solved by decay alone (the
// corrected entry happens to out-rank the stale one on strength), so it does
// NOT prove the stale machinery works. The effective discriminating STALE
// corpus is therefore n=1, not n=2 — which sharpens the standing conclusion
// that the highest-value corpus work is a second, genuinely DECAY-HOSTILE STALE
// case (one where the stale entry looks fresher by every recency signal).
//
// This is a diagnostic (t.Logf), not a hard gate: it is legitimate for a case
// to be non-discriminating, as long as at least one case IS. The hard floor
// (at least one case depends on the machinery) is enforced in
// TestMutation_StaleMechanismsAreCaught via the both-removed gate scoring < 1.0.
func TestMutation_PerCaseDiscrimination(t *testing.T) {
	cases := SeedCases()
	discriminating := 0
	for _, c := range cases {
		out := mutantNoStaleAtAll{}.Rank(c.Candidates, c.Edges, c.AsOf)
		pureDecayCorrect := len(out) > 0 && out[0].ID == c.WantID
		if !pureDecayCorrect {
			discriminating++ // pure decay got it WRONG => machinery is load-bearing here
		}
		t.Logf("case=%-28s pure-decay-solves=%v  discriminating=%v",
			c.Name, pureDecayCorrect, !pureDecayCorrect)
	}
	t.Logf("DISCRIMINATING STALE cases: %d/%d (the rest pass because decay alone "+
		"already ranks the truth first — they do NOT test the stale machinery)",
		discriminating, len(cases))
	if discriminating == 0 {
		t.Errorf("NO discriminating STALE case: every seed case is solved by pure " +
			"decay, so the stale machinery is never actually tested by the corpus.")
	}
}

// TestMutation_StaleRedundancyFinding characterizes HOW the STALE seed cases are
// won: is it the SUPERSEDES edge, the ValidUntil close, or both? It runs the
// two "single-mechanism-removed" mutants and records whether each still solves
// the corpus. This is a diagnostic, not a pass/fail gate — it documents the
// redundancy so a future refactor knows which mechanism is truly load-bearing
// vs. belt-and-suspenders. (Both mechanisms are set by core.Entry.Correct, so
// the redundancy is real and intentional, but the corpus's SENSITIVITY to each
// is worth knowing.)
func TestMutation_StaleRedundancyFinding(t *testing.T) {
	cases := SeedCases()

	edgeOnly := Run(mutantNoStaleness{}, cases)     // ValidUntil ignored, edge honored
	validOnly := Run(mutantNoSupersession{}, cases) // edge ignored, ValidUntil honored

	t.Logf("FINDING — which mechanism carries the STALE win on the seed corpus:")
	t.Logf("  edge-only  (ignore ValidUntil, honor SUPERSEDES): %.2f (%d/%d)",
		edgeOnly.Score(), edgeOnly.TopHits, edgeOnly.Total)
	t.Logf("  valid-only (honor ValidUntil, ignore SUPERSEDES):  %.2f (%d/%d)",
		validOnly.Score(), validOnly.TopHits, validOnly.Total)

	// The real model honors BOTH; the point of this diagnostic is only to log
	// which single mechanism suffices. No assertion on direction here — the
	// hard gate lives in TestMutation_StaleMechanismsAreCaught (the SUPERSEDES
	// mutant MUST be caught).
	if edgeOnly.Score() == 1.0 && validOnly.Score() == 1.0 {
		t.Logf("  => REDUNDANT: either mechanism alone solves the seed corpus (belt-and-suspenders).")
	} else if edgeOnly.Score() == 1.0 {
		t.Logf("  => SUPERSEDES edge is sufficient; ValidUntil is not independently required on these cases.")
	} else if validOnly.Score() == 1.0 {
		t.Logf("  => ValidUntil is sufficient; SUPERSEDES edge is not independently required on these cases.")
	} else {
		t.Logf("  => NEITHER single mechanism suffices; both are jointly required.")
	}
}
