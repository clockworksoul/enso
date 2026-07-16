# WP-3 seam: proving decay is NOT a recency proxy (the recall-bump divergence)

*2026-07-16, Dross Hour. Bench-only (+1 model, +3 tests) + 1 live-corpus parse fix. No production code touched (RH-1/RH-4).*

## The question this closes

The Jul-15 edge-independence probe ended on an honest, deliberately-unbuilt
frontier. It had shown that on the 79-case git-history replay corpus,
**decay-only and recency both score 0.63 and recover the identical set of
cases.** They coincide for a structural reason: the replay is static — every
entry's `LastRefTime` is frozen at `EncodedTime`, so "highest decay strength"
and "newest write" are the *same ordering*. The doc named the seam and stopped:

> Decay and recency diverge the moment `LastRefTime` is bumped by a material
> recall (RECALL-DEF) rather than tracking `EncodedTime`; on this replay corpus
> no recall bumps fire, so they coincide. The value of decay over recency shows
> up only once the recall bump is live (P3) ... **Build the case before the
> layer.**

Until tonight, "decay beats recency" was an **untested claim**. The corpus
could not exercise it, and no benchmark case distinguished the two signals at
all. If decay is only ever a recency proxy, then P3's leaky-integrator machinery
is gold-plating — recency (a one-line sort) would get everything decay gets, for
free. This session builds the falsifiable measurement that settles it.

## Discipline note (validate before build)

This does **not** wire `BumpOnRecall` into any production recall path. The bump
math (`core.BumpOnRecall`, `core.StrengthAt`) already exists and is unit-tested.
What was missing was a *benchmark case that distinguishes decay from recency*.
Per the standing rule — build the measurement before the layer — this makes the
distinction testable so that when P3 wiring lands, its acceptance bar is a real
number on a real scenario, not a reflexive "decay is obviously better."

## The measurement

New `RecallBumpModel` (`wp3_recall_bump_test.go`): ranks by decay strength
**after** applying a set of material-recall (RECALL-DEF) bumps to the
candidates. It is the *only* model in the harness whose ranking can differ from
write-order, because `BumpOnRecall` moves an entry's `LastRefTime` forward past
its `EncodedTime`. With **zero** recall events it is byte-identical to
`DecayBlindModel` (pinned by a no-op test) — so the entire divergence is carried
by the bumps, not by any incidental model difference.

### The scenario (a recall shape, not a supersession)

Two same-subject Facts, both plausibly matching "the enso architecture", neither
stale or wrong:

- **Entry A ("load-bearing invariant"), OLD by write, HOT by usage.** Written
  Apr 1; materially recalled ~weekly (12 spaced recalls) through late June. The
  durable architectural fact Matt asks about repeatedly ("Markdown is canonical;
  the graph is a derived cache").
- **Entry B ("write-only note"), NEW by write, COLD by usage.** Written Jun 25;
  never materially recalled. A one-off consolidation-pass note ("the Jul-2 sync
  had six attendees").

Query at Jun 26. The *right* answer for a relevance-ranked recall is A — the
durable, repeatedly-used memory, exactly what a human's spacing-strengthened
memory surfaces first. Recency structurally cannot produce that ordering.

### Result

| Model | Ranks first | Verdict |
|---|---|---|
| recency (`BaselineModel`) | **B** (newer write) | WRONG for relevance recall |
| decay, no bump (`DecayBlindModel`) | **B** (== recency) | control: coincides, as Jul-15 claims |
| **decay + 12 recalls (`RecallBumpModel`)** | **A** (S=0.7557 > B=0.3361) | **RIGHT: durable + used** |

The control line is load-bearing: decay-*without*-the-bump picking B proves the
Jul-15 coincidence claim is real, so A winning under the bump is attributable to
the recall signal alone — not to decay parameters that happen to favor the older
entry.

## The second thing decay buys: spacing

`TestWP3RecallBumpSpacingMatters` pins a property recency cannot represent *even
in principle*. Two entries recalled the **same number of times** (3) but at
different spacing:

- **spaced** (7 days apart): final strength **0.3072**
- **massed** (1 day apart): final strength **0.2190**

The spaced entry consolidates a higher `S_floor` via the spacing multiplier
`(1 − e^(−Δt/τ))` — Bjork's desirable-difficulty effect. Recency sees only the
last write timestamp and is blind to *how* a memory was used over time. This is
information decay+recall carries that no recency heuristic can encode, full stop.

## What this means for WP-3

The three-way scoreboard from Jul-15 (specificity 0.57 → +decay-edge-free 7 →
+edge-capture 27 → 1.00) measured decay's staleness-suppression contribution but
could not separate it from recency, because both scored 0.63 on the static
replay. This session adds the missing axis:

- **Decay's value OVER recency is now demonstrated, not assumed.** On a scenario
  recency provably gets wrong, decay-with-recall gets right, and the mechanism
  (strength lift + floor consolidation) is pinned numerically.
- **The recall bump is the load-bearing P3 primitive** — not the decay curve
  alone. Decay-blind coincides with recency; it is `BumpOnRecall` moving
  `LastRefTime` that creates the divergence. That sharpens what P3 must ship:
  not just "compute `StrengthAt` at query time" (that alone == recency on any
  never-recalled corpus) but "fire a RECALL-DEF bump when a memory is surfaced
  AND materially used."

## The honest caveat (stop at the next seam)

This is **n=1, hand-built**. It proves the divergence is *possible* and pins the
mechanism, but it is a constructed scenario, not a real observed miss. The claim
it earns is bounded: *decay+recall can beat recency on a relevance-recall case
recency structurally cannot get* — not *decay+recall beats recency at corpus
scale.* To make the latter claim I need a **real logged case** where the live
system surfaced a fresh-but-cold note over a durable-and-used one (a recency
false-positive on relevance). No such case is in the corpus yet — the git-history
corpus is built from supersession pairs, which is a different miss shape.

**Next real seam:** capture a real recency-vs-relevance miss from the live
Phase-0 log (a case where the newest matching entry was surfaced but the
correct answer was an older, frequently-referenced one) and add it as the first
*observed* RecallBump case. Until then, the offline recall-event sequence is a
faithful model of the mechanism but a stand-in for real usage telemetry —
which the live active-memory plugin does not yet emit (`persistTranscripts:false`).
Do NOT wire `BumpOnRecall` into production before that telemetry exists; a bump
that never fires (or fires on mere retrieval, not material use) would make decay
collapse back into recency and forfeit the entire distinction proven here.

## Bonus: a live-corpus parse bug this session surfaced

Running the full suite failed the P1-exit tests (which load the *real* MEMORY
corpus) with:

```
mdstore: in memory/2026-07-15.md: parse error at line 28:
  entry "mem:2026-07-15-enso-wp3-decay-edge-independence" missing required key "encoded_time"
```

The Jul-15 daily `mem:` block (the structured entry logging *that* Dross Hour's
own finding) was written without its required `encoded_time` key, which fails the
entire daily file and silently drops every structured entry in it from the
corpus. Same failure class the Jul-14 gate caught (a malformed uppercase ID).
Fixed by adding the reserved temporal keys (`encoded_time: 2026-07-15T06:00:00Z`
+ the three null placeholders) in the canonical position. This is a recurring
hazard of hand-writing `mem:` blocks during consolidation — **the parser is
strict on `type`/`encoded_time` by design (they're load-bearing for ranking),
so a hand-authored block that omits them takes the whole day's file down.** Worth
a lint step in `enso-append` or a pre-commit check eventually; noted, not built.

## Status

- `make check` IN SYNC (6 sources), `go vet ./...` clean, `go test ./...` green.
- Bench-only: `RecallBumpModel` + 3 tests (`wp3_recall_bump_test.go`, ~260 lines).
  No production code touched (RH-1/RH-4).
- Live-corpus fix: `memory/2026-07-15.md` `mem:` block missing keys (in the
  OpenClaw workspace repo, committed separately there).

**Loop position:** the WP-3 gate is now measured on all four signals —
recency (0.63), specificity (0.57), decay-edge-free (0.63, +7 over specificity),
edge (+27 same-day), **and** decay-vs-recency divergence (demonstrated, n=1).
The graph is not gold-plating and decay is not a recency proxy. What remains
before P3 wiring: a real observed recency-vs-relevance miss + the material-recall
telemetry to feed `BumpOnRecall` in production.
