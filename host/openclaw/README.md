# Ensō Shadow Memory (`memory-enso`)

Shadow-mode observer for Ensō's WP-7 host adapter. It runs Ensō recall
**alongside** the plugin that owns the memory slot and logs divergence — the
live evidence for whether Ensō should ever take the slot. It is **not** a
`kind: memory` plugin: it never claims the slot, never modifies a prompt, and
never writes to the corpus.

**Home:** this directory (`host/openclaw/` in the Ensō repo — renamed
2026-07-27 from `host/memory-enso/` once a second host adapter, Codex, was
confirmed as planned; `host/<host-name>/` is now the standing layout for
driving adapters). Originally built and iterated in a `clockworksoul/openclaw`
fork (branch `claude/memory-enso-shadow`, path `extensions/memory-enso/`)
because that's where an OpenClaw plugin-SDK-shaped scaffold was easiest to
build against. Lifted and shifted into this repo 2026-07-25 so Ensō doesn't
need to maintain an entire OpenClaw monorepo fork just to host one extension —
the plugin's production code only imports from the **published** `openclaw`
npm package's public `plugin-sdk/*` subpath exports, so it runs standalone
with `openclaw` as a normal dependency. The plugin's own `id` (`memory-enso`)
and config key are unchanged by the directory rename — this is a source-tree
organization change only, invisible at install/runtime. See "Standalone
status" below for the one test-only deviation from the original.

## How it works

- `before_prompt_build` (observation-only): for every turn with a prompt, the
  extension spawns the `enso-recall` binary (one process per call; hard
  timeout) against the canonical Markdown corpus and logs Ensō's ranked
  answer. The handler never returns a value, so the host cannot interpret it
  as a prompt modification.
- `after_tool_call` (observation-only): when the slot owner runs
  `memory_search`/`memory_recall`/`memory_get`, a bounded summary of the
  flat-file result is logged with the same turn key, giving both sides of the
  comparison.
- `enso_recall` tool: on-demand side-by-side check; same bridge, same log.

Any failure in the shadow path — missing binary, timeout, malformed output —
is logged as an `enso_error` record and swallowed. A broken shadow never
breaks a turn.

## Setup

1. Build the bridge binary from the Ensō repo root (one level up):
   `go build -o ~/bin/enso-recall ./cmd/enso-recall` (module
   `github.com/clockworksoul/enso`).
2. Install this extension's own dependencies: `npm install` in this
   directory.
3. Configure the plugin (all fields optional):

```jsonc
{
  "corpusRoot": "~/.openclaw/workspace", // dir containing memory/
  "ensoBinary": "~/bin/enso-recall",
  "topK": 5,
  "timeoutMs": 4000,
  "shadowLogDir": "~/.openclaw/workspace/.enso/shadow",
}
```

With `GEMINI_API_KEY` in the binary's environment, recall runs the vector
doorfinder (ADR-002); without it — or on any provider failure — recall
degrades to lexical+traversal and the record says so (`mode`/`degraded`).

## Divergence log format (JSONL, one file per UTC day)

`<shadowLogDir>/YYYY-MM-DD.jsonl`, one JSON object per line:

| Field      | Meaning                                                                                                                                |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `ts`       | RFC3339 UTC observation time                                                                                                           |
| `kind`     | `enso_recall` (Ensō's answer) · `flatfile_result` (slot owner's answer) · `enso_error` (contained shadow failure)                      |
| `turn`     | sha256[:16] of the prompt/query text — correlates the two sides of one turn                                                            |
| `session`  | host session key, when available                                                                                                       |
| `text`     | the query/prompt, truncated to 500 chars (labeling context)                                                                            |
| `enso`     | `{ mode, degraded?, elapsed_ms, spawn_ms, corpus_entries, results: [{id, specificity, strength}] }`                                    |
| `flatfile` | `{ tool, duration_ms?, is_error?, summary }` (summary truncated to 500 chars)                                                          |
| `error`    | bridge failure detail (`enso_error` records)                                                                                           |
| `used`     | RECALL-DEF placeholder — always `"unknown"` in WP-7; materially-used detection is host work that arrives later without a format change |

`spawn_ms` vs `elapsed_ms` is the per-call-binary overhead — the recorded
datum (RH-2) that decides if a long-lived sidecar is ever justified.

## What this log is for

A future gate (WP-8, not yet defined) reads this log to decide whether Ensō
takes the memory slot: label the divergent turns (where `enso_recall` and
`flatfile_result` disagree on the same `turn` key), score both sides, and
apply the project's standing rule — _each phase must beat the prior benchmark
or it doesn't ship._

## Standalone status (2026-07-25 lift-and-shift)

- Production code (`index.ts`, `config.ts`, `enso-bridge.ts`, `shadow-log.ts`,
  `api.ts`) is byte-for-byte the same logic as the `clockworksoul/openclaw`
  fork version — only the import paths changed (`openclaw/plugin-sdk/*` from
  the public npm package instead of the monorepo workspace).
- `config.test.ts`, `enso-bridge.test.ts`, `shadow-log.test.ts` are unchanged
  behaviorally; `config.test.ts` swaps the monorepo's
  `openclaw/plugin-sdk/json-schema-runtime` validator for a tiny local
  schema-shape check scoped to exactly what this plugin's `configSchema`
  uses (not a general JSON Schema validator).
- `index.test.ts` swaps `openclaw/plugin-sdk/plugin-test-api`'s
  `createTestPluginApi` for an inline fake `api` object (same four methods
  the plugin actually calls: `config`, `logger`, `on`, `registerTool`) — same
  test assertions, no monorepo-internal import.
- No monorepo, no workspace links, no forked OpenClaw checkout required to
  build, test, or run this extension.
