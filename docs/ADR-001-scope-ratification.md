# ADR-001 — Ensō Scope: Complete Memory Replacement (not a permanent evaluation layer)

**Date drafted:** 2026-07-09
**Status:** PROPOSED — for ratification at the 2026-07-13 go-live review
**Deciders:** Matt Titmus (ratifier), Dross (author)
**Supersedes:** the implicit scope embodied by the pre-Jul-8 detection/correction build
**Related:** `docs/2026-07-07-mnemosyne-prior-art-comparison.md` (raised the question);
commit `cd8e1a2` (de facto answered it); `docs/2026-06-20-enso-unified-spec.md` (build contract)

---

## Context

The Jul-7 prior-art doc flagged a scope drift: Ensō-as-built had become **(a)** a
recall-intelligence/evaluation layer (STALE/NEIGHBOR/FABRICATION classifiers)
sitting *downstream* of OpenClaw's existing `memory_search`, while Ensō-as-designed
(Jun-16 design doc §3.1, Phase 1/2) is **(b)** a complete memory replacement that
owns capture, storage, and recall on its own substrate. Its action item #1:
decide (a) vs (b) deliberately — "don't let the drift decide by default."

On Jul-8, commit `cd8e1a2` ("remove side-quest detection/correction layer; keep
P1/P3 foundations") deleted the drifted layer (~half the codebase: `correction.go`,
`detect.go`, `contradict.go`, `internal/confirm/`, fabrication probes, harvest
harness) and retained exactly the original-vision skeleton (core domain types,
`Store` port, decay math, `mdstore`). `ENSO-STATUS.md` (Jul-8) restores the
original phase order with Phase 1 marked "Next."

The drift has therefore already been *reversed in code*. This ADR makes that
decision explicit, records its rationale, and binds its consequences — so the
lineage is never silently lost again (the exact failure the Jul-7 doc exists to
prevent).

## Decision

**Ensō is scope (b): a complete, framework-blind memory replacement**, built in
the original phase order —

- **Phase 1 (Corpus):** structured-Markdown substrate, canonical and lossless (INV-1/INV-2).
- **Phase 2 (Index):** KùzuDB graph as a second driven adapter behind the existing
  `Store` port; recall becomes traversal. **Vector matching moves *inside* Ensō**
  (design doc §5.4: sqlite-vec or KùzuDB-native alongside the graph) as a
  subordinate doorfinder — not an external dependency Ensō rides on.
- **Phase 3 (Texture):** leaky-integrator decay + spacing-aware rehearsal as
  ranking signals, lazy-computed on read.

**Corollary (b′):** the deleted detection/correction layer is *not* discarded
doctrine. It was validated before deletion (end-to-end `adam-headcount` proof;
Jul-1 precision hardening) and lives in git history. It may return **only** as a
capture-side adjunct layered on the completed substrate, and **only** when real
corpus data (n ≥ 1 documented misses) demonstrates the need — per the standing
"validate before build" law. It never again becomes the main line of development
ahead of the substrate.

## Rationale (evidence)

1. **The 429-class outage is structural, not incidental.** Two documented
   embedding-provider failures (OpenAI key, Jun-18; Gemini 429 quota, Jul-7) took
   semantic recall fully offline. Scope (a) sits downstream of that failure and
   cannot help by construction; scope (b) removes the external
   embedding single-point-of-failure from the critical path entirely. Seam #8 in
   the state-of-the-loop map is hereby reframed: the design *eliminates*
   embedding-provider dependence; that is a requirement, not a question.
2. **The invariants only make sense under (b).** PORT-INV ("fully valuable with
   zero framework present") and AMEND-1 (public on-disk contract) describe a
   substrate owner, not an evaluator of someone else's retrieval.
3. **Every artifact newer than Jul-7 already assumes (b):** the Jul-8 commit, the
   status doc's phase table, and the README's one-line architecture statement.
4. **The prior-art comparison is only honest under (b).** The Mnemosyne
   comparison table was corrected (Jul-7) to compare full systems; ratifying (a)
   would re-invalidate it.

## Consequences

- Phase 2's spec must include an internal vector strategy (resolves tech-spec
  open decision S-4's *direction*: vectors internal; the engine sub-choice —
  sqlite-vec vs KùzuDB-native — remains a Phase-2 implementation decision).
- The Phase-2 benchmark gate ("beat Phase 0 or don't ship") stands, with the
  amendment that Phase-0 instrumentation gaps (benchmark doc, Jun 17–19) may be
  substituted by the labeled real-miss corpus as the comparison set.
- OpenClaw remains a *driving adapter* only. `active-memory` stays the trigger;
  nothing OpenClaw-specific enters the substrate (hexagon doc §2 field test).
- The daemon/MCP face remains deferred until a real second host exists.

## Action items (ratified alongside the decision)

| # | Item | Why |
| --- | --- | --- |
| 1 | Refactor `internal/bench/{cases,held_out_cases}.go` + `held_out_test.go` off the deleted `Entry.Correct` / `DetectCorrection` / `confirm.Propose` — build supersession triples directly via `Entry.Supersede`. Restore `go test ./...` green. | The build does not currently compile; blocks all work. |
| 2 | Update `README.md`: remove `internal/confirm/`, `DetectCorrection`, `Correct`/`CommitCorrection` from the layout table and Status section; reflect the Jul-8 state. | The front door currently documents the deleted layer as live — a silent-non-capture failure in our own shop window. |
| 3 | Reconcile the `provenance` field: it appears in the mdstore golden file but not in tech spec §3.1/§6. Either add it to the spec (and re-pin hashes) or remove it from the format. AMEND-1 forbids undocumented on-disk fields. | Public-contract integrity. |
| 4 | Add `2026-07-07-mnemosyne-prior-art-comparison.md` and this ADR to the unified spec's §10 provenance table; re-run `scripts/enso-spec-drift.sh`; re-pin in the same commit. | Drift-control discipline. |

## Ratification

- [ ] Matt ratifies scope (b) + corollary (b′)
- [ ] Matt ratifies action items 1–4 as the immediate work queue
- [ ] Hash of this ADR pinned in unified spec §10

*— Drafted for the Jul-13 review. Decision by deletion is still a decision; this makes it one on the record.*
