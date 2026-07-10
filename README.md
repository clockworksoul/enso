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
| `internal/bench/` | Offline replay benchmark — the success metric (recall model vs. naive baseline) + the detector replay + the end-to-end capture proof | ✅ |
| `cmd/enso/` (planned) | CLI / runnable driving adapter | Stage 4 — not started |

> **`confirm` note (2026-06-26):** this package once held a full synchronous human-in-the-loop approval surface (a TTY interview, `Operator`/`Decision` seam, `Confirmer`/`HandleText` loop driver, auto-accept `Policy`). It was removed (~1,259 lines) under the **complexity-kills** principle: corrections are already human-gated by being *stated in conversation*, so a separate "approve each write at a terminal" ceremony solved a problem we don't have. The real workflow is capture-then-notify with reversible writes (INV-2). The deleted spine lives in git history and can be restored if a future validation step shows a confirmation gate is actually wanted.

The Markdown format is a **public contract** (AMEND-1), pinned by a golden-file test (`internal/mdstore/testdata/golden_entry.md`; regenerate intentionally with `UPDATE_GOLDEN=1 go test ./internal/mdstore/ -run TestGolden`).

## Status

Early construction, **Phase-1 loop proven end-to-end on one real case.** Phase 0 (the `active-memory` trigger) is live and collecting benchmark data in the host environment. The domain core (Stage 1) and the Markdown store (Stage 2) exist and are fully tested.

The headline (2026-06-26): the complete capture-and-recall arc runs end to end on a real miss. Starting from the *pre-correction* world (an open stale entry, no `SUPERSEDES` edge, a raw correction utterance), the loop **detects** the correction → **resolves** the open stale entry as the target → **commits** the supersession → and on a later recall, the Ensō model **ranks the corrected answer first while the naive recency baseline is still fooled by the stale entry** (`internal/bench/endtoend_test.go`). The Phase-1 value claim — *recover the current answer where the flat model loses* — is now a passing test, not an assumption. It is proven on **one** case; broadening the corpus is the next work (see below).

No runnable surface yet (`cmd/` is unbuilt by design): the loop is validated in-memory first, and a runnable adapter is justified only once the loop it wraps is proven. Implementation continues stage-by-stage, gated on explicit go-ahead.

## Next steps

In priority order. The discipline (per **complexity kills, simplicity scales**): build the next thing only when current, documented evidence demands it; validate before building; stop at the seam.

1. **Broaden the end-to-end proof past one case.** The loop is proven on the `adam-headcount` restate. Add pre-correction fixtures for the other real misses (notably the `ed-sandoval` *reframe*) and run them through the same end-to-end arc. One passing case is a proof of concept; a handful across miss-classes is a proof of robustness.
2. **Content-aware target resolution — *only if* the broadened corpus demands it.** `StoreResolver` ranks candidates by decay strength, *blind to the correction's content*. With one plausible stale entry that's fine; with several, it can resolve the wrong target. The end-to-end test asserts target-correctness explicitly, so a multi-candidate fixture that mis-resolves will **fail loudly** — that failure is the signal to build content-aware resolution, and not before.
3. **Then, and only then: wire a runnable surface (`cmd/`).** A one-shot harness that drives real conversation lines / the real miss log through `Propose` against a file-backed `FSStore`. Justified once the loop it wraps is proven across the corpus.
4. **Parked until a consumer needs it:** reframe **content extraction** (the reframe detector signals fire but extract no `Content`, having no `captureRe`). No consumer needs the extracted statement yet (a human supplies it), so building extraction now would be speculative.
5. **Deferred:** Phase 2 (graph `Store` behind KùzuDB) and Phase 3 (live decay texture) remain architectural / design-locked sketches. The decay fields are written-but-inert by deliberate design (no backfill later); do not wire them until DM-days of data show relevance-drift misses that supersession alone can't fix.

## License

MIT — see [`LICENSE`](LICENSE).
