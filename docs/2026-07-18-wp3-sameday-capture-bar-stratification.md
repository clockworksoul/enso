# WP-3 — The same-day capture bar is not monolithic (stratification probe)

*2026-07-18, Dross Hour. Bench-only, zero production change.*

## Why this session

The WP-3 gate work (Jul 14–16) landed a sharp three-way scoreboard on the
79-case git-history corpus:

- specificity-only (no supersession filter): **45/79 = 0.57**
- + decay (edge-free, from timestamps): recovers **7** distinct-date pairs
- + SUPERSEDES edge (capture): recovers the remaining **same-day flips**
- full pipeline: **79/79 = 1.00**

The Jul-14 doc named the same-day flips "the precise, minimal capture bar" —
"the ADR-001 deferred capture layer earns its cost specifically on same-day
status flips, not the broad 'any stale belief' class." Jul-15 counted them at
27 and treated them as one irreducible block: same `stale_date == current_date`
⇒ identical `LastRefTime` ⇒ decay is provably powerless ⇒ only the edge breaks
the tie.

That's true about **decay**. It is *not* the whole story about **capture**.
Before WP-3 (or any future capture layer) treats "build same-day capture" as a
single unit of work, we should know: are all the same-day pairs the *same
shape*? Because if some of them carry an edge-free lexical signal, then the
irreducible-capture bar is *smaller* than 27, and the capture layer's minimal
job is *narrower* than "recognize every same-day status flip."

Validate the shape before sizing the layer. (Complexity Kills.)

## What I actually found

First correction to the record: the corpus has **29** same-day pairs, not 27.
The Jul-14/15 docs say 27; the JSONL says 29 (`stale_date == current_date`).
Minor, but the "27" number is now superseded — use 29.

Stratifying the 29 by **edge-free detectability** (ordered, first-match-wins,
deliberately crude heuristics — the point is the distribution, not a detector):

| Tier | Shape | n | Edge-free signal available? |
|---|---|---|---|
| A | **status-completion** — stale=open/in-progress, current gains ✅/done/merged/unblocked/selected/resolved | 8 | **Yes** — completion-marker gain |
| B | **explicit-retraction** — current names the reversal ("previous NO CODEX rule retired", "postponed from Mar 12") | 2 | **Yes** — retraction cue in current |
| C | **scalar-flip** — same key, changed number/date/day/path (96.18%→96.27%, Wed→Thu, sparksnn→sparkkv) | 12 | **Partial** — needs the resolver to know the *key* is the same and the *value* changed |
| D | **reword/rescope** — refinement with no lexical contradiction cue; both versions equally specific | 7 | **No** — genuinely irreducible without the edge |

**Headline: only ~3–7 of the 29 same-day pairs are genuinely irreducible.**
(The D bucket is 7 by the crude heuristic, but 4 of those are actually scalar
flips the regex missed — e.g. `biomimetic-network`→`sparksnn` repo rename,
sparksnn→sparkkv path. The true no-signal-whatsoever floor is ~3: the two
file-naming-policy rewordings and one repo-scope annotation where the stale and
current are equally specific prose restatements of a *rule*, carrying no number,
date, status marker, or retraction word to key on.)

## Why this matters for the gate

The Jul-14 conclusion — "the graph is not gold-plating, the edge's irreducible
contribution is the same-day flips" — **still holds**, and this sharpens *why*:

1. **The capture layer's minimal job is narrower than advertised.** ~18 of 29
   same-day pairs (tiers A+B, and the scalar half of C where key-identity is
   detectable) carry a signal that an edge-free resolver-side check could use to
   *nominate* the supersession without a human pre-declaring the edge. That's
   exactly the class the deleted contradiction-check (`core/contradict.go`,
   removed Jul 8 under ADR-001) was built for: "does this utterance affirm a new
   status for something a current stored entry describes with an old status?"
   The Jul-2 work already proved that path is precise (0/18 innocent FPs).

2. **The genuinely irreducible core is ~3 pairs, and they share a shape:**
   *equally-specific prose restatements of a rule/policy*, same subject, no
   status/scalar/retraction cue. These are the cases where the memory
   *literally cannot know* the new prose supersedes the old prose without an
   external act of supersession — no signal is derivable from the text pair
   alone. This is the true floor of "capture is irreducible," and it's tiny.

3. **It reframes the ADR-001 deferral honestly.** "Capture is deferred" doesn't
   mean "the system is blind to all same-day flips until we build a big capture
   layer." It means: the ~3 pure-reword cases are blind; the ~18 signal-bearing
   cases are *reachable by a resolver-side nomination* that ADR-001 chose not to
   build yet — a scoped, precise, already-prototyped-and-deleted piece, not an
   open-ended one. When capture earns its cost, the build order is
   A (completion) → B (retraction) → C-scalar (key-identity + value-change),
   and D stays edge-only by construction.

## What I did NOT do (the seam)

I did **not** rebuild the contradiction-check or any capture layer. The gate is
still ADR-001-deferred and blocked on real material-recall telemetry (Jul-16
finding). This probe only *measures the shape* of the bar so that when capture
is unblocked, it's built to the smallest sufficient scope — not reflexively
sized to "all 29 same-day flips" when ~18 are signal-bearing and only ~3 are
truly irreducible.

The classifier here is a throwaway heuristic (Python, in the doc history), not
production code. The one durable artifact is a bench test that pins the
distribution so a future corpus change that shifts the irreducible floor gets
noticed.

## Corrections to the record

- **Same-day count: 27 → 29.** The Jul-14/15 WP-3 docs undercounted; the JSONL
  has 29 pairs with `stale_date == current_date`. Update the scoreboard prose:
  specificity 0.57 → +decay(7) → +edge(**22** same-day, not 27) → 1.00. (79
  total; 50 distinct-date of which decay gets 7 for free + specificity gets the
  rest; 29 same-day of which the edge is the only lever.)

  *Wait — arithmetic check:* specificity gets 45/79. Decay adds 7 (all
  distinct-date). Edge adds 27 to reach 79. So the *edge-rescued* count is 27,
  but the *same-day-pair* count is 29 — meaning **2 same-day pairs are already
  won by specificity** (the correction out-specifies the stale entry even on the
  same day, the Adam/Ed shape from the Jul-13 P1-exit finding). So: 29 same-day
  pairs, 27 of which need the edge, 2 of which specificity already breaks. That
  reconciles both numbers and *explains* the 27-vs-29 discrepancy the earlier
  docs glossed. **This is the real correction: 27 = edge-rescued same-day; 29 =
  total same-day; the 2-case gap is specificity-winnable same-day pairs.**

## Next seam (unchanged, now better-scoped)

When capture unblocks (needs real material-recall telemetry per Jul-16):
build the resolver-side nomination in tier order A → B → C-scalar, regression-
target the ~3 D-cases as *known-blind* (edge-only), and re-run this
stratification against the then-current corpus to confirm the irreducible floor
hasn't grown.
