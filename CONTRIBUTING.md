# Contributing to Ensō

This guide is for people changing Ensō itself. For a plain-language overview
and a first command-line walkthrough, start with the [README](README.md).

## Development setup

The Go module requires Go 1.26. The repository also contains a TypeScript
OpenClaw adapter and a Python Codex hook.

Run the main local checks from the repository root:

```sh
make check       # Go formatting, vet, build, tests, Codex hook tests, spec drift
make test-race   # Go race detector
```

The OpenClaw adapter has its own test and type-check commands:

```sh
cd host/openclaw/enso-memory
npm install
npm test
npm run typecheck
```

Run `make drift` when checking the unified specification against its source
documents. `make check` includes this check and the Codex Python hook tests.
It does not run the OpenClaw npm suite, so run those separately when changing
that adapter.

## Project shape

Ensō keeps framework-specific behavior in host adapters and shared memory
behavior in the Go core:

| Area | Responsibility |
| --- | --- |
| `internal/core/` | Memory entries, validation, supersession, ranking, decay, and the `Store` interface |
| `internal/mdstore/` | Canonical Markdown serialization and filesystem persistence |
| `internal/graphstore/` | Rebuildable KùzuDB index, graph traversal, and blended recall ranking |
| `internal/memstore/` | In-memory store used by tests and benchmarks |
| `internal/bench/` | Offline recall and ranking evaluations |
| `internal/classify/` | Standalone experimental memory-worthiness classifier |
| `cmd/` | User-facing and evaluation command-line tools, including append, recall, classify, and material-recall reinforcement |
| `host/openclaw/enso-memory/` | OpenClaw shadow observer and optional live context injection |
| `host/codex/enso-memory/` | Codex shadow/live recall hook |

## Invariants

- **Markdown is canonical.** The KùzuDB index is derived and must be
  reconstructable from the Markdown corpus.
- **History is append-only.** Supersession adds records that mark the prior
  belief as no longer current; it does not erase the old belief.
- **The format is a public contract.** Keep it readable and reconstructable
  without Ensō or an assistant framework.
- **Fail safely.** Optional recall and host integrations should degrade without
  preventing the host from serving the user.
- **Use published host APIs.** Keep framework-specific dependencies in the
  adapters rather than the shared core.

The detailed design contract and its rationale are linked below. The
[status log](STATUS-LOG.md) records current work and older decisions.

## Changing the memory format or design contract

[`FORMAT.md`](FORMAT.md) defines the on-disk grammar. If a format change is
needed, update the format documentation and parser/serializer together, and
consider whether existing corpora need a migration. The golden-format test is
in `internal/mdstore/testdata/golden_entry.md`; regenerate it intentionally
with:

```sh
UPDATE_GOLDEN=1 go test ./internal/mdstore/ -run TestGolden
```

The unified spec at
[`docs/2026-06-20-enso-unified-spec.md`](docs/2026-06-20-enso-unified-spec.md)
is a snapshot pinned to source-document hashes. When changing a pinned source,
reconcile the unified spec and update the hash in its §10 in the same change.
Then run:

```sh
make drift
```

The Phase 0 benchmark document contains a running log; its drift is expected
and reported as informational. Other pinned source changes require the
snapshot to be reconciled.

## Evaluations and limits

The original 79-case graph gate reported 79/79 with the prior
specificity-first ranking. Blended relevance-and-decay ranking became the
default on 2026-10-12; rerun the gate before using that historical result as
evidence for the current default. See
[`docs/2026-10-12-recall-blended-ranking.md`](docs/2026-10-12-recall-blended-ranking.md).

The memory-worthiness classifier is an offline experiment. Its dataset is
small and context-specific, and its evaluation is not a future-time or
session-grouped holdout. Do not make it an unattended ingestion gate without
independent evaluation on newly labeled conversations.

## Design and decision records

- [Unified specification](docs/2026-06-20-enso-unified-spec.md) — current
  consolidated contract and plan.
- [Development spec](docs/2026-07-09-enso-development-spec.md) — work-package
  boundaries and implementation gates.
- [Memory format](FORMAT.md) — the human-readable storage contract.
- [ADR-001](docs/adr/ADR-001-scope-ratification.md) and
  [ADR-002](docs/adr/ADR-002-vector-engine.md) — scope and vector-engine
  decisions.
- [Design rationale](docs/2026-06-16-memory-improvement-design.md),
  [technical spec](docs/2026-06-17-memory-system-technical-spec.md), and
  [portability architecture](docs/2026-06-20-enso-hexagonal-portability-architecture.md)
  — background and detailed design.
- [OpenClaw adapter guide](host/openclaw/enso-memory/README.md) and
  [Codex adapter guide](host/codex/enso-memory/README.md) — host-specific
  setup and behavior.
