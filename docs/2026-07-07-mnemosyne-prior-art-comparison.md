# Mnemosyne — Prior-Art Comparison & the Scope-Drift Flag

*Drafted 2026-07-07. Author: Dross (with Matt). Status: prior-art record +
scope-correction note for the Jul-13 go-live review.*

## Why this doc exists

Ensō's origin lineage was **not captured anywhere** — on Jul 7 Matt had to dig
through chat logs to recover the name of the prior-art system that sparked it.
That is exactly the **silent non-capture** failure (design-doc failure mode #2)
Ensō is being built to prevent, so the omission is on-brand and a little
embarrassing. This doc closes the gap: it records the prior art, compares it to
Ensō, and — more importantly — flags a **scope drift** Matt caught in the same
conversation.

## The prior art: `github.com/rand/mnemosyne`

Mnemosyne = "agentic memory and orchestration system for Claude Code," Rust +
LibSQL (SQLite/sqlite-vec) + PyO3. Matt and Dross evaluated it pre-Ensō and
rejected it as **"far too complex; we could do a better job."**

**Crucial framing:** Mnemosyne is a *memory system PLUS a full multi-agent
orchestration engine* — Ractor actors (Orchestrator/Optimizer/Reviewer/Executor),
work queues, deadlock resolution, P2P peer networking (Iroh), a CRDT collaborative
editor (ICS), dashboards, gRPC servers. The memory core is maybe ~30% of it. The
"too complex" reaction was to the **sprawl**, not the memory design — which is
actually good.

## ⚠️ The scope correction (Matt, Jul 7) — read this before the table

The feature comparison below was first written comparing Mnemosyne against Ensō
**as it currently exists in code** — i.e. a recall-intelligence / evaluation layer
(STALE / NEIGHBOR / FABRICATION classifiers) that ranks candidates handed to it by
OpenClaw's *existing* `memory_search` embedding retrieval.

**That is NOT the original Ensō vision, and comparing on that basis undersells
both what Ensō was meant to be and how directly it competes with Mnemosyne.**

Original Ensō (per the Jun-16 design doc) = a **complete memory replacement**:
- **§3.1:** "The graph IS the memory... not an index over the memory — the graph
  is the *model* of the memory." Ensō owns the substrate.
- **Phase 0** = turn on existing file-based `memory_search` **as the benchmark to
  beat** — explicitly the *control group*, not the foundation.
- **Phase 1** = Ensō's own KùzuDB graph store + full typed schema.
- **Phase 2** = the graph must **beat** the Phase-0 `memory_search` benchmark or
  it doesn't ship.
- **§5.4:** vectors kept *inside* Ensō ("reuse the existing index **or** stand up
  sqlite-vec alongside") — semantic matching was meant to become **one internal,
  subordinate component of Ensō**, under the graph, not an external dependency
  Ensō rides on top of.

**So the drift, stated plainly:** we built the layer that *judges* retrieval
(STALE/NEIGHBOR/FABRICATION, validated against real misses) **before** we built
the retrieval substrate it was meant to *replace* (the Phase-1/2 KùzuDB graph that
owns capture + store + recall). The evaluation layer is real and done; the
**memory replacement is still ahead.** Current-Ensō *looks* like an eval layer;
that's a waypoint, not the destination.

**Concrete evidence the drift matters — tonight's 429 outage:** the Gemini
embedding quota exhausted and took semantic recall fully offline. Current-Ensō
couldn't have helped — it sits *downstream* of the exact retrieval layer that
broke. **Full-scope Ensō would have owned that retrieval** (its own KùzuDB graph +
its own vector supplement), so a Gemini quota failure wouldn't be in the critical
path at all. Seam #8 in the state-of-the-loop map ("does Ensō reduce
embedding-provider dependence?") was written from *inside* the drifted assumption;
under the original vision the answer is "yes, by construction — Ensō replaces that
layer."

## Feature comparison — Mnemosyne vs. FULL-SCOPE Ensō

*(Comparing complete-system to complete-system, per the correction above.)*

| Concept | Mnemosyne | Ensō (full-scope vision) | Verdict |
|---|---|---|---|
| Typed nodes | Insight, Architecture, Decision, Task, Reference | Fact, Decision, Insight, Person, Project, Task | Near-identical instinct. Ensō adds `Person` (tracks Matt's world, not just code); Mnemosyne adds `Reference` (code-centric). |
| Graph | Bidirectional links; **10%** of hybrid search score | **Graph IS the memory**; recall = traversal; primary spine | Biggest philosophical split. Mnemosyne: graph is a garnish on vector search. Ensō: graph primary, vectors supplement ("vector finds the door, graph walks the house"). |
| Supersession / staleness | "Supersede: track replacements w/ audit trail" — one evolution op | `SUPERSEDES` edge = core STALE mechanism, elevated to a first-class **validated recall class** (detector + contradiction check + benchmark) | Convergent (independent teams both chose explicit supersession — validates the bet). Ensō goes deeper on the axis. |
| Decay | `evolve links` **batch job** | **Lazy-compute-on-read**, no background job ("I don't exist between turns") | Ensō genuinely cleaner — decay as a pure function of stored fields + clock, no process to tick. |
| Importance / ranking | 1-10 score + **online-learned weights** (session→project→global, implicit signals) | `S_floor`/`S_last`/`confidence`, empirically hand-tuned vs. a labeled real-miss corpus | Mnemosyne more automated (adaptive black box); Ensō more validated (falsifiable, refuses to tune on n=1). |
| Consolidation | LLM-assisted merge (lossy, opaque) | Lossless/auditable, diff of what was dropped | Ensō prioritizes auditability; Mnemosyne automation. |
| Storage | LibSQL (SQLite+sqlite-vec), vectors co-located | KùzuDB (embedded Cypher graph), vectors separate; Neo4j as config-swap scale-up | Both embedded/local/zero-ops. Ensō picks a real graph engine (Omega Cypher muscle-memory transfer). |
| Serialization / audit | Binary DB, no plaintext canonical form | **Markdown = canonical lossless serialization + human-readable audit log** | Ensō's signature move; **absent in Mnemosyne**. Duty-of-care: Matt must be able to `cat` what's held. |
| Hybrid search | 70% semantic / 20% FTS / 10% graph | Vector→entry-nodes → traverse; FTS + lexical in recall layer | Mix inverted: Mnemosyne vector-dominant, Ensō graph-dominant. |
| Proactive surfacing | ❌ retrieval-on-demand only | ✅ **Goal #1**: dropped-thread nudges, commitment reminders, contradiction flags | Ensō's biggest functional addition — the executive-function support that replaced Granola. No Mnemosyne analog. |
| FABRICATION guard | ❌ none | ✅ confabulation-toward-plausible-gist detection | Novel to Ensō. |
| NEIGHBOR / vocab-drift | partial (graph + semantic sim) | ✅ explicit class (specificity-vs-recency) | Both attack vocab bias via graph; Ensō formalizes it. |
| **Orchestration/networking/UI** | ✅ huge (actors, P2P, CRDT editor, dashboards, gRPC) | ❌ deliberately none | The sprawl Ensō rejected. "Kept the seed, dropped the sprawl." |

## What to steal from Mnemosyne (roadmap, not now)

1. **Online-learned ranking weights** (implicit access/edit/commit signals,
   session→project→global). Ensō hand-tunes — more rigorous but non-adaptive.
   Borrow the feedback loop **only after** the hand-tuned baseline validates on
   the corpus (skipping validation to chase adaptivity would violate the standing
   "validate before build" law).
2. **A `Reference` node type** for code-context memories, if/when Ensō is used for
   engineering knowledge and not just Matt's world.
3. **The discipline of shipping + benchmarking** (Mnemosyne: 2.25ms ops, 124+
   tests). Ensō's value only materializes once it's live over real memory — which
   is the Jul-13 go-live argument.

## The one-liner

Mnemosyne is a maximalist *memory + orchestration platform* where memory is one
well-built subsystem. **Full-scope Ensō** was meant to be a minimalist but
*complete* memory replacement — its own graph substrate, its own vectors, its own
recall — wrapped in a validated recall-intelligence layer Mnemosyne lacks, plus a
plaintext audit log and proactive surfacing. **Current-Ensō is only the recall-
intelligence layer so far; the substrate-replacement half (Phase 1/2) is the
unfinished original vision.** Kept the seed, dropped the sprawl — but haven't yet
grown the whole plant.

## Action items for Jul-13 go-live review

1. **Decide the scope question explicitly:** is Ensō (a) a permanent evaluation
   layer over OpenClaw's `memory_search`, or (b) the original complete-replacement
   (own KùzuDB substrate + own vectors)? The build has drifted toward (a); the
   design says (b). Pick deliberately — don't let the drift decide by default.
2. If (b): the 429-outage class of failure becomes a **motivating requirement** —
   owning retrieval removes the external embedding-provider single-point-of-failure.
3. Revisit **seam #8** — reframe from "does Ensō reduce embedding dependence?" to
   "the original design *eliminates* it; is that still the goal?"
4. Keep this doc as the prior-art record so the lineage is never silently lost
   again.
