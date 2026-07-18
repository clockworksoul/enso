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
| `internal/core/` | Domain (innermost) — Entry/Edge/ID types, validation, supersession (`Entry.Supersede`), recall math (`StrengthAt`/`Rank`/`RankBySpecificity`), the RECALL-DEF event primitive (`MarkRecalled`, Phase 3), `Store` port | ✅ |
| `internal/mdstore/` | Markdown driven-adapter — serializer/parser (lossless round-trip), file-backed `FSStore`, inline daily-file entries, supersession ceremony, advisory locking | ✅ |
| `internal/graphstore/` | KùzuDB driven-adapter (WP-3/4) — same `Store` port, deterministic rebuild from Markdown, log-first write path, recall = lexical+vector seeds → 1–2-hop traversal → supersession filter → core ranking, degrade-to-lexical on embedding outage (ADR-002) | ✅ |
| `internal/memstore/` | In-memory `Store` — test/speed double for the core and bench | ✅ |
| `internal/storetest/` | Shared `Store`-contract conformance suite all three adapters must pass | ✅ |
| `internal/bench/` | Offline replay benchmark + the hard gates: 79-case real-miss corpus, WP-3 graph gate, WP-4 vector gate, WP-5 activation proofs | ✅ |
| `cmd/enso-append`, `cmd/enso-lint` | Minimal runnable surfaces: one-shot structured append; write-time corpus validator (same parser as `Load`, cannot drift) | ✅ |
| `cmd/corpus-builder`, `cmd/embed-corpus`, `cmd/enso-load-check` | Benchmark tooling: git-history corpus builder, Gemini embedding precompute, corpus load smoke-check | ✅ |

> **Detection/correction layer removed (2026-07-08, commit `cd8e1a2`; ratified by ADR-001).** An earlier build grew a recall-*evaluation* layer downstream of the host's search — a correction detector (`core/detect.go`, `core/contradict.go`), a capture chokepoint (`core/correction.go`), a resolver/approval surface (`internal/confirm/`), and fabrication/harvest probes — ~half the codebase. Per ADR-001 (scope b), Ensō is a **complete memory replacement that owns retrieval**, not a permanent eval layer riding on top of an external index, so that layer was deleted and the original-vision skeleton kept (core types, `Store` port, decay math, `mdstore`). The deleted spine lives in git history and returns only by ADR-001 corollary b′ — **real misses first, restoration second, and never before the Phase-2 benchmark gate (WP-4) closes** (see `docs/enso-development-spec.md` RH-9).

The Markdown format is a **public contract** (AMEND-1), pinned by a golden-file test (`internal/mdstore/testdata/golden_entry.md`; regenerate intentionally with `UPDATE_GOLDEN=1 go test ./internal/mdstore/ -run TestGolden`).

## Status

**All work packages (WP-0 … WP-5) are closed as of 2026-07-18** — see `ENSO-STATUS.md` for the per-WP verdicts and gate numbers. Scope is fixed by **ADR-001**: Ensō is a *complete memory replacement that owns retrieval*, built in gated work packages (`docs/enso-development-spec.md`); the vector engine decision is **ADR-002**.

The value claims are measured, not asserted (79-case real-miss corpus, `internal/bench/`):

- **Staleness suppression:** graph recall scores **P@1 = 1.00 with zero stale surfacings** where naive recency scores 0.63 and flat lexical search 0.57 — the SUPERSEDES edge is load-bearing on the 34 same-subject cases specificity provably cannot break (+0.43).
- **Fail-safe vectors (ADR-002):** the vector doorfinder recovers 8 real no-lexical-overlap cases; an embedding-provider outage degrades recall to byte-identical lexical+traversal results — never to zero.
- **INV-1 proven:** kill-the-graph drills (with and without vectors, and with Phase-3 bump records) rebuild identical recall from Markdown alone.
- **Phase-3 texture live:** `core.MarkRecalled` wires the spacing-aware RECALL-DEF bump through the Store port as appended temporal-update records (INV-2); on the recency-vs-relevance case, the bumped pipeline surfaces the durable-and-used memory where every recency proxy fails.

Capture *detection* — recognizing a correction from a raw utterance — **returned on 2026-07-18 (WP-6), exactly as ADR-001 b′ prescribed**: restored from git history in its precision-hardened form, against the four real correction utterances, only after WP-4's gate closed. `core.ProposeSupersession` turns an utterance plus the loaded corpus into an evidence-named supersession proposal (contradiction-first, then lexical); confirmation and the ceremony remain the operator's — there is no auto-commit path.

## Next steps

The substrate is complete; the remaining seams are host-side, each gated on real evidence (RH-2, n ≥ 1):

1. **Host wiring:** an OpenClaw plugin adapter that serves `memory_recall` from `graphstore` (log-first writes via `LogFirst`, quarantine fallback to flat-file search), surfaces `ProposeSupersession` proposals for confirmation, and emits RECALL-DEF events into `core.MarkRecalled`.
2. **Material-recall telemetry:** the host-side half of Phase-3 measurement — the n=1 constructed divergence case is proven; the corpus-scale number needs live events.

## License

MIT — see [`LICENSE`](LICENSE).
