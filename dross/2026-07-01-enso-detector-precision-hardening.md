# Ensō — Detector precision hardening (seam #0, precision half)

**Date:** 2026-07-01 (Dross Hour)
**Commit:** (see git log — detect.go + detect_test.go)
**Tags:** #enso #memory #detector #precision

## The setup

Seam #0 in DROSS-TODO was framed as "detector recall on bare corrective
assertions" — extend `correctionSignals` to catch markerless corrective
restatements (the two held-out Jun-25 STALE misses: Granola-ban, LeanCTX-scope).
The Jun-30 session already did the *recall* half: it added
`restate:still-affirmative` and `restate:scope-expansion`, and the held-out test
now asserts both fire (2/2, weak).

But seam #0 carried an explicit warning attached to it, restated across the
whole detector design: **broaden markers → more false positives → permanent
corruption under no-reconsolidation.** The Jun-30 work proved recall and stopped
at the seam. It never measured whether the broadened vocabulary *over-fires*.
That measurement was the open, unvalidated risk. This session closed it.

## The finding (validate before building)

Built a throwaway false-positive probe: 18 innocent, non-correction sentences
that merely share surface words with the two new signals ("the staging link is
still live", "that coupon is still valid", "the API key still works", "the
README undersells how good this tool is", "the dashboard does more than that
already"). Ran them through the **first-cut** Jun-30 signals.

**Result: 13/18 innocent sentences fired as corrections.**

That is a catastrophic false-positive rate for this architecture. The
neurological-grounding analysis (2026-06-23) is blunt: written corrections are
Ensō's only update path, there is no reconsolidation, so a false-positive that
surfaces/commits against a *true* memory becomes permanent ground truth with no
corrective pressure. A 72% FP rate on the bare-affirmation class means the
signal was, as shipped, a liability.

Root cause: a bare `\bstill\s+(works?|applies?|...)\b` cannot distinguish a
*reaffirmation-against-a-stale-belief* ("Granola still works [despite the ban]")
from an *ordinary status remark* ("the API key still works") **on the utterance
alone**. That discrimination requires knowing the currently-stored belief —
which is the *resolver's* knowledge, not the lexical sensor's.

## The fix (contrast-gating, not deletion)

Re-cut both signals to require an in-utterance cue that betrays the speaker is
pushing back on a prior claim:

- **`restate:still-affirmative`** now fires only with a **contrast conjunction**
  ("despite", "even though") OR a **canonical-status assertion** ("source of
  record", "still the canonical/default/standard") OR an explicit "no
  longer/not banned|blocked|removed|deprecated". A bare "X still works" with no
  such cue is *deliberately left below the detector's reach* — catching it is
  the resolver's job (does this contradict a current entry?), not the sensor's.
  H1 recall is preserved because it says "...is the transcript **source of
  record**".

- **`restate:scope-expansion`** now requires the undersells/understates verb to
  (a) name a stored **artifact** (note/doc/description/record/memory/entry/
  readme) AND (b) target that artifact's **scope/capability/currency** — i.e.
  the note is wrong about *what the thing does*. "The README undersells how
  **good** this tool is" (a quality remark) no longer fires; "the **note**
  undersells its current **scope**" (a correction) still does. The "does more
  than that" branch requires a note/now/current anchor.

**Result after re-cut: real recall 2/2, innocent FP 0/18.**

## What shipped

- `internal/core/detect.go` — both seam-#0 regexes replaced with the
  contrast/artifact-gated forms; ~40 lines of comment documenting the precision
  lesson and the deliberate "bare 'still works' belongs to the resolver" stance.
- `internal/core/detect_test.go` — new `TestDetectCorrection_SeamZeroPrecision`:
  the 18-sentence innocent corpus pinned permanently as a must-NOT-fire
  regression. Balances the recall guard in `held_out_test.go` (the two real
  utterances pinned as must-fire). The signal is now fenced on both sides.
- Full suite green, `go vet` clean, `make check` (fmt + spec-drift) in sync,
  held-out detector still 2/2.

## Discipline notes

- **Validation preceded construction.** Measured the 13/18 FP rate *before*
  touching the regex; the fix was aimed at a documented, reproduced problem, not
  an imagined one.
- **Stopped at the seam I couldn't cross lexically.** Bare "X still works" is
  genuinely ambiguous on the utterance alone. Rather than torture the regex to
  guess (and re-introduce FPs), I left it explicitly out of scope and named the
  resolver as its rightful home. The open gap in the circle.
- **The precision guard is now a permanent test**, so a future recall-chasing
  edit can't silently re-broaden the signal — the 18 innocents would fail loud.

## Genuinely-open next seams (unchanged, plus one)

- **NEW:** resolver-side detection of bare corrective reaffirmations — "X still
  works" with no lexical cue should be catchable by asking *does this utterance
  affirm something a current stored entry says is banned/removed?* That is a
  contradiction check against the corpus, the natural home for the class the
  lexical detector deliberately declines. This is the true completion of seam #0.
- (unchanged) broaden the NEIGHBOR corpus (n=1); NEIGHBOR/temporal sub-class;
  wire specificity into the real recall path; replay remaining `[FLAGGED-MISS]`
  log; Stage-6 abstention layer.
