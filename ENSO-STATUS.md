# Ensō — Current Status

*Single source of truth for where we are and what done looks like. Updated 2026-07-08.*
*Authoritative spec: `docs/2026-06-20-enso-unified-spec.md`. ADRs: `docs/`.*

---

## Architecture at a glance

| Phase | Piece | Fixes | Status |
|---|---|---|---|
| P0 | **Trigger** — active-memory plugin | #1 silent non-retrieval | ✅ Live since Jun 17 |
| P1 | **Corpus** — structured Markdown format | #2 unauditable consolidation, partial #4 staleness | 🔨 Next |
| P2 | **Index** — KùzuDB graph plugin in `memory` slot | #3 vocabulary drift, #4 staleness at retrieval | ⬜ Blocked on P1 |
| P3 | **Texture** — leaky-integrator decay | ranking quality, temporal texture | ⬜ Blocked on P2 |

---

## Phase 0 — LIVE ✅

Turned on the stock `active-memory` plugin (Jun 17). Benchmarking flat-file recall.
This is the **control group**: every later phase must beat it or it doesn't ship.

Benchmark log: `docs/2026-06-17-phase0-benchmark.md`

---

## Phase 1 — Structured Markdown Corpus 🔨

**What it is:** Start writing memories in a structured, parseable, append-only format with supersession conventions and reserved temporal fields. The format is the durable artifact — retrofit is expensive, so we spec it to the millimetre now.

**Target:** The corpus active-memory already searches becomes structured, auditable, and supersession-aware — without a graph yet.

### Definition of done

- [ ] Matt has ratified the entry grammar (§3 of tech spec) and open decisions below
- [ ] First batch of real structured entries written (starting with today's key decisions)
- [ ] Parser round-trips a representative sample losslessly (`parse(serialize(e)) == e`)
- [ ] Reserved P3 fields (`last_ref_time`, `S_last`, `S_floor`, `lambda`, `S_cap`) present in the format with sane defaults — even though inert until P3
- [ ] `go test ./...` green including mdstore round-trip tests
- [ ] 1 week of stable grammar (no churn) + enough entries that a graph index would be worth building
- [ ] P1 exit decision: does structured Markdown + active-memory already beat the P0 baseline? If yes, **pause** — let P2 earn its keep rather than build it reflexively

### Open decisions (need Matt's call before building)

| # | Decision | Lean | Notes |
|---|---|---|---|
| **S-1** | Entry location: inline in `memory/YYYY-MM-DD.md` vs dedicated `memory/structured/` store | **Inline (a)** | Capture fidelity — fewer places = more likely to actually write entries. Final call at P1 start. |
| **S-schema** | Ratify the entry grammar as written in tech spec §3 | Pending | Matt must sign off. The format is permanent; changing it later requires a migration. |
| **S-reserved** | Init values for `S_last`, `S_floor`, `lambda`, `S_cap` per node type | Placeholders ok | Values get tuned in P3; we just need the keys present with any defensible default. |

### Key invariants (non-negotiable, from unified spec §1)

- **INV-1:** Markdown is canonical and lossless. The graph (P2) is a derived cache. Memory survives without it.
- **INV-2:** Append-only. Supersession flags entries; it never deletes them. "What did Dross know about X, and when?" must always be answerable.
- **PORT-INV:** `git clone` + no framework = human can reconstruct complete state. Framework-meaningful fields go in adapters, never the substrate.

---

## Phase 2 — KùzuDB Graph Plugin ⬜

**Blocked on:** P1 grammar ratified + stable corpus exists to index.

**Target:** Replace the `memory` slot with a KùzuDB-backed plugin that does graph traversal instead of flat-file search.

### Definition of done

- [ ] KùzuDB plugin registered in `plugins.slots.memory`
- [ ] Plugin parses P1 Markdown corpus → graph nodes/edges on startup
- [ ] `memory_recall` returns results with supersession filtering (stale entries suppressed)
- [ ] **Beats the P0 baseline** on real recall cases — specifically: connected-fact retrieval (#3) and staleness suppression (#4) — without inflating noise rate
- [ ] Graceful degradation: plugin quarantine falls back to flat-file search cleanly

**Gate:** Does not ship unless it beats P0. No exceptions.

---

## Phase 3 — Leaky-Integrator Decay ⬜

**Blocked on:** P2 benchmark data exists.

**Target:** Turn the graph into a temporally textured one — recency and importance as first-class ranking signals.

### Definition of done

- [ ] `StrengthAt` / `BumpOnRecall` wired into P2 recall ranking (math already written in `internal/core/recall.go`)
- [ ] Spacing-aware bump (`α_eff`) implemented first (highest engineering value)
- [ ] Recall bump fires only on RECALL-DEF: surfaced AND materially used in a reply — not just returned by search
- [ ] Runaway Hebbian feedback tested: `S_cap` ceiling + novelty bonus prevent rich-get-richer monopoly
- [ ] **Beats the P2 baseline** on ranking quality metrics

---

## Codebase state (as of 2026-07-10) — WP-0 COMPLETE

**WP-0 closed:** `go build ./...` was broken because `internal/bench` referenced deleted symbols (`Entry.Correct`, `core.Correction`, `CorrectRestate`, `CorrectReframe`, `DetectCorrection`, `confirm.Propose`). Fixed by:
- Rewrote `bench/cases.go` and `bench/held_out_cases.go` to build supersession triples directly via `core.NewEntry` + `Entry.Supersede`. All case semantics and timestamps preserved.
- Rewrote `bench/held_out_test.go`: deleted `TestHeldOut_DetectorGeneralizes` (dead — detection layer gone); kept `TestHeldOut_RecallGeneralizes` (generalization gate, still passes 2/2).
- Pruned dead symbol references from comments in `bench/corpus.go`, `bench/mutation_test.go`, `bench/cases.go`.
- `make check` + `make test-race` green. Ensō 2/2 STALE seed + 2/2 held-out. Capture is load-bearing (0.50 without edges). NEIGHBOR documented 0/1 as expected.

**State of codebase:**
- **Kept:** entry model + store port + mdstore adapter (P1 infrastructure), decay math + ranking (P3 math), benchmark harness with real cases
- **Deleted (Jul 8):** detection/correction layer (`core/correction.go`, `core/detect.go`, `core/contradict.go`, `internal/confirm/`), fabrication probes, synthetic expectations, harvest harness
- **Open gap:** Verify mdstore format includes reserved P3 fields (`last_ref_time`, `S_last`, `S_floor`, `lambda`, `S_cap`). If not, add them — this is WP-1 work.

**Current WP: WP-1** — format reconciliation + documentation hygiene.

**`provenance` call signed (Matt, 2026-07-10):** remove it. No production consumer exists; synthetic/real separation is already enforced at the bench level (separate RealCases vs. SyntheticExpectations buckets). YAGNI. Remove from golden file, marshal tests, and any doc references.

---

## Phase gate rule

*Each phase must beat the prior benchmark or it doesn't ship.*
*Build the house before the roof. Don't gold-plate the roof we haven't reached.*
