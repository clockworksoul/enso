# WP-2 hardening: a write-time guard for hand-authored `mem:` blocks (`enso-lint`)

*2026-07-17, Dross Hour. Implemented via Codex per coding policy. Bench/tooling-only; no core or mdstore production code touched.*

## The known, thrice-observed problem

The Phase-1 corpus is authoritative (INV-1: Markdown is canonical, the graph is a
derived cache). Structured `mem:` blocks are hand-authored inline in
`memory/YYYY-MM-DD.md` during consolidation passes (heartbeat, cron, Dross Hour).
The parser is **strict by design** — a block missing `type` or `encoded_time`, or
carrying a malformed ID, is a loud `ParseError`, because those keys are
load-bearing for decay/specificity ranking. Strictness is correct.

The problem is *when* the strictness fires. The daily files live in the **OpenClaw
workspace repo** and get committed there. Nothing on the write path ever calls
`mdstore.Load` (or `Parse`). The only things that call `Load` are the benchmark
suite and `enso-load-check`, neither of which runs at write/commit time. So a
malformed block sits committed and silent until — days later — the Ensō benchmark
suite fails, and **the whole daily file's entries silently drop from the corpus**
until then (INV-1 quietly violated: a memory that was "written" is not actually in
the canonical corpus).

This is not hypothetical. It has bitten the benchmark **three times in four days**:

| Date | File | Failure |
|---|---|---|
| Jul-13 | `memory/2026-07-13.md` | malformed **uppercase** `mem:` ID → whole file failed → dropped the sensitive-comp note + **both** supersession triples |
| Jul-14 | (same class, caught by gate) | malformed ID |
| Jul-15 | `memory/2026-07-15.md` | `mem:` block **missing required `encoded_time`** → whole file failed → dropped every structured entry in it |

Each was found *by accident* when a benchmark run failed, potentially long after
the bad commit. Three real occurrences of the same failure class = a **known
problem**, which is exactly the bar YAGNI sets for building a guard (validate a
confirmed problem, never an imagined one — AGENTS.md "Complexity Kills").

## The fix: a fast, standalone write-time validator

`cmd/enso-lint` — validate one or more daily files (or a whole memory dir) by
running each file's contents through the existing `mdstore.Parse`, reporting every
located error, exiting non-zero if any file fails. It reuses the one canonical
parse entry point, so the lint can never disagree with what `Load` will accept —
no second, drifting implementation of the grammar (avoids the classic
lint-vs-real-parser skew).

### Why not extend `enso-load-check`?

`enso-load-check` loads the **entire** corpus from a hardcoded root and prints a
dump. It's a whole-corpus inspector, not a per-file gate. A pre-commit hook needs
to validate *just the staged daily files*, fast, with a clean nonzero exit and
per-file located errors — a different tool with a different contract. Keeping them
separate (inspector vs. gate) is the honest factoring.

### Contract

```
enso-lint [-q] <path>...            # lint specific files
enso-lint [-q] -dir <memory-dir>    # lint every *.md in a dir
```

- Each path is read and passed to `mdstore.Parse`. A file with zero structured
  blocks is valid (prose-only daily files are normal — §3.5a inline layout).
- On any `ParseError`, print `FILE:LINE: message` to stderr and continue to the
  next file (report ALL failures in one run, don't stop at the first — a
  consolidation pass may have written several blocks).
- Exit `0` iff every file parsed clean; exit `1` if any failed; exit `2` on
  usage/IO error (missing file, unreadable dir).
- `-q` suppresses the per-file OK lines (only failures + summary), for hook use.
- Summary line to stdout: `enso-lint: N files, M entries, K edges, F failed`.

### Design constraints (from the standing discipline)

- **Reuse `mdstore.Parse` verbatim.** No re-implementation of the grammar. The
  guard's whole value is that it's the *same* check `Load` runs, moved earlier.
- **No production code touched.** New `cmd/` package only. `core` and `mdstore`
  stay byte-identical (RH-1/RH-4).
- **Right-sized.** ~120 LoC + a table test. Not a framework. It reads files and
  calls one function.

## Wiring (documented, opt-in — not force-installed)

The daily files live in the **workspace** repo, so the natural home for the hook
is there, not in the enso repo. Document the one-liner in `FORMAT.md` /
`ENSO-STATUS.md`; do not auto-install a git hook (that would be modifying Matt's
workspace git config unprompted — ask first). The hook body is just:

```sh
# .git/hooks/pre-commit (workspace repo) — validate staged daily files
staged=$(git diff --cached --name-only --diff-filter=ACM | grep '^memory/.*\.md$') || exit 0
[ -z "$staged" ] && exit 0
exec go run github.com/clockworksoul/enso/cmd/enso-lint -q $staged
```

Until wired, the manual invocation before committing a consolidation pass is:

```sh
go run github.com/clockworksoul/enso/cmd/enso-lint ~/.openclaw/workspace/memory/$(date +%F).md
```

## What this is and isn't

- **Is:** a write-time move of the existing strict parse, so a malformed block is
  caught at authoring time (seconds after writing) instead of by a benchmark run
  days later. Closes the "silent INV-1 violation until next bench run" gap.
- **Isn't:** a new grammar, a schema change, or an auto-fixer. It does not repair
  bad blocks — it names them loudly so the author fixes them while the context is
  fresh. (An auto-fixer would risk guessing `encoded_time`, which is exactly the
  load-bearing value a human must supply correctly.)

## Status

- Implemented via Codex (coding policy).
- New package `cmd/enso-lint` only; `make check` must stay green with no core/
  mdstore diff.
- Follow-up (not built tonight, ask Matt first): install the workspace pre-commit
  hook so the guard actually fires on every consolidation commit.
