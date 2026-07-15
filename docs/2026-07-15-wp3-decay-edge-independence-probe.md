# WP-3 probe: how much staleness suppression is edge-INDEPENDENT?

*2026-07-15, Dross Hour. Zero production code touched (bench-only: +1 model, +2 tests).*

## The question this answers

The Jul-14 gate proved the `SUPERSEDES` edge is load-bearing (+0.43 over the
specificity ranker, 34 of 79 cases). But that doc's own caveat named the real
frontier and then stopped at it:

> The full pipeline scoring 1.00 is partly a corpus-construction property:
> `GitHistoryCases` builds each pair with an explicit `SUPERSEDES` edge and a
> `ValidUntil`... The corpus proves the filter *uses* the edge correctly; it
> does **not** prove the harder upstream problem — that the live system will
> reliably *create* that edge from an uncorrected, in-conversation status
> change. That capture problem is deliberately out of scope (ADR-001).

The gate isolated three signals: **recency** (baseline), **specificity** (P1's
ranker), and the **edge** (full pipeline). It never isolated the fourth:
**decay** — the leaky-integrator strength P3 introduces.

Decay is special because it is **edge-independent**. `StrengthAt` reads only
`LastRefTime` (init = `EncodedTime`); it needs no capture layer, no detection,
no human correction — just the write timestamps every entry already has. So the
honest WP-3 design question, unasked until tonight:

**Of the 34 same-subject cases specificity provably can't break, how many can
decay ALONE recover — with no edge and no `ValidUntil` — just by letting the
older entry age below the fresher one?**

That slice is staleness suppression the live system gets **for free**, without
solving the hard capture problem.

## The measurement

New `DecayBlindModel` (`corpus.go`) — `core.Rank` (decay strength) over ALL
candidates, supersession/staleness filter stripped, no query. It is the
query-blind decay analogue of `SpecificityBlindModel`. New test
`TestWP3DecayEdgeIndependentContribution` (`wp3_decay_probe_test.go`) scores all
four signals over the same 79 real supersession pairs:

| Model | P@1 | edge? |
|---|---|---|
| baseline-recency | 50/79 = 0.63 | no |
| specificity-only (no supersession) | 45/79 = 0.57 | no |
| **decay-only (no supersession, no query)** | **50/79 = 0.63** | **no** |
| enso-specificity+staleness+decay (full) | 79/79 = 1.00 | YES |

### The load-bearing breakdown

Of the **34** cases specificity-only misses:

- **decay-only RECOVERS 7** — edge-free, timestamp-only staleness suppression.
- **27 both still miss** — irreducibly edge-only; no edge-free signal recovers them.

And the sharp structural result:

> **All 27 edge-only cases are same-day supersessions** (`stale_date ==
> current_date`).

## Why the split is exactly where it is

The corpus mines each supersession pair from MEMORY.md git history. When the
stale note and its correction were written on **different days**, the current
entry has a later `EncodedTime` → later `LastRefTime` → higher `StrengthAt` at
query time. Decay breaks the tie on its own. That is the 7.

When both were written on the **same day** (a note edited and re-landed within
one commit-day), the two entries have **identical `LastRefTime`**, so
`StrengthAt` returns the *same* value and decay is provably powerless — the tie
can only be broken by the `SUPERSEDES` edge (or `ValidUntil`). That is the 27.

Decay didn't fail on the 27 for a tunable reason (wrong `lambda`, wrong floor).
It failed for a **structural** one: zero elapsed time between the two writes
means zero decay differential. No amount of P3 tuning changes that. This is
confirmed independently by `TestWP3DecayMonotoneOnDistinctDates`, which pins the
mechanism: on a synthetic distinct-date same-subject pair with no edge, the
fresher entry *must* rank first.

## What this means for WP-3

Two conflated mechanisms in the 1.00 headline, now separated with a number:

1. **Soft strength DECAY — edge-independent, timestamp-only.** Recovers the
   distinct-date slice of same-subject staleness (7 of the 34 here) with zero
   capture machinery. This is a real, free floor: P3 decay buys staleness
   suppression the P1/P2 system cannot, *without* touching the deferred
   detection layer. It is not just a tiebreaker — on distinct-date same-subject
   pairs it is the *only* edge-free signal that works (specificity ties, recency
   coincidentally agrees here but for the wrong reason — it privileges write
   order, not strength).

2. **Hard supersession FILTER — edge-dependent, capture-deferred.** The
   irreducible 27 are same-day supersessions where *only* the edge can break the
   tie. This is now the precise, minimal capture bar: the live system's
   detection/capture layer (ADR-001 deferred) earns its cost specifically on
   **same-day status flips**, not on the broad "any stale belief" class. That's
   a much smaller, sharper target than "capture all supersessions."

**Reframed gate for WP-3:** decay is not gold-plating and not merely a
tiebreaker — it independently recovers the distinct-date staleness class the
edge is otherwise credited for. The edge's *irreducible* contribution on this
corpus is the **27 same-day** cases. When the KùzuDB graph + P3 decay land,
the honest scoreboard is three-way:
- specificity floor: 0.57
- + decay (edge-free): recovers 7 → the distinct-date staleness class
- + edge (capture-dependent): recovers the final 27 same-day flips → 1.00

## Caveats / honesty notes

- **The 7 vs 27 split is a property of THIS corpus's date distribution.** A
  corpus with more distinct-date supersessions would shift more of the 34 into
  decay's free column; one with more same-day edits would shift more to
  edge-only. The *structural claim* (same-day ⇒ decay-powerless; distinct-day ⇒
  decay-capable) is corpus-independent and is what the mechanism test pins. The
  specific 7/27 is not a universal constant.
- **Recency also scores 0.63 here** and coincidentally recovers the same
  distinct-date cases — but for the wrong reason (it ranks by write order, blind
  to strength). Decay and recency diverge the moment `LastRefTime` is bumped by
  a *material recall* (RECALL-DEF) rather than tracking `EncodedTime`; on this
  replay corpus no recall bumps fire, so they coincide. The value of decay over
  recency shows up only once the recall bump is live (P3), which this offline
  corpus cannot exercise. Flagged as the next real seam.
- **This does not reduce the need for capture** — it *sharpens* it. The 27
  same-day flips still need the edge, and the edge still needs a capture layer
  to exist outside a hand-built corpus. The probe just proves that capture's
  minimal justified target is same-day status flips, and that everything else in
  the same-subject class is reachable from timestamps alone.

## Status

- `make check` IN SYNC (6 sources), `go vet ./...` clean, `go test ./...` green.
- Bench-only: `DecayBlindModel` (+~25 lines, `corpus.go`), 2 new tests
  (`wp3_decay_probe_test.go`). No production code touched (RH-1/RH-4).
- **Next real seam:** the recency/decay divergence. On this replay corpus they
  coincide because no material-recall bump fires. To show decay's value *over*
  recency requires a case where an older-but-recalled entry should outrank a
  newer-but-untouched one — which needs a RECALL-DEF bump in the replay. That's
  the P3 recall-bump seam, and it has no corpus case yet. Build the case before
  the layer.
