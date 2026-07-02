# Ensō — Seam (0b): the LeanCTX scope-expansion miss

**Date:** 2026-07-02 (Dross Hour, pt. 2, after the seam-#0 contradiction check)
**Commit:** (this session)
**Files:** `internal/bench/scope_expansion_test.go` (measurement + spec, no production change)

---

## TL;DR

Seam (0b) was opened on a premise that has since become false. **The LeanCTX H2
scope-expansion utterance is already DETECTED** — the Jun-30 `restate:scope-expansion`
signal (artifact-gated Jul-1) fires on it as a weak restate. There is no
undetected-miss bug left. The genuinely-open shape is narrower and subtler than
"a missed correction," so this session built the **measurement** that pins the
real shape and STOPPED at the seam rather than shipping a speculative extractor
for a case that does not exist in the corpus yet (n=0). Validate before construct.

---

## What the miss looks like

- **Stored belief (Apr 15):** "LeanCTX is a narrow context-trimming helper; limited scope."
- **Reality (Jun 25, Day-7 Phase-0 log, 19:09 UTC):** the tool's scope had grown;
  the note *undersold* it. Matt flagged the note as too narrow and updated it.
- **Faithful utterance (from `held_out_cases.go`):**
  `"LeanCTX does more than that now, the note undersells its current scope"`

## Current handling (verified this session)

```
DetectCorrection("LeanCTX does more than that now, the note undersells its current scope")
  => IsCorrection=true, Kind=restate, Confidence=weak,
     Signals=[restate:scope-expansion], Content=""
```

`held_out_test.go` already asserts the detector fires 2/2 on the held-out set
(H1 Granola via `restate:still-affirmative`, H2 LeanCTX via
`restate:scope-expansion`). So the recall half **and** the capture (detect) half
already handle H2 as far as they safely can from the utterance alone:

- **DETECT ✓** — weak restate.
- **RESOLVE ✓** — the narrow-scope entry is a current candidate to supersede.
- **COMMIT-with-operator-content ✓** — Content="" ⇒ the operator must supply the
  fuller capability set before `Correct` accepts it. This is the SAME
  empty-content-requires-operator invariant the reframe end-to-end test already
  pins (Jun 27). Scope-expansion shares it.

## Why it's a DIFFERENT shape from status-negation (contradiction)

The task framed it exactly right: **stored X ⊂ true capability Y — the stored
entry isn't WRONG, it's INCOMPLETE.** That distinction has a concrete home:

1. **It is NOT a contradiction.** `contradict.go` fires only when a CURRENT stored
   entry asserts a NEGATION (banned/removed/deprecated/disabled) that the utterance
   affirms against. "LeanCTX is a narrow helper" asserts no negation, so
   `DetectContradiction` correctly returns false against both the stale and the
   corrected entry. Requiring the negation from the *stored* side is exactly what
   keeps the contradiction check precise where the lexical detector can't be — and
   it is exactly why the contradiction check does NOT (and should not) cover H2.
   → pinned executably in `TestScopeExpansion_NotAContradiction`.

2. **It COLLAPSES INTO `restate` at the correction-KIND layer, and that's correct.**
   Once the scope grows, the stored *description* ("narrow; limited scope") becomes
   operatively false — **wrong-by-omission**. The right mechanism is supersession:
   close the narrow-scope entry, open a broader one. There is no fourth
   CorrectionKind needed. The "incomplete vs. wrong" distinction lives at the
   **content-extraction layer**, not the kind layer: the stored *belief* is
   incomplete; the stored *sentence*, read as the whole truth, is wrong.

## The real detection axis (where the subtlety actually is)

The scope-expansion utterance has **two sub-forms**:

- **Meta-only (the real H2):** "the note undersells its scope / does more than that
  now." Asserts *that* the stored scope is too narrow, carries **no expansion
  payload** (doesn't enumerate the new capabilities). `Content=""` is **correct** —
  nothing to extract; operator supplies. Current behavior is right.

- **Payload-bearing (synthetic; n=0 in the real corpus):** "LeanCTX also does X, Y,
  Z now." The corrected content is present and could be extracted, saving the
  operator a step.

The right detection axis for the open enhancement is therefore **payload
extraction on the payload-bearing sub-form** — NOT a new correction signal (it's
already detected) and NOT a contradiction extension (wrong shape).

## What's OPEN (and why I stopped here)

**Open:** extract the "it also/now …" capability clause from payload-bearing
scope-expansions.

**Precision evidence gathered (so the future build is safe):** a candidate capture
`\bit (?:also|now|can also|additionally)\s+(.+)$` was validated against the Jul-1
innocent corpus + comparisons:

| utterance | extracts? |
|---|---|
| "…it also does full-context assembly and prompt caching" | ✅ payload |
| "…it now also handles retrieval and ranking" | ✅ payload |
| "LeanCTX does more than that now, the note undersells its current scope" (real H2) | ∅ (correct — meta-only) |
| "This service does more than the old one…" | ∅ |
| "The dashboard does more than that already, we're fine." | ∅ |
| "the README undersells how good this tool is." | ∅ |
| "We have more than enough time." | ∅ |

So a precise extractor **exists** — but the one real case we have (H2) is
meta-only and carries no payload for it to catch. **Building the extractor now
would be constructing downstream of an n=0 validation** — the exact trap Ensō's
discipline names. So this session encodes the spec + the precision evidence as an
executable test (`TestScopeExpansion_PayloadExtractionSpec`) and stops. When a
real payload-bearing scope-expansion miss lands in the log, THAT case is the
regression target that earns the extractor.

## What shipped

`internal/bench/scope_expansion_test.go` — 3 tests, no production code touched:

1. `TestScopeExpansion_DetectedAsRestateWithEmptyContent` — pins current correct
   handling (weak restate, empty content by design). Guards against both
   un-detection AND content fabrication.
2. `TestScopeExpansion_NotAContradiction` — makes the class distinction executable
   (scope-expansion ≠ status-negation).
3. `TestScopeExpansion_PayloadExtractionSpec` — validation-before-construct
   artifact: demonstrates the current no-extraction behavior and validates a
   precise candidate extractor against the innocent corpus, without wiring it in.

`make check` green (fmt + vet + build + test + spec-drift). No benchmark
regression.

## Seam status after tonight

- (0b) scope-expansion — **✅ characterized + measured.** Not an undetected miss;
  it's a detected weak-restate whose empty content is correct for the meta-only
  form. Open sub-thread: payload extraction for the payload-bearing form,
  precision-validated but **blocked on a real n≥1 case** (deliberately not built).
- Still-open elsewhere (unchanged): contradiction corpus n≈1 (need a 2nd real
  status-negation case before tuning vocab); NEIGHBOR corpus n=1; specificity
  production-wiring; Stage 6 abstention.

#enso #memory
