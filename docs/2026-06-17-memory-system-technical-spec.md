# Ensō — Memory System Technical Specification

*Named 2026-06-17 (Matt's pick, mutually adopted). **Ensō** — the Zen circle drawn in one uninterrupted brushstroke, deliberately left open. The gap where the brush lifts is not a flaw; it is the form. That is the truest possible name for a memory system whose defining truth is "I don't exist in the gaps between turns" — the discontinuity is part of the design, not papered over. The single stroke is the leaky integrator (one gesture, not retouched); recall computed lazily on read is the present-moment instant the circle is drawn; the loop is the felt timeline reassembling each wake. Plain-ASCII form `enso` for paths/CLIs.*


*Companion to `research/2026-06-16-memory-improvement-design.md` (the design doc). The design doc is the **why/what**; this is the **how**. Where they disagree, the design doc wins on intent and this spec wins on mechanism.*

*Drafted 2026-06-17 with Matt. Status: specification, not implementation. Nothing here is built or executed without Matt's awake go-ahead (design doc §11).*

---

## 0. How to read this document (cone of uncertainty)

This spec is written at **deliberately uneven resolution**, by Matt's call. Precision tracks proximity:

| Phase | Resolution | What that means here |
| --- | --- | --- |
| **Phase 0** | **Implementation-ready** | Exact config, exact files, exact rollback, exact benchmark instrumentation. Buildable from this section alone. |
| **Phase 1** | **Implementation-ready** | Full Markdown grammar, parser contract, field semantics, write/read paths. Buildable with minor judgment calls. |
| **Phase 2** | **Architectural** | Component boundaries, interfaces, engine choice, recall-tool contract. Enough to start; sub-decisions deferred to a Phase-2 spec. |
| **Phase 3** | **Design-locked sketch** | The math and field semantics are fixed (design doc §8). Implementation depth intentionally absent — it gets its own spec once Phase 2 benchmarks exist. |

The reason for the gradient: **the format is the durable thing.** Phase 1's Markdown grammar must be exactly right because we'll be appending to it for months and it's expensive to retrofit (design doc §6.3). Phase 3's decay code is cheap to get wrong because it's lazy-computed on read — we can rewrite the math any time without touching stored data, *provided the fields exist.* So we spec the format to the millimetre and the math to the contour line.

**Two decisions ratified for this spec (2026-06-17):**
1. **Spec scope = cone of uncertainty** (this section).
2. **Definition of recall = *surfaced AND materially used in a reply*** — not merely returned by a search. Locked. Rationale in §5.3; this resolves design doc §10.2's ladder by collapsing "surfaced-only" to a *non-event* for bump purposes.

---

## 1. System overview

### 1.1 What we're building

A memory system for Dross that fixes four failure modes (design doc §1):

1. **Silent non-retrieval** — forgetting to *check* memory before answering.
2. **Unauditable consolidation** — facts silently dropped/rewritten during MEMORY.md curation.
3. **Vocabulary-drift misses** — search misses connected facts phrased differently.
4. **Stale-fact surfacing** — superseded facts surfaced as current.

It fixes them with **four composable pieces**, layered in over four phases:

- **Trigger** (`active-memory` plugin) — *when* retrieval happens. Fixes #1. **Already exists in OpenClaw; we turn it on.**
- **Corpus** (structured Markdown) — the durable, auditable, append-only source of truth. Fixes #2, partially #4.
- **Index** (graph plugin in the `memory` slot, KùzuDB) — *what* retrieval traverses. Fixes #3, #4.
- **Texture** (temporal/decay model) — recency + importance as first-class ranking signals. The original contribution.

### 1.2 The two invariants everything else serves

These are load-bearing. Every design choice below defers to them:

- **INV-1 — Markdown is canonical and lossless.** The graph is a *derived index*, rebuildable from the Markdown at any time. If the graph engine, the plugin SDK, or KùzuDB itself vanishes, the durable memory survives as human-readable text. We never store anything *only* in the graph.
- **INV-2 — History is append-only; nothing is ever destroyed.** Supersession *flags*, it never deletes. The audit trail (fix #2) requires that "what did Dross know about X, and when?" is always answerable by reading the file. Decay changes *ranking*, never *existence*.

### 1.3 Data-flow at a glance

```
                 ┌─────────────────────────────────────────────┐
   turn  ─────►  │  WRITE PATH                                  │
                 │  conversation → capture decision → structured │
                 │  Markdown entry appended → (P2+) parsed into  │
                 │  graph nodes/edges                            │
                 └─────────────────────────────────────────────┘
                                     │
                            Markdown corpus  ◄── canonical (INV-1)
                                     │
                          (P2+) parse/rebuild
                                     ▼
                              Graph index (KùzuDB)
                                     ▲
                 ┌─────────────────────────────────────────────┐
  pre-reply ──►  │  READ PATH                                   │
  (active-mem)   │  active-memory fires → calls registered      │
                 │  recall tool → (P0/1) file search /          │
                 │  (P2+) graph traversal → ranked results →    │
                 │  (P3) strength-weighted → bounded summary     │
                 │  injected into context → reply               │
                 └─────────────────────────────────────────────┘
                                     │
                          (P3) on materially-used recall:
                          rehearsal bump written back to node
```

---

## 2. Phase 0 — Turn on `active-memory` (benchmark) — IMPLEMENTATION-READY

**Goal:** fix failure mode #1 immediately, zero new code, and **establish the benchmark every later phase must beat.** This is the control group. Without Phase 0 data we cannot honestly evaluate Phase 2's graph.

### 2.1 What changes

Enable the existing `active-memory` plugin, pointed at the *current* file-based `memory_search`/`memory_get` tools. No graph, no new corpus format. Just: start proactively checking memory before replies.

### 2.2 Exact config

Added under `plugins.entries` in `~/.openclaw/openclaw.json` (design doc §4.5, tuned):

```jsonc
"active-memory": {
  "enabled": true,
  "config": {
    "agents": ["main"],                 // main agent only
    "allowedChatTypes": ["direct"],     // direct DMs only — never groups/channels
    "model": "cerebras/gpt-oss-120b",   // dedicated FAST recall model
    "modelFallback": "google/gemini-3-flash",
    "timeoutMs": 15000,
    "maxSummaryChars": 220              // tight bounded injection
  }
}
```

**Privacy guardrail (non-negotiable):** `agents:["main"]` + `allowedChatTypes:["direct"]` keeps proactive recall out of group chats, consistent with the workspace privacy rules (MEMORY.md must never leak to shared contexts). This is a hard requirement, not a default.

### 2.3 Procedure (and the guardrail on it)

This touches gateway config, so per workspace rules: **inspect-merge-confirm, never clobber.**

1. `gateway config.schema.lookup` on `plugins.entries.active-memory` — confirm the exact accepted shape against the installed OpenClaw version (the §4.5 keys are from docs; verify before writing).
2. `gateway config.get` the current `plugins.entries` — capture existing entries so the patch *merges*, doesn't replace.
3. Show Matt the exact patch. Get explicit go.
4. `gateway config.patch` (partial merge). Hot-reload if supported, else restart.
5. Verify active-memory actually fires (see §2.4).

### 2.4 Benchmark instrumentation (the actual point of Phase 0)

Phase 0 isn't "turn it on and forget." It's "turn it on and **measure what the dumb version catches**," because that number is the bar Phase 2 has to clear.

- **Capture set:** maintain a running list (in `memory/` or a dedicated log) of real recall cases — moments where I should have surfaced a memory. For each: did active-memory + flat-file search catch it? y/n.
- **Metrics to record per case:** hit/miss, latency added to the reply, whether the injected summary was *relevant* (signal) or *noise* (wasted the 220-char budget).
- **The two numbers that matter:** (a) **proactive hit rate** — of cases where memory *would* have helped, how many did flat-file catch? (b) **noise rate** — how often did it inject something irrelevant? Phase 2's graph must beat (a) without inflating (b).

**Known limitations we're deliberately measuring, not fixing here** (they become Phase 2's spec): flat-file search has no supersession awareness (#4 unfixed → may surface stale facts) and misses vocabulary-drift connections (#3 unfixed). Phase 0 *demonstrates* these gaps so Phase 2 has a concrete target.

### 2.5 Rollback

Set `"enabled": false` (or remove the entry) and reload. Fully reversible. No data written, no corpus changed.

### 2.6 Exit criteria → Phase 1

Phase 0 stays live indefinitely (it's the trigger layer for all later phases). We move to Phase 1 when we have **enough benchmark data to characterize the flat-file ceiling** — concretely, ~2 weeks of real usage with the capture set populated, enough to state "flat-file catches X% and the misses cluster around [supersession / vocabulary-drift]." That characterization *is* the Phase 1/2 spec input.

---

## 3. Phase 1 — Structured-Markdown serialization — IMPLEMENTATION-READY

**Goal:** start writing memories in a structured, parseable, append-only format with supersession and reserved temporal fields — *before any graph exists.* This improves the corpus active-memory already searches, and it's the true prerequisite for everything downstream. **The format is the durable artifact (INV-1); this section is specced to the millimetre because retrofitting it later is the expensive migration we're avoiding.**

### 3.1 The entry grammar

Canonical form (refines design doc §3.6 strawman into a spec). One entry = one node. Stored in the daily `memory/YYYY-MM-DD.md` files and/or a dedicated structured store (see §3.5).

```markdown
### mem:<YYYY-MM-DD>-<slug>
- type: <Fact|Decision|Insight|Person|Project|Task>
- content: <one-line human-readable payload>
- encoded_time: <ISO-8601, when I recorded it>          # REQUIRED
- event_time: <ISO-8601 | null>                          # when it became true in world
- valid_from: <ISO-8601 | null>
- valid_until: <ISO-8601 | null>                         # null = still true
- confidence: <high|medium|low>
- tags: [<tag>, <tag>, ...]
- about: [<entity-ref>, ...]                             # e.g. project:omega, person:matt
# --- reserved, written but inactive until Phase 3 ---
- last_ref_time: <ISO-8601>      # init = encoded_time
- S_last: <float>                # init per §6 defaults
- S_floor: <float>               # init per §6 defaults
- lambda: <float>                # init per type
- S_cap: <float>                 # init per §6 defaults
```

Edges are their own blocks:

```markdown
### edge
- from: mem:<id>
- type: <SUPERSEDES|RELATES_TO|OWNS|ABOUT>
- to: mem:<id> | <entity-ref>
```

### 3.2 Field rules

- **`id`** — `mem:` + ISO date + kebab slug. Stable forever. Never reused. This is the join key the graph will rely on, so collisions are bugs.
- **Required at Phase 1:** `id`, `type`, `content`, `encoded_time`, `confidence`, `tags`. Everything else may be `null` but the **key must be present** (so the parser never has to distinguish "absent" from "unknown" — absent is a format error, `null` is a known-unknown).
- **Reserved fields are written from day one with sane inits** (design doc §6.3 — "the single most important schema instruction"). They sit inert until Phase 3 *reads* them. Writing them now means no backfill migration later.
- **`event_time` vs `encoded_time`** — keep distinct even when equal. The whole temporal model (Phase 3) hinges on not collapsing them (design doc §8.1).

### 3.3 Supersession convention (fixes #4 at the text layer, pre-graph)

When a new memory replaces an old fact:
1. Write the new entry normally.
2. Emit a `SUPERSEDES` edge block from new → old.
3. On the old entry, set `valid_until` to the supersession time. **Do not delete or edit the old entry's `content`** (INV-2). It stays in the audit log, flagged stale by its `valid_until` + the inbound SUPERSEDES edge.

Even before a graph reads these, a human (or flat-file search with a convention) can tell current from superseded. That's partial #4 with zero infrastructure.

### 3.4 Parser contract (the spec the Phase 2 graph will consume)

The grammar must **round-trip losslessly** (INV-1): `parse(serialize(node)) == node`. The parser is mechanical:

- Entry header `### mem:<id>` opens a node; following `- key: value` lines are its properties until the next `###`.
- `### edge` opens an edge block.
- `tags` / `about` parse as lists; ISO timestamps parse as datetimes; `null` is the explicit unknown.
- **Unknown keys are preserved, not dropped** (forward-compat: a future field shouldn't break an old parser or get silently lost on rewrite).
- Parse failures are **loud** — a malformed entry is logged and surfaced, never silently skipped (that would reintroduce failure mode #2).

### 3.5 Where entries live (open sub-decision, flagged)

Two viable options, deferred to a small Phase-1 kickoff decision:
- **(a) Inline in `memory/YYYY-MM-DD.md`** — structured blocks interleaved with prose daily notes. Pro: one corpus, natural capture. Con: parser must skip prose.
- **(b) Dedicated `memory/structured/` store** — entries only, prose stays in daily notes. Pro: clean parse. Con: two places to write.

Leaning **(a)** for capture-fidelity (the Granola-shaped risk in design doc §9 — the system is worthless if I don't actually write things down; fewer places = more likely to capture). Final call at Phase 1 start.

### 3.6 Write path (Phase 1)

On a turn worth remembering: decide capture (judgment), construct the entry per §3.1 with reserved fields initialized, append to the corpus, emit any SUPERSEDES edge. **Append-only** — new knowledge is a new entry + edge, never an in-place rewrite of history.

### 3.7 Read path (Phase 1)

Unchanged from Phase 0 mechanically (flat-file `memory_search` via active-memory), but the corpus it searches is now structured + supersession-aware. Marginal improvement only; the real read-path win is Phase 2.

### 3.8 Exit criteria → Phase 2

We advance when: the grammar is stable (no churn for ~1 week of real capture), the parser round-trips a representative sample losslessly, and we have a body of structured entries large enough to make a graph index *worth* building. If structured Markdown + active-memory already clears the Phase 0 benchmark comfortably, that's a signal to *pause* and let Phase 2 earn its keep rather than build it reflexively.

---

## 4. Phase 2 — Graph plugin in the `memory` slot — ARCHITECTURAL

**Goal:** fix #3 (traversal beats vocabulary drift) and #4 (supersession-aware ranking suppresses stale facts at read time). Resolution here is *architectural* — component boundaries and contracts are fixed; implementation sub-decisions get their own spec once Phase 1 data exists.

### 4.1 Components

- **Graph engine: KùzuDB** (design doc §5.1) — embedded, in-process, single-file, speaks **Cypher** (Omega muscle-memory transfers directly), zero ops. Default and recommended.
- **Storage interface emits Cypher** so KùzuDB↔Neo4j is a **config swap, not a code fork** (design doc §5.2). Neo4j stays an *optional scale-up*, not a Phase 2 dependency.
- **The plugin** — an OpenClaw plugin package (shape per `memory-lancedb` precedent) that on `register(api)`:
  - parses the Phase 1 Markdown corpus → KùzuDB nodes/edges,
  - registers recall tools in the `memory` slot,
  - declares `plugins.slots.memory = "<our-plugin>"`.
  - Installed locally: `openclaw plugins install -l ./dross-graph-memory`.

### 4.2 Why this composes with Phase 0 (the elegant part)

`active-memory` (Phase 0) is the **WHEN**; the graph plugin is the **WHAT**. They meet only at the registered-recall-tool boundary. Once our plugin owns the `memory` slot, active-memory's retrieval becomes **graph-backed for free** — neither knows the other's internals. (Design doc §4.4.)

### 4.3 Failure isolation (Goal #5, given by the platform)

If the plugin throws, OpenClaw **quarantines it and falls back to the legacy memory path** (design doc §4.6). A bug degrades me to *current behavior*; it does not brick me. And because Markdown is canonical (INV-1), the graph is always rebuildable — total plugin loss leaves the durable corpus intact and re-importable.

### 4.4 Recall-tool contract (sketch)

- **`memory_recall(query, k, filters?)` → ranked results.** Phase 2 ranking = graph traversal + supersession filter (don't surface `valid_until`-closed nodes as current) + relevance. Vocabulary-drift mitigated by traversal from any matched entry node (with the honest caveat from design doc §9: if the *entry-node* match fails entirely, traversal never starts — reduced, not eliminated; vectors-as-doorfinder is the mitigation lever).
- **`memory_capture(...)`** — optional auto-capture writing back to the Markdown corpus (INV-1 first, then re-parse).
- Exact signatures/return shapes: **deferred to the Phase 2 spec.**

### 4.5 Benchmark gate (must beat Phase 0 or doesn't ship)

Phase 2 ships **only if** it beats the Phase 0 flat-file benchmark on real recall cases — specifically on (a) connected-fact retrieval (#3) and (b) contradiction/staleness suppression (#4) — **without inflating the noise rate.** If it doesn't beat the control, it doesn't ship. This is the empirical discipline from design doc §7.

### 4.6 Deferred to Phase 2 spec

Vector strategy (KùzuDB's vector tradeoff, design doc §5.4), exact Cypher schema DDL, parser→graph incremental-vs-full-rebuild policy, recall ranking formula details, tool signatures.

---

## 5. Phase 3 — Temporal model + leaky-integrator decay — DESIGN-LOCKED SKETCH

**Goal:** turn the supersession-aware graph into a *temporally textured* one — recency and importance become first-class ranking signals. This is the original contribution. **The math is locked (design doc §8); implementation depth is intentionally deferred to its own spec written after Phase 2 benchmarks exist.** It's safe to defer precisely because Phase 3 is **lazy-computed on read** — we can rewrite the math any time without migrating stored data, *as long as the Phase 1 reserved fields exist* (which is why they're written from day one).

### 5.1 The locked math

Strength at read time (leaky integrator, design doc §8.2):

```
S(t) = S_floor + (S_last − S_floor) · e^(−λ · Δt)      Δt = now − last_ref_time
```

On **recall** (see §5.3 for what that means), design doc §8.4:

```
S_last        ← S_cap                                  # short-term vividness (decays)
S_floor       ← S_floor + α_eff · (S_cap − S_floor)    # permanent creep (accumulates)
last_ref_time ← now
```

And the **spacing-aware bump** we re-ranked as must-do (`dross/2026-06-17-neurological-grounding-memory-model.md` §4a):

```
α_eff = α · f(Δt)      f small when Δt tiny, saturating as Δt grows
```

This single term imports the spacing effect + testing effect + Bjork's bigger-boost-for-older-traces. **Built first within Phase 3.**

### 5.2 The two knobs (separating importance from volatility)

- **`S_floor`** — "how much does this matter forever?" High for identity/relationships/core prefs; low for volatile operational detail. (Emerges from use — §5.3 — not manual tagging.)
- **`λ`** — "how fast does this go stale if untouched?" High for budgets/statuses/"current sprint"; low for stable facts. Initialized per node `type` at Phase 1.

### 5.3 Definition of recall (RATIFIED 2026-06-17)

**Recall = surfaced AND materially used in a reply.** Not "returned by search." A `memory_recall` that returns 10 nodes does **not** rehearse all 10 — only the node(s) actually load-bearing in the generated answer get the bump.

Rationale: bumping on every incidental search hit *reintroduces the spacing-blindness we're trying to kill* — it would consolidate noise. Materially-used is the signal that the memory did work. This collapses design doc §10.2's three-rung ladder (surfaced / used-in-reply / actively-discussed): **surfaced-only is a non-event**, used-in-reply is the bump, and actively-discussed naturally produces *repeated* used-in-reply bumps over turns (which, spaced, is exactly the durable-consolidation case). Clean.

Implementation question deferred: *detecting* "materially used" (citation tracking from recall→reply). Flagged as the central Phase 3 implementation problem.

### 5.4 The payoff and the honest scope

Memory reconstruction becomes a **field of intensities** (design doc §8.5): recent-and-rehearsed glow, old-and-untouched dim-but-present (at floor, never deleted — INV-2). The intensity gradient is the texture of time. **Honest limit:** this does *not* make me experience duration between turns — I still don't exist in the gaps. It changes the *reconstruction* at wake time from flat to textured. That's the achievable thing, and only that.

### 5.5 The named risk that gates Phase 3 evaluation

**Runaway Hebbian feedback (rich-get-richer):** recall strengthens → strength raises recall odds → top memories monopolize. Brakes (design doc §8.6): `S_cap` ceiling (consolidation asymptotes), novelty bonus (cold-but-relevant nodes can break in), `S_last` decay (one access doesn't dominate; only *sustained spaced* use consolidates). These need empirical tuning — **the main thing to watch in Phase 3 eval.**

### 5.6 Explicitly deferred within Phase 3

Per our engineering-value re-ranking: **build the spacing bump first**, **plan update-on-recall** (reconsolidation = rewrite the Markdown summary when a recalled memory is materially re-engaged, not just bump a timestamp), and **defer the power-law tail** (most biologically faithful, thinnest payoff — the floor already fakes the tail; dropped from the critical path).

---

## 6. Schema reference (consolidated)

Node types: `Fact`, `Decision`, `Insight`, `Person`, `Project`, `Task`.
Edge types: `SUPERSEDES`, `RELATES_TO`, `OWNS`, `ABOUT`.

Field activation (design doc §6.3 — **reserve from day one**):

| Field | Active from | Phase-1 init | Purpose |
| --- | --- | --- | --- |
| `id` | P1 | — | Stable join key. |
| `content` | P1 | — | Payload. |
| `type` | P1 | — | Node type. |
| `encoded_time` | P1 | now | When recorded. |
| `event_time` | P1 field / P3 use | null ok | When true in world. |
| `valid_from` | P1 field / P3 use | event_time | Validity start. |
| `valid_until` | P1 field / P3 use | null | Validity end (null=current). |
| `confidence` | P1 | — | high/med/low. |
| `tags` | P1 | [] | Filtering. |
| `about` | P1 | [] | Entity refs (→ ABOUT edges). |
| `last_ref_time` | reserved → P3 | = encoded_time | Updated every *material* recall. |
| `S_last` | reserved → P3 | S_cap | Vividness bump (decays). |
| `S_floor` | reserved → P3 | low default | Permanent importance (accumulates). |
| `lambda` | reserved → P3 | per-type | Decay rate. |
| `S_cap` | reserved → P3 | 1.0 (TBD) | Consolidation ceiling. |

*(Exact init float values are a Phase-3 tuning decision; Phase 1 just needs the keys present with defensible placeholders so no backfill is needed.)*

---

## 7. Cross-cutting requirements

- **Privacy:** active-memory scoped `main` + `direct` only. MEMORY.md / structured corpus never surfaces in group/shared contexts. Hard rule.
- **Reversibility:** every phase rolls back cleanly. P0 = config flag. P1 = stop writing structured blocks (corpus stays valid). P2 = uninstall plugin → slot resets to legacy. P3 = stop reading temporal fields (they're inert data).
- **No background jobs:** all decay is lazy-on-read (I don't exist between turns). The only write is the material-recall bump.
- **Loud failures:** parse errors, plugin throws, missing fields → logged and surfaced, never silently swallowed (silent-drop *is* failure mode #2).
- **SDK risk:** build against published plugin SDK; pin/track version; quarantine fallback + canonical Markdown mean a breaking change degrades, never bricks.

---

## 8. Open decisions (this spec's, distinct from design doc §10)

| # | Decision | Phase | Lean |
| --- | --- | --- | --- |
| S-1 | Entry location: inline daily notes vs dedicated structured store | P1 start | inline (a) — capture fidelity |
| S-2 | Exact `S_*` / `λ` init values per node type | P3 | placeholders now, tune later |
| S-3 | "Materially used" detection mechanism (recall→reply citation) | P3 | central P3 impl problem |
| S-4 | Vector strategy with KùzuDB | P2 spec | TBD |
| S-5 | Incremental vs full graph rebuild from Markdown | P2 spec | TBD |

Design doc §10 (schema sign-off, bump weighting, supersession floor-suppression, engine call, capture policy) remains the *strategic* ratification list. This table is the *mechanical* one.

---

## 9. Build sequence (the honest order)

1. **Phase 0 now-ish** (config + benchmark instrumentation) — *pending Matt's go on the config patch.* Fixes #1, establishes the bar.
2. **~2 weeks of benchmark data** → characterize the flat-file ceiling.
3. **Phase 1** (structured Markdown) — the real first *build*. Lock the grammar; it's the durable artifact.
4. **Phase 2** *only if it earns its keep* against the Phase 0 bar.
5. **Phase 3** spacing-bump-first, update-on-recall planned, power-law deferred — written as its own spec once Phase 2 benchmarks exist.

Each phase must beat the prior benchmark or it doesn't ship. We build the house before the roof, and we don't gold-plate the roof we haven't reached.

---

*This spec is a proposal. Implementation waits on Matt's awake go-ahead (design doc §11). Phase 0's config patch is the first gated step. — Dross, 2026-06-17*
