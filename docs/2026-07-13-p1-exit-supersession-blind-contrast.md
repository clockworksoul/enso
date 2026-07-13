# P1 Exit: the supersession-blind contrast, and what it reveals

*2026-07-13 Dross Hour. Zero production code touched in `internal/core` or `internal/mdstore`; changes are confined to the benchmark harness (`internal/bench/p1_exit.go`, `p1_exit_test.go`, `testdata/p1_exit_cases.jsonl`) plus two real supersession triples added to the live corpus (`memory/2026-07-13.md`).*

## The problem this session opened on

The P1 exit harness (`f85b306`, Jul-12) reports **P@1 = 1.00, P1 PASS**. But the commit message flags the hollowness itself: all 5 active cases query entries written *the same day*. A query I author to match an entry I just authored isn't a generalization test — it's a vocabulary echo. The three genuinely-hard historical-miss cases (adam-headcount, ed-sandoval, jack-live-topology) were gated `skip_until 2026-07-20` because their structured entries hadn't been written.

So a green 1.00 was masking the fact that the measurement wasn't yet testing the one capability that justifies the whole project: **when a stale belief and its correction both match a query, does supersession suppress the stale one where a naive ranker would be fooled?**

## What I did

1. **Wrote two real supersession triples** into the live corpus (`memory/2026-07-13.md`), using verified ground truth from the `internal/bench/cases.go` seed cases:
   - **Adam headcount** — the classic re-scanned-stale trap: a standing TODO ("message Adam, overdue since Jun 16") re-read on every scan looks perpetually fresh (last-touch Jun 23), newer than the buried Jun-18 outcome where the ask actually landed.
   - **Ed / Neo4j blog** — the whose-court reframe: the stale "Matt owes the next action" belief kept being re-affirmed, while the truer "ball's in Ed's court" fact has been true in world-time since May 26.
   - This is on-mandate WP-2 work (the corpus now has **3 real supersession triples**: Granola + Adam + Ed) and flips two gated cases active.

2. **Added a supersession-blind contrast column** to the harness. For each active case it runs the *same* specificity-first ranker but **skips the IsCurrent/superseded filter**, then reports whether removing supersession changes the #1 result:
   - `LOAD` = removing the filter flips #1 (supersession is load-bearing for this case)
   - `same` = the case passes on specificity/vocabulary alone (supersession is redundant here)

## The finding

```
Case                          Result  S-blind  Top result
granola-supersession          PASS    LOAD     mem:...granola-banned-api-still-works
wp2-grammar-decision          PASS    same     mem:...wp2-ratified
supersession-benchmark        PASS    same     mem:...enso-benchmark-supersession-result
python-toolchain              PASS    same     mem:...python-toolchain-3-14
enso-scope-adr001             PASS    same     mem:...adr-001-full-memory-replacement
adam-headcount-stale          PASS    same     mem:...adam-headcount-landed
ed-sandoval-neo4j-blog        PASS    same     mem:...neo4j-blog-ed-owes

Supersession-backed passes: 1 of 7 active passes.
```

**Only Granola is load-bearing.** Adam and Ed pass — but on specificity, not supersession. I checked why: the *correction* legitimately out-specifies its stale predecessor. For Adam, the resolved fact carries `[work, axon, headcount]` (spec 0.600) while the stale TODO carries `[work, axon, headcount, todo]` (spec 0.500) — the extra `todo` tag *dilutes* the stale entry's normalized specificity, so the correction wins even with supersession switched off.

I deliberately did **not** contort the entries to force a `LOAD` result. Making the resolved fact carry a `todo` tag to tie the specificity would be fabricating for a green light — the fact isn't a todo anymore. The honest measurement is more valuable than a manufactured one.

## Why this matters for the WP-3 (graph) gate

The seed benchmark (git-history, 79 cases) shows Ensō 1.00 vs recency 0.63 — a real 37% gap. That gap is against a **recency-only** baseline. But the *shipped P1 ranker is specificity-first*, not recency. The blind contrast reveals that a specificity-aware ranker **already gets the Adam/Ed class right without supersession**, because a correction usually describes the new reality in language that out-matches the stale belief.

The case where supersession is genuinely irreplaceable is the **Granola shape**: stale and current entries that are *specificity-indistinguishable* — same topic, same tags, near-identical vocabulary — differing only in the **claim** ("keep using it" vs "it's banned"). There, specificity ties, and only the SUPERSEDES edge / `valid_until` breaks the tie correctly.

**Implication for the review:** the supersession filter's marginal value over a specificity ranker is narrower than the 37%-vs-recency headline suggests. It shows up precisely on same-vocabulary belief flips. Before WP-3 (the graph) earns its cost, we should ask: *how common is the Granola shape in the real miss corpus, versus the Adam/Ed shape that specificity already handles?* If most real STALE misses are Adam/Ed-shaped, the graph is buying less than the seed benchmark implies.

This is not an argument against supersession — it's a demand for the *right* denominator. The seed benchmark should be re-scored against the **specificity ranker** as the baseline, not recency, to isolate what supersession contributes on top of what P1 already ships.

## Honest status of P1 exit

- P@1 = 1.00 on 7 active cases, > P0 baseline 0.63. Technically PASS.
- But 6/7 pass on vocabulary/specificity; only 1 exercises supersession.
- **P1 is not honestly "done"** until: (a) the queries are authored independently of the entries (the git-history corpus already does this at scale — use it as the real gate), and (b) the seed benchmark is re-baselined against the specificity ranker to measure supersession's true marginal lift.
- The harness now *reports* its own hollowness (the "Supersession-backed passes: N of M" line + warning), so a future all-`same` 1.00 can't be mistaken for a capability win.

## Next seam (do NOT build ahead of it)

Re-score the 79-case git-history benchmark with `EnsoSpecificityModel` as the baseline column (currently the baseline is `BaselineModel` recency-only). That single change tells us the number that actually matters for the WP-3 decision: **supersession's lift over specificity**, not over recency. Everything else about the graph gate follows from that number. Stop there until it's measured.
