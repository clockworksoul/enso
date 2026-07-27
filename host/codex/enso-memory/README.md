# Ensō Memory for Codex

Recall-only Codex adapter for the portable Ensō memory substrate. It invokes
the existing `enso-recall` Go binary for every `UserPromptSubmit` event and
keeps all Codex concepts outside the canonical Markdown corpus.

## Modes

- `off`: do nothing.
- `shadow` (default): run recall and append metadata to a local JSONL log, but
  inject nothing into the Codex turn.
- `live`: log the same metadata and inject a strictly bounded set of recalled
  entries as additional developer context.

Every failure exits successfully without context. A missing binary, unreadable
corpus, timeout, malformed output, schema mismatch, or logging failure cannot
prevent Codex from handling the prompt.

## Prerequisites

Build the host-independent bridge from the Ensō repository:

```bash
go build -o /absolute/path/to/enso-recall ./cmd/enso-recall
```

Python 3 is required for the standard-library-only hook wrapper.

## Configuration

Copy [`config.example.json`](config.example.json) to a stable location and set:

```bash
export ENSO_CODEX_CONFIG=/absolute/path/to/config.json
```

When installed as a plugin, the hook also reads `$PLUGIN_DATA/config.json` if
present. Individual environment variables override the JSON file:

| Environment variable | JSON key | Default |
| --- | --- | --- |
| `ENSO_CODEX_MODE` | `mode` | `shadow` |
| `ENSO_CORPUS_ROOT` | `corpus_root` | required |
| `ENSO_RECALL_BIN` | `enso_binary` | `enso-recall` |
| `ENSO_CODEX_TOP_K` | `top_k` | `5` |
| `ENSO_CODEX_TIMEOUT_MS` | `timeout_ms` | `4000` |
| `ENSO_CODEX_MAX_CONTEXT_CHARS` | `max_context_chars` | `4000` |
| `ENSO_CODEX_SHADOW_DIR` | `shadow_log_dir` | `$PLUGIN_DATA/shadow` |

Live mode must be explicitly enabled:

```bash
export ENSO_CODEX_MODE=live
```

Codex requires review and trust for plugin-bundled command hooks. Inspect it
with `/hooks` after installation.

## Telemetry

The adapter writes one JSON object per line under the configured shadow
directory. Records contain session/turn identifiers, cwd, a SHA-256 of the
prompt, recall mode, latency, result IDs, and scores. Prompt text and recalled
content are not logged.

## Non-goals

C0 does not capture memories, commit supersessions, update recall strength,
parse transcripts, run a daemon, expose MCP tools, or replace native Codex
memories.
