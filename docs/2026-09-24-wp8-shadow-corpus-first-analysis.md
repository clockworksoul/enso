# WP-8 — First analysis of the real shadow corpus (2026-09-24, Dross Hour)

*Status of WP-8 before this pass: "undefined by design, blocked on real shadow-log
data that does not exist yet (`.enso/shadow/` still empty as of Jul 23/24)."*

**That block is lifted: the data now exists.** 18 days of real shadow observation
(2026-07-25 → 2026-08-11), 1,261 records, live traffic, zero effect on production.
This is the first look at it. The headline is not the one the WP-8 plan anticipated.

---

## TL;DR — three load-bearing findings

1. **The corpus does not measure what WP-8 needs it to measure.** All 823
   "successful" Ensō recalls ran in **`degraded` (lexical) mode — 100% of them** —
   because the Gemini embedding endpoint returned **HTTP 404 for the entire
   window**. `vector_ok = 0` on every single day. The vector recall pipeline that
   WP-4's gate validated (1.00 P@1, the whole reason to prefer Ensō over flat
   search) **never once fired during shadow observation.** The corpus is real; it
   validates the *fallback path*, not the recall quality the slot-takeover decision
   hinges on.

2. **The shadow corpus cannot produce a single A/B-comparable turn.** The two hooks
   key on structurally different inputs and share **zero** keys — 0 turn-hash
   overlap and 0 exact-query-text overlap across 820 Ensō turns and 53 flat-file
   turns. Direct divergence labeling (the literal WP-8 task) is **impossible from
   this corpus as instrumented.** This is a WP-7 design flaw surfaced only by real
   data, exactly the kind of thing shadow mode is for.

3. **The degradation contract held perfectly — that is the one thing this corpus
   genuinely proves.** Across 823 forced-fallback recalls: 0 empty result sets,
   every call fell back to lexical cleanly, p99 latency **3.94 s** (under the 4 s
   shadow deadline), never threw into the live turn. The WP-4/WP-7 fail-safe design
   is validated on real traffic — which matters, because it means the 404 outage was
   invisible to Matt the whole time. (That invisibility is also finding #1's teeth:
   a silent 3-week vector outage that nothing alerted on.)

---

## The corpus

| Metric | Value |
|---|---|
| Window | 2026-07-25 → 2026-08-11 (18 days) |
| Total records | 1,261 |
| `enso_recall` (success) | 823 |
| `enso_error` | 342 |
| `flatfile_result` | 96 (53 `memory_search`, 43 `memory_get`) |
| Distinct turns | 1,212 |
| Paired turns (both engines) | **0** |

Logging stopped 2026-08-11 — the last `status.json` error records Matt's turn *"Turn
off all of Ensō please. I think I'm done."*, so the shadow plugin was almost
certainly disabled that day. `status.json` still reads `state: degraded,
consecutiveErrors: 67`.

---

## Finding 1 — 100% degraded: the vector path never ran

Every `enso_recall.enso.degraded` field carries the identical string:
`embed query: gemini embed: HTTP 404:`. Per-day breakdown: `vector_ok` is **0 on all
18 days**; `degraded` equals the full success count every day.

Root cause is the embedding model retirement fixed in enso commit **`dae4f48`
("Fix Gemini embedding model retirement (001 -> 2) and endpoint suffix bug")**,
landed ~Aug 10 — *after* essentially the entire shadow window. During Jul 26–Aug 11
the binary requested the retired `gemini-embedding-001`, got 404, and fell back to
lexical every time.

Consequence for WP-8: the standing rule is *"after DM-days of shadow logs, label the
divergent turns and decide slot takeover."* But the recall arm under observation was
lexical-only. Deciding takeover on this corpus would be deciding it on the **wrong
pipeline**. The corpus is a valid test of "does lexical fallback stay safe under a
provider outage" (yes) and nothing more.

**Current state (verified today, 2026-09-24):** with the `001→2` fix in place,
`enso-recall` runs in real `vector` mode against the live corpus (68 entries) and
returns results — so the outage is fixed. A *new* shadow window would now capture the
vector path. See finding 3 for the catch.

## Finding 2 — the corpus is structurally unpairable

The WP-7 host adapter observes two things:
- **Ensō side:** `before_prompt_build`, keyed on a hash of the whole assembled prompt.
- **Flat-file side:** `after_tool_call` on `memory_search`/`memory_get`/`memory_get`,
  keyed on the *tool call's query argument*.

These are never the same string, so the turn hashes never coincide (0/820∩53) and the
raw `text` fields never match exactly (0 exact-text overlap). The `used:"unknown"`
placeholder is also never filled — no RECALL-DEF/material-use telemetry exists — so
even a heuristic pairing would have no ground-truth "which engine's result was
actually used" label to score against.

**Net:** WP-8's core deliverable (label divergent turns, pick the winner) is not
executable against this instrumentation. Fixing it is a WP-7 amendment, not a WP-8
analysis: co-key both hooks to a shared per-turn id, and fill `used` from a
material-use signal.

## Finding 3 — the degradation contract is proven; the latency story splits by mode

On the 823 forced-lexical recalls:
- Latency p50 **3.48 s**, p90 3.77 s, p99 **3.94 s**, max 3.97 s. **0/823 over the
  4 s deadline.** (Note the floor is ~3.1 s even in lexical mode — that is the failed
  Gemini call's own round-trip *before* it 404s and falls back; real lexical-only cost
  with no key is ~100 ms, measured today.)
- 0 empty result sets; every degraded call still returned ranked results.
- Flat-file incumbent for comparison: p50 84 ms, but a **10.7 s tail** (`memory_get`
  on large files) — so the incumbent is not uniformly fast either.

**The catch for a fresh vector-mode shadow window:** measured today, a real
`vector`-mode `enso-recall` against the live corpus takes **16–20 s wall at ~7 % CPU**
— i.e. almost entirely the synchronous Gemini query-embedding round-trip
(`GeminiEmbedder.Embed` → `generativelanguage.googleapis.com/v1beta/models/…`), not
compute. That is **4–5× over the 4 s shadow deadline.** So the moment the vector path
is actually exercised (now that 404 is fixed), it will breach the deadline and
generate the exact `Command failed`/timeout errors already seen in the error arm.
WP-7's RH-2 latency escape hatch ("no sidecar until a latency case is logged") now has
its logged case: **query-embedding latency is the case.** A fresh window will not
succeed without either query-embedding caching, a warm local embedder, or moving the
embed off the per-call critical path.

## The error arm (342 records), for completeness

- **177 `Command failed`** — shell-argument fragility (the CLI takes the raw query as
  an argv string; queries containing newlines, JSON blobs, and the `untrusted-text`
  routing metadata break the invocation) plus timeouts. Real robustness gap: the query
  should pass via stdin or a temp file, not argv.
- **132 corpus parse errors** — `mem: 2026-07-27-dross-memory-design-principles`
  reported *missing required key "content"* at line 23 for weeks. **The current file
  is well-formed and parses clean today** (verified: 68 entries, exit 0), so this was
  a transient corpus state during the window, since fixed. But it silently dropped a
  whole daily file from the corpus for ~2 weeks with nothing alerting — the exact
  INV-1 silent-corruption class `enso-lint` was built to catch, and confirmation that
  the write-time guard (still not installed as a pre-commit hook) earns its keep.
- **33 `ENOENT`** — the early `api.config`→`api.pluginConfig` binary-path bug, fixed
  Jul 26 (`cdc36c5`).

---

## What this changes for WP-8

WP-8 stays **blocked**, but the blocker is now *specific and actionable* instead of
"no data exists":

1. **Instrumentation fix (WP-7 amendment) — co-key the two hooks** to a shared turn id
   and fill `used` from a material-use signal. Without this, no shadow corpus of any
   length can produce a labeled divergence set. This is the true long pole.
2. **Re-run shadow with the vector path actually live** (404 is fixed) — but only
   after the latency case is addressed, or the window will be all timeouts.
3. **Address query-embedding latency** (RH-2 case now logged): cache query embeddings,
   use a warm/local embedder, or take the embed off the per-call path.
4. **Do not decide slot takeover on the current corpus.** It measures lexical
   fallback safety (which passed), not vector recall quality (which never ran).

The honest one-liner: *the shadow corpus WP-8 waited for arrived, and its first real
lesson is that the observation rig itself needs a fix before it can answer the WP-8
question — plus a concrete, now-unavoidable latency case for the vector path.*

*Analysis scripts (throwaway): `/tmp/enso_shadow_analysis2.py`,
`/tmp/enso_shadow_pairing.py`. No production code touched this pass; no corpus writes.*
