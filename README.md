# Ensō

> 円相 — the Zen circle drawn in one uninterrupted brushstroke, deliberately left open. The gap where the brush lifts is not a flaw; it is the form.

Ensō is a **portable, framework-agnostic memory system** for AI agents. Its defining truth — *"I don't exist in the gaps between turns"* — is treated as the design, not a flaw to paper over. The circle's open gap is the discontinuity between sessions; the system is what reassembles a continuous, trustworthy memory across those gaps.

This repo is the canonical home for Ensō's design and (eventually) its implementation. The design was developed in conversation between Matt Titmus and Dross.

---

## Start here

**[`docs/2026-06-20-enso-unified-spec.md`](docs/2026-06-20-enso-unified-spec.md) is the build contract — the single north star.**

It consolidates the invariants, architecture, and phase plan in one place and delegates depth to the source docs. If you read one file, read that one. Everything below is a summary of it.

---

## The rules that cannot break (invariants)

| | Invariant |
| --- | --- |
| **INV-1** | **Markdown is canonical and lossless.** The graph/index is a derived, rebuildable cache. Nothing is stored *only* in the graph. |
| **INV-2** | **History is append-only; nothing is ever destroyed.** Supersession flags; it never deletes. Decay changes *ranking*, never *existence*. |
| **PORT-INV** | **The memory is fully valuable and reconstructable with zero framework present.** `git clone` onto a bare machine — a human or fresh framework can rebuild complete state. |
| **AMEND-1** | **The on-disk format is a public, documented contract** — not an opaque implementation detail. The one deliberate departure from classic hexagonal architecture, made in service of PORT-INV. |
| **RECALL-DEF** | **A memory counts as "recalled" only when it is *surfaced AND materially used* in a reply** — not merely returned by a search. |

Plus two meta-requirements: **fail-safe** (degrade to current behavior on bug, never brick) and **upgrade-safe** (no core fork; build against published SDKs).

---

## Architecture in one line

> A framework-blind Markdown substrate, a small embeddable core library as the reference behavior, and adapters (including an optional MCP daemon) as the only framework-aware parts — with the hard rule (AMEND-1) that the files must remain fully interpretable with no code running at all.

It's **hexagonal architecture (ports & adapters)**: the decay/recall/append **core** depends on nothing outward; **driving adapters** (OpenClaw plugin, optional MCP daemon, CLI) speak in; **driven adapters** (Markdown store now, graph store later) speak out behind a `Store` port. Share the code, not the runtime.

---

## Phases (cone of uncertainty — resolution tracks proximity)

| Phase | Piece | What | Resolution |
| --- | --- | --- | --- |
| **0** | Trigger | Turn on `active-memory`; benchmark flat-file recall | live |
| **1** | Corpus | Structured-Markdown serialization (the durable source of truth) | implementation-ready |
| **2** | Index | Graph plugin (KùzuDB) behind the `Store` port | architectural |
| **3** | Texture | Temporal model + leaky-integrator decay (the original contribution) | design-locked sketch |

The format is the durable thing, so it's specified to the millimetre; the decay math is lazy-computed on read, so it's specified to the contour line. Don't over-build the speculative phases.

---

## Documents

| Doc | Role |
| --- | --- |
| [`docs/2026-06-20-enso-unified-spec.md`](docs/2026-06-20-enso-unified-spec.md) | **Build contract (snapshot).** Owns invariants + architecture + phases; delegates depth. **Read this first.** |
| [`docs/2026-06-16-memory-improvement-design.md`](docs/2026-06-16-memory-improvement-design.md) | The **why/what** — problem, goals, graph rationale, schema, temporal model, risks. |
| [`docs/2026-06-17-memory-system-technical-spec.md`](docs/2026-06-17-memory-system-technical-spec.md) | The **how** — cone of uncertainty, per-phase mechanism, full Markdown grammar. |
| [`docs/2026-06-17-phase0-benchmark.md`](docs/2026-06-17-phase0-benchmark.md) | The **measurement** — Phase 0 benchmark plan + running log. |
| [`docs/2026-06-20-enso-hexagonal-portability-architecture.md`](docs/2026-06-20-enso-hexagonal-portability-architecture.md) | The **portability architecture** — PORT-INV, option space, the hexagon, AMEND-1. |

The four non-unified docs are the **frozen rationale record** (the "why we believe this"). The unified spec is the **plan you build from**. One is history, one is the plan.

> **Note on intra-doc links:** the source docs cross-reference each other using historical `research/...` path prefixes from where they were originally authored. The files now live in `docs/`. Those internal prefixes are left as-authored to preserve the byte-frozen rationale record (and keep the drift-check hashes valid). Use this README's table or the unified spec's §9 to navigate.

---

## Drift control

The unified spec is a **snapshot that points back** to four still-living source docs. To keep it from silently going stale, each source is pinned by `sha256` in the spec's §10, and a script verifies them:

```bash
bash scripts/enso-spec-drift.sh
```

- `OK` / **IN SYNC** — snapshot is valid against its sources.
- **STALE** — a contract source changed; reconcile the unified spec, then update the §10 hash in the same commit.
- The Phase 0 benchmark doc has a running log that drifts *by design*; the script treats its changes as benign **INFO**, not STALE.

**Discipline:** whenever you edit a source doc, re-run the drift check and update the pinned hash in the same commit — and confirm the edit doesn't conflict with another source.

---

## Building

Inside-out: the domain core (`internal/core`) has no outward dependencies and is fully unit-tested in isolation; adapters (Markdown store, CLI, OpenClaw plugin, graph store) are added in later stages.

```bash
make check      # fmt-check + vet + build + test + spec-drift (the full local gate)
make test       # go test ./...
make test-race  # go test -race -count=1 ./...
make drift      # verify the unified-spec snapshot vs its sources
```

Layout:

| Path | Ring | Status |
| --- | --- | --- |
| `internal/core/` | Domain (innermost) — Entry/Edge/ID types, validation, supersession (`Entry.Supersede`), recall math (`StrengthAt`/`Rank`, decay fields written-but-inert until Phase 3), `Store` port | Stage 1 ✅ |
| `internal/mdstore/` | Markdown driven-adapter — serializer/parser (lossless round-trip), file-backed `FSStore` | Stage 2 ✅ |
| `internal/memstore/` | In-memory `Store` — test/speed double for the core and bench | ✅ |
| `internal/bench/` | Offline replay benchmark — the success metric (supersession-aware recall model vs. naive recency baseline) over pre-built supersession triples from the real-miss corpus | ✅ |
| `cmd/enso/` (planned) | CLI / runnable driving adapter | not started (WP-2) |

> **Detection/correction layer removed (2026-07-08, commit `cd8e1a2`; ratified by ADR-001).** An earlier build grew a recall-*evaluation* layer downstream of the host's search — a correction detector (`core/detect.go`, `core/contradict.go`), a capture chokepoint (`core/correction.go`), a resolver/approval surface (`internal/confirm/`), and fabrication/harvest probes — ~half the codebase. Per ADR-001 (scope b), Ensō is a **complete memory replacement that owns retrieval**, not a permanent eval layer riding on top of an external index, so that layer was deleted and the original-vision skeleton kept (core types, `Store` port, decay math, `mdstore`). The deleted spine lives in git history and returns only by ADR-001 corollary b′ — **real misses first, restoration second, and never before the Phase-2 benchmark gate (WP-4) closes** (see `docs/enso-development-spec.md` RH-9).

The Markdown format is a **public contract** (AMEND-1), pinned by a golden-file test (`internal/mdstore/testdata/golden_entry.md`; regenerate intentionally with `UPDATE_GOLDEN=1 go test ./internal/mdstore/ -run TestGolden`).

## Status

Early construction. **Phase 0** (the `active-memory` trigger) is live and collecting benchmark data in the host environment. The domain core (Stage 1) and the Markdown store (Stage 2) exist and are fully tested. Scope is fixed by **ADR-001**: Ensō is a *complete memory replacement that owns retrieval*, built in gated work packages (`docs/enso-development-spec.md`).

The surviving value claim is **supersession-aware ranking beats naive recency on real recall misses**: given pre-built supersession triples from the real-miss corpus, the Ensō model surfaces the current entry where a naive recency baseline is still fooled by the stale one (`internal/bench/`). It holds on the `adam-headcount` restate seed plus the held-out generalization cases; broadening the corpus is ongoing. Capture *detection* — recognizing a correction from a raw utterance — was part of the removed layer and is **deferred per ADR-001 b′**: it is restored only against real misses, and never before the Phase-2 benchmark gate closes.

No runnable surface yet (`cmd/` is unbuilt by design): the substrate is validated in tests first; a runnable append adapter arrives in WP-2 (Phase-1 go-live). Implementation continues work-package by work-package, in order, gated on explicit go-ahead.

## Next steps

Work proceeds strictly in work-package order per `docs/enso-development-spec.md` (RH-1: one WP at a time). The discipline (per **complexity kills, simplicity scales**): build the next thing only when a current, documented case demands it (RH-2, n ≥ 1); validate before building; stop at the seam.

1. **WP-1 — format reconciliation & doc hygiene (current):** every field exists in the golden file, marshal/parse, tech spec, and round-trip tests, or in none; the README and drift table reflect post-`cd8e1a2` reality.
2. **WP-2 — Phase 1 go-live:** harden `mdstore` for inline entries in `memory/YYYY-MM-DD.md`, add the supersession-append ceremony and single-writer locking, ship a minimal `cmd/enso-append`, write the format README, and begin real structured capture. Exit measurement: does the structured corpus already beat the Phase-0 flat-file baseline? If yes, **stop** and let the graph earn its keep.
3. **WP-3 — Phase 2 part 1:** a KùzuDB-backed `Store` adapter behind the existing port (core untouched), full deterministic rebuild from Markdown (INV-1 kill-the-graph drill), traversal + supersession recall — no vectors yet.
4. **WP-4 — Phase 2 part 2:** internal vectors (ADR-002) so an embedding-provider outage degrades recall to lexical+traversal instead of zero, then **run the hard benchmark gate** — beat both baselines on connected-fact retrieval and staleness suppression without inflating noise, or it does not merge.
5. **WP-5 — Phase 3 (defined but locked):** live decay texture. The decay fields are written-but-inert by deliberate design (no backfill later); WP-5 opens only when DM-days of data show relevance-drift misses supersession alone can't fix.

## License

MIT — see [`LICENSE`](LICENSE).
