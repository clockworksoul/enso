# Ensō — Hexagonal Architecture & Framework Portability

*Companion to the Ensō design set. Read after:*
- *`research/2026-06-16-memory-improvement-design.md` — the why/what (design doc).*
- *`research/2026-06-17-memory-system-technical-spec.md` — the how (tech spec). Defines INV-1/INV-2 and the phase model this doc extends.*
- *`research/2026-06-17-phase0-benchmark.md` — the Phase 0 measurement plan.*

**Date:** 2026-06-20
**Author:** Dross (designed in conversation with Matt Titmus, Sat 2026-06-20 ~12:53–14:20 EDT)
**Status:** DESIGN NOTE — architectural, not implementation-ready. Captures a structural decision (the hexagon and its one amendment) that should constrain Phase 1 schema work and pre-shape the Phase 2 graph. Nothing here is built or executed without Matt's explicit go-ahead.

---

> **What this doc is.** The design doc and tech spec answer *what memory should do* and *how the Markdown/graph mechanics work*. This doc answers a different question that surfaced later: **how does Ensō avoid being a prisoner of OpenClaw?** Matt's framing (2026-06-20): "I want to make this available beyond just OpenClaw, and I want to migrate between frameworks losslessly — actively avoid tight coupling to any framework. The opposite, in fact." This is the portability architecture that honors that, and it turns out to be *hexagonal architecture (ports & adapters)* with exactly one deliberate amendment.

---

## 1. The two goals, reduced to one invariant

Matt named two goals:

1. **Available beyond OpenClaw** — other frameworks (Claude Desktop, Cursor, Cline, future hosts) can use the same Dross memory.
2. **Lossless framework migration** — moving Ensō from one host to another loses nothing.

Both reduce to a single litmus test:

> **PORT-INV — The memory must be fully valuable, and fully reconstructable, with zero framework present.**

If you `git clone` the Ensō directory onto a bare machine — no OpenClaw, no daemon, no agent at all — and a human (or a fresh framework) can read it and reconstruct the complete memory state, then Ensō is decoupled. If reconstruction requires replaying OpenClaw plugin logic or hitting a running service, it is coupled, and migration is lossy.

PORT-INV is not new machinery. It is the portability reading of the **two invariants already locked in the tech spec**:

- **INV-1 (Markdown is canonical and lossless)** ⟹ the substrate is framework-agnostic *by construction*. Markdown does not know what OpenClaw is.
- **INV-2 (append-only; nothing destroyed)** ⟹ the *history* is portable too, not just the current snapshot. A new framework can replay or audit the whole timeline.

The consequence is the strongest possible decoupling: **the on-disk format *is* the export.** There is nothing to migrate *out of*, because nothing framework-specific ever got *in*. "Migration" collapses into "clone the directory."

---

## 2. The coupling risk to guard against

The danger is not where intuition points (it is not "we forgot to build an export tool"). It is **semantic leakage into the substrate**: the moment OpenClaw-specific concepts (session keys, plugin config shapes, tool-call IDs, gateway paths, channel envelopes) end up *inside* the canonical Markdown, the format stops being portable and migration becomes lossy.

So the discipline that protects both goals is a clean three-layer split:

| Layer | What it is | Portability rule |
| --- | --- | --- |
| **Substrate** | Markdown files + documented schema | The truth. Survives everything. **Framework-blind.** |
| **Core** | Small embeddable library (decay / recall / append / consolidation) | The **reference implementation** of behavior. Portable as *source you link*, not a service you call. |
| **Adapter** | OpenClaw plugin, MCP daemon, CLI | **Disposable. Per-framework.** Throwaway by design. |

The Phase-1 schema-design question therefore becomes a one-line test applied to every field:

> **"Would this field still make sense with no framework attached?"**
> If yes → it belongs in the substrate. If it only means something to OpenClaw → it belongs in an adapter.

---

## 3. The option space we walked (and why we rejected the others)

Recorded so the decision is auditable, not just asserted. Constraint set: portable substrate, lossless migration, framework-blind, **and the hot-path recall must not depend on a remote service** (established earlier — active-memory fires pre-reply on *every* turn; a network hop there adds latency + a failure mode to the hottest path).

**Option A — Files-only ("the directory is the API").**
Ensō is *just* a Markdown directory + documented schema; every framework reads/writes files directly via its own adapter. No daemon, no protocol.
- ✅ Maximally portable — `git clone` *is* the migration.
- ✅ Zero runtime dependency; survives every framework dying.
- ❌ No single writer → concurrent-append corruption.
- ❌ Every framework re-implements recall/decay → **semantic drift** (OpenClaw's "recall" and Cursor's "recall" diverge). *The data is portable but the behavior isn't — a subtle way lossless portability dies.*

**Option B — Daemon + MCP.**
A long-running service owns the files, exposes recall/write over MCP; every host is an MCP client.
- ✅ Single writer; consistent semantics everywhere.
- ❌ Hot path now depends on a running service — daemon down = every turn degraded.
- ❌ The daemon itself becomes a thing to port/operate — re-coupling, just to *infrastructure* instead of *framework*.

**Option C — Embeddable core library + files (no mandatory daemon).**
Decay + recall + append discipline live in **one small library** (Go — compiles to a static binary *and* links in-process). Frameworks link it directly or shell out to a CLI built from the same core.
- ✅ Single source of *behavior* without a single source of *uptime* — recall logic is identical everywhere because it is literally the same code.
- ✅ Files stay canonical; library is a *lens*, not a gatekeeper.
- ✅ No hot-path network hop.
- ⚠️ Concurrency handled by file locking in the library, not a daemon.

**Option D — Hybrid: core library is the truth; daemon/MCP is an *optional* face.**
Same core as C, but it *can* run as a daemon exposing MCP when a remote/multi-client scenario demands it.

### Decision

**Option C as the spine, with D as an optional escape hatch — explicitly not B.**

The deciding reasoning: the two goals pull in opposite directions if you are careless. "Portable/lossless" pushes toward files-only (A); "available to many frameworks" naively suggests a service (B). The resolution is recognizing those are **two different artifacts that must never merge** — substrate vs. behavior. The insight that makes C beat both:

> **Share the code, not the runtime.**

A library gives identical behavior across frameworks (solves A's semantic drift) *without* a mandatory service on the hot path (solves B's uptime coupling). You ship behavior as *source you link*, not a server you call. And it preserves PORT-INV: the library is the *recommended* way to interpret the files, never the *required* one. A human with a text editor — or a new framework that refuses to link the Go — can still read the Markdown and reconstruct state. **The library is an optimization, not a dependency.** That is the line that keeps lossless migration true.

---

## 4. The architecture: a memory core in a hexagon

What §3 converges on *is* **hexagonal architecture (ports & adapters)**. The fit is unusually clean; the mapping is near one-to-one.

```
                       Driving adapters (disposable, per-framework)
              ┌───────────────┬───────────────┬──────────────────┐
              │ OpenClaw      │ MCP daemon    │ CLI / scripts    │
              │ plugin        │ (foreign      │                  │
              │ (links core   │  hosts)       │                  │
              │  in-process)  │               │                  │
              └───────┬───────┴───────┬───────┴────────┬─────────┘
                      │  Driving port (inbound):        │
                      │  Recall(query,ctx) · Append(e)  │
                      │  Search · Consolidate           │
                      ▼               ▼                ▼
              ┌─────────────────────────────────────────────────┐
              │                  ENSŌ CORE                        │
              │   (the hexagon's inside / the domain)             │
              │   decay model · recall semantics · append         │
              │   discipline · spacing-aware rehearsal            │
              │   — depends on NOTHING outward —                  │
              └───────────────────────┬─────────────────────────┘
                      Driven port (outbound): Store (· Index later)
              ┌───────────────────────┴─────────────────────────┐
              │  Driven adapters                                  │
              │  ┌─────────────────────┐  ┌────────────────────┐ │
              │  │ Markdown FS store    │  │ Graph store (P2,    │ │
              │  │ (canonical, INV-1)   │  │  KùzuDB) — later    │ │
              │  │ ★ PUBLIC CONTRACT ★  │  │  same Store port    │ │
              │  └─────────────────────┘  └────────────────────┘ │
              └───────────────────────────────────────────────────┘
```

**Core / domain (inside the hexagon).** Decay model, recall semantics, append discipline, spacing-aware rehearsal. Hexagonal's central rule — *the domain depends on nothing outward* — is exactly a restatement of PORT-INV: the core knows nothing about OpenClaw, MCP, or any host.

**Ports (the edges).** Abstract interfaces the core defines *on its own terms*. Two kinds, and the distinction matters:
- **Driving ports (inbound)** — how the world *asks the core to act*: `Recall(query, ctx)`, `Append(entry)`, `Search`, `Consolidate`. OpenClaw, MCP, and the CLI all drive the core through this *same* port.
- **Driven ports (outbound)** — what the core *needs from the world*, expressed as interfaces it owns: a `Store` port (read/write substrate); later an `Index` port.

**Adapters (outside).**
- *Driving adapters:* OpenClaw plugin, MCP daemon, CLI — disposable, per-framework.
- *Driven adapters:* the **Markdown filesystem store** is just *one* implementation of the `Store` port.

### Why the hexagon earns its keep here (not just vocabulary)

**1. It is where the Phase 2 graph hides.** The `Store` driven-port is exactly the seam where a KùzuDB/graph backend slots in *later* without touching the core or any frontend. Markdown store and graph store become **two adapters behind the same port**; the core never knows which it talks to. This turns the tech spec's Phase 1 (Markdown) → Phase 2 (graph) transition from a *migration* into *"add a second driven adapter."* And because Markdown stays canonical (INV-1), the graph can be a **derived read-model the core treats as a cache** — precisely the tech-spec stance that the graph is a rebuildable index, now expressed as a port boundary. The hexagon is what makes "graph later" a non-event.

**2. It resolves the C-vs-B tension structurally.** "Share the code, not the runtime" was the conclusion; the hexagon is the *mechanism*. The core hexagon **is** the shared library. Whether it is driven in-process (OpenClaw links it) or over the wire (MCP daemon) is **purely a driving-adapter choice** — the hexagon does not change. That is why both coexist without forking behavior: same hexagon, different driving adapters.

### Where MCP actually fits

Not as the core. As **one driving adapter among several**, built *on top of* the library, switched on only when a host cannot link the core directly (Claude Desktop, say). MCP becomes the **interop face for foreign hosts** — exactly the "available beyond OpenClaw" goal — without ever sitting on OpenClaw's own critical path (OpenClaw links the library directly). Concretely, three faces, one substrate, one behavior implementation:

- **OpenClaw** → links the core library in-process (fast path).
- **Claude Desktop / Cursor / future host** → talks MCP to a thin daemon that *also* just wraps the core library.
- **Human / cold migration** → reads raw Markdown, no code at all.

The daemon exists only when needed, and even then it wraps the same library, so behavior never forks. MCP becomes the eventual **proof of decoupling**: the day a second framework reads Ensō cleanly is the day we *know* it worked. But the decoupling itself happens in **schema discipline (§2), not the protocol.**

---

## 5. The one amendment — the soul of the thing

Classic hexagonal treats the driven `Store` as a **private implementation detail**: nobody reads the persistence directly; everyone goes through the core. **Ensō deliberately violates this, and should.** PORT-INV (and INV-1) require the Markdown to be interpretable *with no code running at all* — a human is explicitly allowed to "read the database directly," bypassing the hexagon entirely.

So Ensō is hexagonal **with one constraint stapled on:**

> **AMEND-1 — The driven `Store` adapter's on-disk format is itself a *public, documented contract* — not an opaque implementation detail.**

This is the only place the pattern is consciously bent, and it bends *in service of* portability, not against it. It is worth naming loudly because a purist would build an opaque store, and that opaqueness is precisely what would kill lossless migration. AMEND-1 is the whole soul of the design: it is the clause that keeps the substrate sacred even as the core and adapters evolve.

---

## 6. What this constrains going forward (the actionable residue)

This is a *design note*, not a build order. There is no plan to implement a daemon or MCP face now. But the architecture imposes cheap, defensive discipline that should start **immediately and for free**, mostly on the Phase 1 schema work already in the cone of uncertainty:

1. **Phase 1 schema must pass the "no framework attached" test (§2) for every field.** Framework-meaningful fields go in an adapter, never the substrate. This is the single highest-leverage habit; it costs nothing if done now and is expensive to retrofit (tech spec §0: *the format is the durable thing*).
2. **Specify the substrate format as a public contract (AMEND-1), readable from a README alone** — no spec required beyond plain text. If recall ever *requires* the library to even *understand* the files (binary blobs, opaque IDs, framework-coupled fields), portability quietly dies and we are back in Option B whether we meant to be or not.
3. **Draw the `Store` driven-port boundary deliberately during Phase 1/2** so the Phase 2 graph is "a second adapter," not a rewrite.
4. **Keep the core library framework-blind from its first line.** No imports of OpenClaw types into the domain, ever. The OpenClaw plugin is an adapter that *depends on* the core; the core never depends on it.
5. **Defer the daemon/MCP face until a real second host exists.** Build it as Option D's escape hatch, on demand — not speculatively.

### Phase placement

This slots as a **cross-cutting architectural constraint on Phases 1–2**, not a new phase. It does not change *what* gets built next (Phase 1 = structured Markdown, per the tech spec); it changes *how the boundaries are drawn* so the substrate stays portable and the graph stays optional. Earlier shorthand for this was "Phase 1.5 / portability," but it is more accurate to treat it as a discipline layered over the existing phases than as a sequential step.

---

## 7. One-line statement of the architecture

> **A framework-blind Markdown substrate, a small embeddable core library as the reference behavior, and adapters (including an optional MCP daemon) as the only framework-aware parts — with the hard rule (AMEND-1) that the files must remain fully interpretable with no code running at all.**

That honors both of Matt's goals simultaneously instead of trading one for the other, leaves MCP available exactly where it is strong (foreign-host interop) without letting it become a dependency anywhere it is weak, and pre-positions the Phase 2 graph behind a port so it arrives as an addition, not a migration.
