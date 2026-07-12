# Ensō Memory Format

*The on-disk grammar for structured memory entries. A human can write a valid
entry from this document alone — no code required.*

---

## Overview

Structured entries live **inline** in daily notes at `memory/YYYY-MM-DD.md`,
interleaved with ordinary prose. The parser skips everything that isn't an Ensō
block. You can write freely around them.

A block is opened by a `###` heading that starts with `mem:` or equals `edge`.
Any other `###` heading is treated as prose.

---

## Entry block

```markdown
### mem:YYYY-MM-DD-<slug>
- type: <NodeType>
- content: <one-line payload>
- encoded_time: <ISO-8601>
- event_time: <ISO-8601 | null>
- valid_from: <ISO-8601 | null>
- valid_until: <ISO-8601 | null>
- confidence: <high | medium | low>
- tags: [<tag>, ...]
- about: [<entity-ref>, ...]
# --- reserved; written now, inert until Phase 3 ---
- last_ref_time: <ISO-8601>
- S_last: <float>
- S_floor: <float>
- lambda: <float>
- S_cap: <float>
```

### Field reference

| Field | Required | Notes |
|---|---|---|
| `id` (the `### mem:` header) | ✅ | `mem:YYYY-MM-DD-<kebab-slug>`. Stable forever; never reused. |
| `type` | ✅ | One of: `Fact` `Decision` `Insight` `Person` `Project` `Task`. Hard error on unknown values. |
| `content` | ✅ | One-line human-readable payload. The thing you want to remember. |
| `encoded_time` | ✅ | UTC ISO-8601 — **when you wrote this down**, not when it happened. |
| `event_time` | key must be present | UTC ISO-8601 or `null`. When the event became true in the world. May differ from `encoded_time`. |
| `valid_from` | key must be present | UTC ISO-8601 or `null`. When this belief became valid. |
| `valid_until` | key must be present | UTC ISO-8601 or `null`. `null` = still true. Set by the supersession ceremony. |
| `confidence` | ✅ | `high` `medium` `low`. |
| `tags` | ✅ (may be `[]`) | `[tag1, tag2]` or `[]`. |
| `about` | key must be present | Entity refs: `[project:omega, person:matt]` or `[]`. |
| `last_ref_time` | reserved | Init = `encoded_time`. Updated on material recall (Phase 3). |
| `S_last` `S_floor` `lambda` `S_cap` | reserved | Decay parameters. `// TUNABLE` — calibrated in Phase 3. |

**Key-presence rule:** absent is a parse error, `null` is a known-unknown.
If a field is optional but listed above as "key must be present", write `null`
rather than omitting the line.

### Node types

| Type | When to use |
|---|---|
| `Fact` | A factual belief about the world that can become stale |
| `Decision` | A resolved choice — what was decided and why |
| `Insight` | A learned pattern or non-obvious observation |
| `Person` | A persistent record about a person |
| `Project` | Persistent state about a project or initiative |
| `Task` | A time-bound to-do with inherent volatility |

---

## Edge block

```markdown
### edge
- from: mem:<id>
- type: <EdgeType>
- to: mem:<id> | <entity-ref>
```

| Field | Required | Notes |
|---|---|---|
| `from` | ✅ | A `mem:` ID. |
| `type` | ✅ | `SUPERSEDES` `RELATES_TO` `OWNS` `ABOUT`. |
| `to` | ✅ | A `mem:` ID or entity-ref string. |

---

## Supersession ceremony

When a belief changes, **never edit history**. Instead, append three blocks:

1. The **new entry** (the current truth).
2. A **closed copy** of the old entry — identical except `valid_until` is set to
   the timestamp of the new entry's `encoded_time`.
3. A **SUPERSEDES edge** — `from:` the new entry, `to:` the old entry.

```markdown
### mem:2026-07-06-granola-banned-yext
- type: Fact
- content: Granola is being uninstalled from all Yext devices; REST API still works without the app
- encoded_time: 2026-07-06T14:00:00Z
- event_time: 2026-07-06T00:00:00Z
- valid_from: null
- valid_until: null
- confidence: high
- tags: [granola, yext, tooling]
- about: [project:granola]
- last_ref_time: 2026-07-06T14:00:00Z
- S_last: 1
- S_floor: 0.05
- lambda: 0.05
- S_cap: 1

### mem:2026-06-25-granola-keep-using
- type: Fact
- content: Granola remains the transcript source of record; keep using it
- encoded_time: 2026-06-25T00:00:00Z
- event_time: null
- valid_from: null
- valid_until: 2026-07-06T14:00:00Z
- confidence: high
- tags: [granola, yext, tooling]
- about: [project:granola]
- last_ref_time: 2026-06-25T00:00:00Z
- S_last: 1
- S_floor: 0.05
- lambda: 0.05
- S_cap: 1

### edge
- from: mem:2026-07-06-granola-banned-yext
- type: SUPERSEDES
- to: mem:2026-06-25-granola-keep-using
```

The old entry is preserved with `valid_until` set. "What did Ensō believe about
Granola as of June 25th?" is always answerable.

---

## Appending with the CLI

```bash
# Normal append
enso-append \
  -root ~/.openclaw/workspace \
  -type Fact \
  -content "The REST API works without the desktop app" \
  -tags "granola,tooling" \
  -confidence high

# Supersession
enso-append \
  -root ~/.openclaw/workspace \
  -supersede mem:2026-06-25-granola-keep-using \
  -type Fact \
  -content "Granola is being uninstalled from all Yext devices; REST API still works" \
  -tags "granola,yext,tooling"

# Preview without writing
enso-append -dry-run -type Fact -content "..." -root ~/.openclaw/workspace
```

---

## Inline example (how it looks in a daily note)

```markdown
# 2026-07-12

Had a great morning working on Ensō. The benchmark results came in.

### mem:2026-07-12-enso-supersession-benchmark
- type: Insight
- content: Semantic embedding (session memory) scores 0.61 on supersession — no better than recency baseline (0.63); Ensō scores 1.00
- encoded_time: 2026-07-12T12:00:00Z
...

Then we ratified WP-2 and started building. The grammar is now frozen.

### mem:2026-07-12-wp2-ratified
- type: Decision
- content: WP-2 ratified: grammar frozen, inline entries, placeholder decay values
...
```

The prose lines before and after the blocks are completely ignored by the parser.
