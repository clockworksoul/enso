# CLAUDE.md — Ensō

Ensō is a **portable, framework-blind memory system** for AI agents: a canonical
structured-Markdown substrate, a Go core library as the reference behavior, and
disposable per-framework adapters. You are working on the memory prosthesis a
real person depends on. Treat correctness and auditability as duty-of-care, not
polish.

## Read these before writing any code, in this order

1. `docs/enso-development-spec.md` — **the execution contract.** Work packages,
   definitions of done, budgets, and the rabbit-hole guards (RH-1…RH-10). If
   this file and anything else disagree, this file wins (precedence chain is in
   its header).
2. `ENSO-STATUS.md` — the live state. It tells you **which work package is
   open**. You work on that one. Only that one.
3. `docs/ADR-001-scope-ratification.md` — the scope decision (full memory
   replacement, not an eval layer) and why.
4. Depth on demand: `docs/2026-06-20-enso-unified-spec.md` (build contract),
   `docs/2026-06-17-memory-system-technical-spec.md` (Markdown grammar §3.1/§6),
   the design/hexagon/neuro docs for rationale.

Do not start from your general knowledge of "how memory systems work." Start
from the spec. This project has already deleted half its codebase once for
drifting from it.

## Invariants — violating any of these fails review

- **INV-1:** Markdown is canonical and lossless. Graph/vectors are derived,
  rebuildable caches. Never store anything only in an index.
- **INV-2:** Append-only. Supersession flags; nothing is ever deleted or
  rewritten in place. Decay changes ranking, never existence.
- **PORT-INV:** the corpus must be fully reconstructable with zero framework
  present. No OpenClaw (or any host) concept ever enters the substrate or
  `internal/core/`.
- **AMEND-1:** the on-disk format is a public, documented contract. A field
  exists in the tech spec, the golden file, marshal/parse, and tests — or in
  none of them.
- **RECALL-DEF:** "recalled" = surfaced AND materially used in a reply. Search
  hits are non-events for strength purposes.

## Hard rules (compressed from the spec's RH guards — the spec's versions govern)

- **One work package at a time, in order.** No "while I'm in here" fixes —
  note them in `ENSO-STATUS.md` and stay on task.
- **n ≥ 1:** no feature, detector, heuristic, or tuning without a real,
  documented case it addresses. Synthetic cases justify tests, never features.
- **Benchmark gates are hard gates.** WP-4 merges only if it beats its
  baselines, measured and recorded. Failing the gate honestly is acceptable;
  arguing past it is not.
- **No speculative surfaces:** no daemon, MCP face, dashboard, or CLI beyond
  what the open WP's DoD names. **No background jobs ever** — all decay is
  lazy-on-read.
- **Budget tripwires:** if you exceed 1.5× the WP's LoC budget or need a
  dependency not on the allowlist (`internal/core` = stdlib only; graphstore =
  pinned KùzuDB binding; vectors per ADR-002), **stop and escalate** in
  `ENSO-STATUS.md`. Overrun is fine; concealment is the failure.
- **Format changes are a ceremony**, all in one commit: tech-spec update +
  `UPDATE_GOLDEN=1 go test ./internal/mdstore/ -run TestGolden` + round-trip
  tests green + hash re-pin via `scripts/enso-spec-drift.sh`.
- **Tunables stay untuned:** numeric knobs keep their `// TUNABLE` defaults
  until Phase-3 calibration against a labeled corpus. Never tweak a constant to
  make a test pass.
- **When the spec is silent, stop and ask Matt.** Record question and answer in
  `ENSO-STATUS.md`. Do not invent.

## Commands

```bash
make check      # fmt-check + vet + build + test + spec-drift — green at EVERY commit
make test       # go test ./...
make test-race  # green at every WP close
make drift      # unified-spec snapshot vs pinned source hashes
```

## Conventions

- Go, module `github.com/clockworksoul/enso`. Errors are loud: a malformed
  corpus entry is surfaced with file+line, never silently skipped (silent drop
  *is* failure mode #2).
- `internal/core/` imports nothing outward — no adapters, no storage engines,
  no hosts. Adapters depend on core; never the reverse.
- Comments explaining *why* a test case or constant exists are load-bearing.
  Preserve them when refactoring mechanism.
- At WP close: update `ENSO-STATUS.md` checkboxes + a one-paragraph verdict,
  re-pin drift hashes if any `docs/` source changed, and reference the WP in the
  commit message.

## The boredom clause

The most valuable work here is usually the least interesting checkbox in the
open WP. If you feel pulled toward recall-ranking experiments, embedding
research, clever detectors, or visualizations — that pull is the signal to
re-read the open WP's non-goals and return to its next unchecked box. The fun
parts are behind gates because the gates are what make them trustworthy.
