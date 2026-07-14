# WP-3 Gate: Re-baselining the 79-case git-history corpus against specificity

*2026-07-14, Dross Hour. Enso commit: see log. Zero production code touched (bench-only + one corpus ID fix).*

## The question this answers

`ENSO-STATUS.md` (P1 exit honesty pass, 2026-07-13) named the next real seam
before WP-3 (the KùzuDB graph) can earn its cost:

> Before WP-3, re-baseline the 79-case git-history benchmark against
> `EnsoSpecificityModel` (not `BaselineModel`).

The worry behind it: on the **7 hand-curated P1-exit cases**, only 1 (Granola)
actually exercised supersession. The other 6 passed because the *correction
out-specified* the stale entry, so the shipped specificity ranker won **without**
the supersession filter. If that generalized to the whole corpus, the
supersession machinery WP-3 is built to make first-class would be nearly
redundant on real data, and the graph would be gold-plating a roof we don't need.

## The measurement

New test `TestGitHistorySupersessionGate` (`internal/bench/git_history_test.go`)
contrasts two **query-aware** models over the same 79 real supersession pairs
mined from `MEMORY.md`'s git history:

| Model | What it does | P@1 |
|---|---|---|
| `specificity-only (no supersession)` | shipped specificity ranker, **staleness/supersession filter removed** | **45/79 = 0.57** |
| `enso-specificity+staleness+decay` | full shipped P1 pipeline (filter → specificity-rank) | **79/79 = 1.00** |

**Supersession's marginal lift over specificity alone: +0.43 (34 cases).**

New model: `SpecificityBlindModel` (`corpus.go`) — the honest contrast. It is
`EnsoSpecificityModel` with the `IsCurrent`/`superseded` filter stripped out, so
it isolates exactly what specificity contributes on its own.

## The finding (overturns the 7-case worry)

**At corpus scale, supersession is load-bearing on 34 of 79 cases.** The 7-case
result did not generalize — it was an artifact of hand-picked cases where the
correction happened to add discriminating vocabulary.

**Why:** every one of the 34 losses is a `"What is the current status of X?"`
query where the stale entry and the current entry describe the **same subject**
(same project/topic/person). Specificity measures query→entry match; when both
candidates are *about the same thing*, they score **equally**, and specificity
cannot break the tie. Only the `SUPERSEDES` edge / `ValidUntil` knows which head
of the chain is current.

Sample of the 34 (all `cat=supersession`, all same shape):
- "current status of Omega Project?"
- "current status of Issue Comms Automation?"
- "current status of Reliability Workstreams?"
- "current status of SparkKV / SparkSNN / SparkGPT?"
- "current status of CoffeeOps?" ... etc.

The 45 specificity *wins* are the cases where the correction text introduced a
new discriminating token (a renamed artifact, a new person, a moved path) that
the stale entry lacked — the same mechanism that made 6/7 exit cases pass. Real,
but not the majority shape of real supersession.

## Why this matters for the phase gate

The Ensō phase rule is *"each phase must beat the prior benchmark or it doesn't
ship."* This measurement gives WP-3 its target and its justification in one
number:

- **Specificity (what P1 ships) tops out at 0.57 on real supersession.** It
  cannot, in principle, close the gap — same-subject stale/current pairs are
  specificity-indistinguishable by construction.
- **The +0.43 is a supersession-shaped gap.** It is precisely the "connected-fact
  / staleness suppression" class the graph (P2/P3) exists to serve (unified spec
  fixes #3 and #4). WP-3 is not gold-plating; it is closing a measured, real,
  majority-shape gap that no query-content ranker can touch.

So the gate flips from *"is the graph even needed?"* to *"the graph must recover
these 34 same-subject supersession cases that specificity provably can't."* That
is now the WP-3 acceptance bar on this corpus.

## Caveat / honesty notes

- The full pipeline scoring **1.00** is partly a corpus-construction property:
  `GitHistoryCases` builds each pair with an explicit `SUPERSEDES` edge and a
  `ValidUntil` on the stale entry, so the filter always has the edge it needs.
  The corpus proves the *filter uses the edge correctly*; it does **not** prove
  the harder upstream problem — that the live system will reliably *create* that
  edge from an uncorrected, in-conversation status change. That capture problem
  is deliberately out of P1/WP-3 scope (ADR-001 deferred the detection layer).
- What this corpus **does** prove: given the edges, specificity+decay alone
  recovers only 57% of real supersessions, so the edge is not redundant. That is
  the claim the gate needed.

## Bonus: benchmark caught a real live-corpus bug

Running the full suite surfaced that `memory/2026-07-13.md` failed to parse:
entry ID `mem:2026-07-13-jack-comp-DO-NOT-SHARE` violated the kebab-slug grammar
(uppercase). A malformed ID **fails the whole daily file**, silently dropping
every entry in it (including that day's sensitive comp note and the two real
supersession triples written for the P1-exit pass). Fixed the slug to
`mem:2026-07-13-jack-comp-sensitive-do-not-share`; corpus now loads clean and
`TestP1Exit` passes again. Lesson: the parser's fail-the-file-on-bad-ID behavior
is a sharp edge worth a friendlier error / per-entry skip someday, but for now
the benchmark is doing its job as a corpus canary.

## Status

- `make check` IN SYNC (6 sources), `go test ./...` green, `go vet` clean.
- Bench-only change + one corpus ID fix. No production code touched (RH-1/RH-4).
- **Next:** WP-3 acceptance bar on this corpus = recover the 34 same-subject
  supersession cases specificity can't. Re-run `TestGitHistorySupersessionGate`
  once the graph plugin lands; the specificity-only 0.57 is the floor it must
  clear by using the edge.
