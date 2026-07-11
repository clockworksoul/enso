# Ensō — Unified Design Spec (Snapshot)

**Date:** 2026-06-20
**Author:** Dross (compiled in conversation with Matt Titmus)
**Status:** SNAPSHOT — build contract. This is the single north star to construct against. It **owns** the consolidated invariants, architecture, and phase plan, and **delegates depth** to four still-living source docs (§9). It does **not** supersede them: they remain the canonical rationale record. Implementation is still gated on Matt's explicit go-ahead (design doc §11).

> **What this doc is and isn't.** This is a *snapshot that points back*, not a living document that absorbs its sources. The four source docs continue to evolve as the rationale record (the "why we believe this," including the option-space walks and the neuro grounding). This snapshot is the *plan you build from*. The risk that buys us — a source doc changing in a way that conflicts with another source or silently invalidates this snapshot — is defended against mechanically by the **provenance table (§10)** and the **drift-check script** (`scripts/enso-spec-drift.sh`), not by memory. **If §10's hashes don't match, this snapshot is stale until reconciled.**

---

## 1. The rules you cannot break (consolidated invariants)

Stated once, authoritatively. Everything downstream defers to these. Sourced from design doc §3.2, tech spec §1.2, and the hexagon doc §1/§5.

- **INV-1 — Markdown is canonical and lossless.** The graph/index is a *derived, rebuildable* cache. Nothing is stored *only* in the graph. If the graph engine, plugin SDK, or KùzuDB vanishes, durable memory survives as human-readable text. *(tech spec §1.2)*
- **INV-2 — History is append-only; nothing is ever destroyed.** Supersession *flags*; it never deletes. "What did Dross know about X, and when?" is always answerable by reading the file. Decay changes *ranking*, never *existence*. *(tech spec §1.2)*
- **PORT-INV — The memory must be fully valuable, and fully reconstructable, with zero framework present.** `git clone` the directory onto a bare machine with no OpenClaw/daemon/agent, and a human or fresh framework can reconstruct complete state. This is the portability reading of INV-1/INV-2, not new machinery. *(hexagon doc §1)*
- **AMEND-1 — The `Store` adapter's on-disk format is a public, documented contract**, not an opaque implementation detail. This is the one deliberate violation of classic hexagonal architecture, made *in service of* PORT-INV. A human is explicitly allowed to "read the database directly," bypassing all code. *(hexagon doc §5)*
- **RECALL-DEF — A memory counts as "recalled" only when it is *surfaced AND materially used* in a reply** — not merely returned by a search. This is the bar Phase 0 benchmarking measures and the event that earns a strength bump. *(tech spec §0; design doc §10.2)*

Two meta-requirements that also gate every design choice (design doc §2 goals #5/#6):

- **Fail-safe.** Any new machinery must degrade to *current* behavior on bug, never brick. (OpenClaw plugin quarantine gives this for free.)
- **Upgrade-safe.** No core fork. Build against the published SDK; survive OpenClaw upgrades.

---

## 2. The four composable pieces

Ensō fixes four failure modes (design doc §1, tech spec §1.1) with four composable pieces, layered over four phases:

| Piece | What it is | Fixes | Status |
| --- | --- | --- | --- |
| **Trigger** | `active-memory` plugin — *when* retrieval fires | #1 silent non-retrieval | **Exists in OpenClaw; turned on (Phase 0 live)** |
| **Corpus** | structured Markdown — durable, auditable, append-only source of truth | #2 unauditable consolidation, partially #4 | Phase 1 |
| **Index** | graph plugin in `memory` slot (KùzuDB) — *what* traversal walks | #3 vocabulary drift, #4 staleness | Phase 2 |
| **Texture** | temporal/decay model — recency + importance as ranking signals (the original contribution) | ranking quality | Phase 3 |

The four failure modes, for reference: **#1** silent non-retrieval (forgetting to *check*), **#2** unauditable consolidation (facts silently dropped/rewritten), **#3** vocabulary-drift misses (connected facts phrased differently), **#4** stale-fact surfacing (superseded facts surfaced as current).

---

## 3. The architecture: a memory core in a hexagon

The organizing frame is **hexagonal architecture (ports & adapters)** (hexagon doc §4). The four pieces of §2 map onto it: the **Texture** (decay) + recall semantics + append discipline are the **core/domain**; **Corpus** (Markdown) and **Index** (graph) are **driven adapters** behind a `Store`/`Index` port; the **Trigger** (active-memory) is a **driving adapter**.

```
                 Driving adapters (disposable, per-framework)
        ┌───────────────┬───────────────┬──────────────────┐
        │ OpenClaw      │ MCP daemon    │ CLI / scripts    │
        │ plugin        │ (foreign      │                  │
        │ (links core)  │  hosts, opt.) │                  │
        └───────┬───────┴───────┬───────┴────────┬─────────┘
                │ Driving port (inbound):         │
                │ Recall(query,ctx)·Append(e)     │
                │ Search·Consolidate              │
                ▼               ▼                ▼
        ┌─────────────────────────────────────────────────┐
        │                 ENSŌ CORE (domain)               │
        │  decay model · recall semantics · append         │
        │  discipline · spacing-aware rehearsal            │
        │  — depends on NOTHING outward —                  │
        └───────────────────────┬─────────────────────────┘
                Driven port (outbound): Store (· Index later)
        ┌───────────────────────┴─────────────────────────┐
        │  ┌─────────────────────┐  ┌────────────────────┐ │
        │  │ Markdown FS store    │  │ Graph store (P2,    │ │
        │  │ canonical (INV-1)    │  │  KùzuDB) — same     │ │
        │  │ ★ PUBLIC CONTRACT ★  │  │  Store port         │ │
        │  └─────────────────────┘  └────────────────────┘ │
        └───────────────────────────────────────────────────┘
```

**Two things the hexagon earns** (hexagon doc §4):
1. **The Phase 2 graph hides behind the `Store` driven-port.** Markdown store and graph store are two adapters behind one port; the core never knows which it talks to. Phase 1→Phase 2 becomes "add a second driven adapter," not a migration. The graph is a derived read-model the core treats as a cache (consistent with INV-1).
2. **It resolves portability vs. multi-framework structurally** via *share the code, not the runtime*: the core hexagon **is** the shared library. In-process (OpenClaw) vs. over-the-wire (MCP daemon) is purely a driving-adapter choice; behavior never forks.

**Architecture decision (hexagon doc §3):** Option C (embeddable core library + files) as the spine; Option D (optional daemon/MCP face) as an escape hatch; **explicitly not** Option B (mandatory daemon/MCP on the hot path). MCP is *one driving adapter among several*, switched on only for hosts that can't link the core directly — never on OpenClaw's critical path.

**Engine:** KùzuDB (embedded graph) for the Index. *(design doc §5; tech spec §4.)*

---

## 4. Phase plan (preserving the cone of uncertainty)

Resolution tracks proximity — *deliberately uneven*, by Matt's call (tech spec §0). The unified spec **preserves** this gradient rather than flattening it; do not over-build the speculative phases.

| Phase | Resolution | What it is | Build coordinates |
| --- | --- | --- | --- |
| **Phase 0** | **Implementation-ready / LIVE** | Turn on `active-memory` (the Trigger); benchmark flat-file recall | tech spec §2; benchmark doc |
| **Phase 1** | **Implementation-ready** | Structured-Markdown serialization (the Corpus) — full grammar, parser contract, field semantics, write/read paths | tech spec §3, §6 |
| **Phase 2** | **Architectural** | Graph plugin in `memory` slot, KùzuDB (the Index) — component boundaries, recall-tool contract; sub-decisions deferred | tech spec §4; design doc §5 |
| **Phase 3** | **Design-locked sketch** | Temporal model + leaky-integrator decay (the Texture) — math + field semantics fixed; implementation depth deferred | tech spec §5; design doc §8 |

**Why the gradient is a feature:** the *format* is the durable thing (tech spec §0). Phase 1's Markdown grammar must be exactly right — we append to it for months and retrofitting is expensive. Phase 3's decay math is cheap to change because it's lazy-computed on read — rewrite the math any time *provided the fields exist*. So: spec the format to the millimetre, the math to the contour line.

**Build sequence (the honest order):** tech spec §9 is authoritative. Phase 0 (live) → Phase 1 Corpus → Phase 2 Index → Phase 3 Texture.

### Cross-cutting constraint: portability discipline (Phases 1–2)

Not a phase — a discipline layered over Phases 1–2 (hexagon doc §6). Costs nothing if done now; expensive to retrofit:

1. **Every Phase-1 field must pass "would this make sense with no framework attached?"** Framework-meaningful fields (session keys, tool-call IDs, gateway paths, channel envelopes) go in an *adapter*, never the substrate.
2. **Specify the substrate format as a public contract (AMEND-1), readable from a README alone.** If recall ever *requires* code to even understand the files, portability dies and we're silently back in Option B.
3. **Draw the `Store` driven-port boundary deliberately in Phase 1/2** so Phase 2 graph is a second adapter, not a rewrite.
4. **Core library framework-blind from line one.** No OpenClaw imports into the domain, ever. The plugin depends on the core; the core never depends on the plugin.
5. **Defer the daemon/MCP face until a real second host exists.** Build it on demand, not speculatively.

---

## 5. Write path & read path (canonical flow)

**Write path** (design doc §3.3) — log-first, apply-second (write-ahead-log discipline):
```
new memory → append structured Markdown entry   (durable commit — always first)
           → upsert node/edges into graph        (fast read cache; P2+)
```
If the graph write fails, the Markdown entry still captured it; the next rebuild reconciles. The durable commit never depends on the graph being healthy.

**Read path** (tech spec §1.3):
```
pre-reply → active-memory fires → calls registered recall tool
          → (P0/1) file search / (P2+) graph traversal
          → ranked results → (P3) strength-weighted → bounded summary
          → injected into context → reply
```

---

## 6. Phase 0 status (live)

Enabled 2026-06-17. Scope = main agent + direct DMs only. Model `anthropic/claude-sonnet-4-6`, queryMode=recent, promptStyle=balanced, thinking=off, timeoutMs=15000, maxSummaryChars=220, logging=true. Currently in ~2-week benchmark-collection window measuring hit rate / noise rate against RECALL-DEF — this becomes the bar a Phase 2 graph must beat. Decision criteria + log: benchmark doc. *(See MEMORY.md June 17 section for the live config record and the open instrumentation-gap note.)*

---

## 7. Open decisions (carried, not resolved here)

This snapshot does not resolve open decisions; it *points* to them so construction knows what's still soft.
- **Design doc §10** — open decisions for Matt to ratify (the original set).
- **Tech spec §8** — the spec's own open decisions, distinct from design doc §10.
- **Instrumentation gap (open, from MEMORY.md Jun 17):** active-memory plugin confirmed loaded but its logging output location is unconfirmed in the gateway log. Track before leaning hard on Phase 0 numbers.

---

## 8. One-line statement of the architecture

> **A framework-blind Markdown substrate, a small embeddable core library as the reference behavior, and adapters (including an optional MCP daemon) as the only framework-aware parts — with the hard rule (AMEND-1) that the files must remain fully interpretable with no code running at all.**

---

## 9. Source docs (delegated depth — the rationale record)

This snapshot owns invariants/architecture/phases; for depth, go to source. These remain **living**; this snapshot must be reconciled if they change (§10).

| Source | Owns (depth) | Key sections |
| --- | --- | --- |
| `2026-06-16-memory-improvement-design.md` | The **why/what**. Problem statement, goals, graph rationale, OpenClaw integration discovery, engine selection, schema proposal, **temporal model (the original contribution)**, risks, open decisions. | §1 problem, §2 goals, §3 architecture, §4 OpenClaw integration, §5 engine, §6 schema, §8 temporal model, §10 open decisions, §11 gating |
| `2026-06-17-memory-system-technical-spec.md` | The **how**. Cone of uncertainty, per-phase mechanism, **full Markdown grammar/parser contract**, recall-tool contract, consolidated schema, build sequence. | §0 cone, §2 Phase 0, §3 Phase 1 grammar, §4 Phase 2, §5 Phase 3, §6 schema ref, §8 open decisions, §9 build order |
| `2026-06-17-phase0-benchmark.md` | The **measurement**. What/why we measure, config under test, capture method, decision criteria, running log. | full doc |
| `2026-06-20-enso-hexagonal-portability-architecture.md` | The **portability architecture**. PORT-INV, coupling risk, option-space A/B/C/D, the hexagon mapping, AMEND-1, actionable residue. | §1 PORT-INV, §2 coupling risk, §3 options, §4 hexagon, §5 AMEND-1, §6 residue |
| `2026-07-07-mnemosyne-prior-art-comparison.md` | The **prior-art comparison** that surfaced the scope-drift question (eval-layer vs. complete replacement) and the anti-sprawl lessons. | full doc |
| `adr/ADR-001-scope-ratification.md` | The **scope decision**: Ensō is a complete memory replacement (scope b), not a permanent evaluation layer — with corollary b′ (deleted detection layer returns only by real misses, never before WP-4). | full doc |

---

## 10. Provenance & drift control (the staleness defense)

**This snapshot is valid only against the source-doc content hashes below.** If a source changes, this snapshot may silently conflict or go stale. Defense is mechanical, not memory-based:

- **Drift check:** `bash scripts/enso-spec-drift.sh` recomputes each source's sha256 and compares to this table. Non-matching → snapshot is **STALE**; reconcile §1–§9 against the changed source, then update the hash here.
- **Discipline:** whenever you edit any source doc, re-run the drift check and update this table in the same commit. Also confirm the edit doesn't conflict *across* sources (e.g., an invariant restated differently in two places) — sources must stay mutually consistent, since this snapshot assumes they are.

Pinned as of 2026-06-20 (compile time); Mnemosyne prior-art doc + ADR-001 added 2026-07-11 (WP-1):

| Source doc | sha256 (pinned) |
| --- | --- |
| `2026-06-16-memory-improvement-design.md` | `e26dd50d6de8758cafc73f4e81f0c6bdb048e8a43d62cc86ca06326bd159ac65` |
| `2026-06-17-memory-system-technical-spec.md` | `39189e4c280b760060d40d28cd2ecc2d48f663ee795331c562954f30f99d4c5c` |
| `2026-06-17-phase0-benchmark.md` | `ec2ceeb2e4766e9f88fffbc07448d4eaad896db7038dcb798531cc611b74ca47` |
| `2026-06-20-enso-hexagonal-portability-architecture.md` | `7e8112ec32656f5ad366f5358dd2f3754c487c3e251e623a793d479c2dde100d` |
| `2026-07-07-mnemosyne-prior-art-comparison.md` | `a8b11ea79c891357eaf642cdca32c7a74d1a8652a2716753ff871096553298ac` |
| `adr/ADR-001-scope-ratification.md` | `a0fe0fdce4d1a5057962275ecc0cdafd23729ace09293c2d4fb78bf31acb711e` |

> ⚠️ Note: `2026-06-17-phase0-benchmark.md` has a **running log** (§Log) that updates as Phase 0 data accrues. Its hash *will* drift by design. That is expected and benign — the benchmark log is data, not contract. Re-pin its hash on reconcile without treating log-only changes as a spec conflict. Only treat changes to the *other contract sources* (or to the benchmark's contract sections) as snapshot-invalidating.
