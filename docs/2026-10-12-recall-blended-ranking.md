# Recall query ranking: remove the hard tier gate

*2026-10-12. Targeted correction to `GraphStore.Recall`; no recall-pipeline rewrite.*

## Problem

For a non-empty query, recall previously concatenated two ranked lists. Any
lexical/vector/traversal hit was placed in tier 1 and therefore beat every
unmatched current entry in tier 2. Decay strength could sort within a tier but
could never cross the boundary. A weak generic vocabulary overlap consequently
acted as an unconditional veto over a much fresher memory.

Empty-query recent mode was already correct: it ranks by decay alone and is
unchanged.

## Decision

The default non-empty-query path now ranks every current entry on one score:

```text
score = 0.60 * relevance + 0.40 * (StrengthAt(entry, now) / SCap)
```

Lexical relevance is `Specificity`. Vector seeds get a relevance floor of
`0.50`; graph-only reached entries get a lighter `0.05` floor because
undirected traversal can also walk from a precise child back to a vague parent.
These preserve both doorfinders without making reach a hard ordering tier. The
two blend weights and both floors are marked `// TUNABLE`; these are explicit
priors for benchmark calibration.

The convex score keeps both inputs on `[0,1]`. Strong structural matches still
count heavily, while a fresh entry can overcome a weak incidental content
match when the decay-strength difference is large enough.

## Comparison and fallback modes

`GraphStore.Recall` defaults to blended ranking. `RecallWithOptions` accepts:

- `RankingBlended` (`blended`): the new default combined score.
- `RankingSpecificity` (`specificity`): the prior tier-1-then-tier-2,
  specificity-first behavior, retained exactly for comparison and fallback.
- `RankingDecay` (`decay`): pure decay-strength order.

`enso-recall` exposes the same choice with
`-ranking blended|specificity|decay`. Empty queries use decay order in every
mode.
