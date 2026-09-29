# WP-7 amendment — co-key the two shadow hooks (2026-09-29, Dross Hour)

*Executes finding #2 of `docs/2026-09-24-wp8-shadow-corpus-first-analysis.md`: the
"true long pole" for WP-8. No Go core touched; TS host adapter only.*

## The problem this fixes

The 2026-07/08 shadow corpus (1,261 records, 18 days) produced **0 A/B-comparable
turns** — 0/820∩53 turn-hash overlap between the Ensō side and the flat-file side.
WP-8's literal deliverable (label divergent turns, pick a winner) was **not executable
against that instrumentation**, and no shadow window of any length would have fixed it,
because the flaw is structural, not a sample-size problem.

Root cause, confirmed by reading the two hook handlers:

- **Ensō side** (`before_prompt_build`) keyed its record on `turnKey(prompt)` — a
  sha256 of the **whole assembled prompt**.
- **Flat-file side** (`after_tool_call`) keyed on `turnKey(query)` — a sha256 of the
  **tool call's `query` argument**.

Those two strings are never equal, so the hashes never coincide. Worse, the
`after_tool_call` handler only accepted the `event` argument — it never even received
the hook `ctx`, so it had no access to any host-authoritative id.

## The fix

Both hook contexts already expose the same host-authoritative per-turn identifiers.
Verified against the published SDK type defs
(`openclaw/dist/plugin-sdk` → `hook-runner-global-*.d.ts`):

- `PluginHookAgentContext` (ctx for `before_prompt_build`): `runId`, `sessionId`,
  `sessionKey`.
- `PluginHookToolContext` (ctx for `after_tool_call`): `runId`, `sessionId`,
  `sessionKey`. The `after_tool_call` **event** also carries `runId` as a second source.

So the correlation key was switched from a text hash to `runId`:

- New `correlationKey(ctx, fallbackText)` in `shadow-log.ts`: prefers `runId`, then
  `sessionId`, then degrades to the legacy `turnKey(fallbackText)` hash only when the
  host supplies neither id. Returns the key **and** its provenance
  (`turn_src: "runId" | "sessionId" | "text"`).
- `before_prompt_build` now passes its `ctx` into `shadowRecall`.
- `after_tool_call` now accepts `ctx`, and prefers `ctx.runId`, then the event's
  `runId`, then text.
- New `turn_src` field on every `ShadowRecord` so an analysis pass can trust a
  host-id pairing and discount any legacy text-hash record instead of silently
  mixing them.

The fallback chain matters: it is why this is safe to ship without a fresh SDK
guarantee. If a future host omits `runId`, records still write (as before,
unpairable) rather than crash — but on this host, where `runId` is present, both
sides now correlate.

## Latent bug found and fixed in passing

`summarizeToolResult(undefined)` threw `TypeError: Cannot read properties of
undefined (reading 'replace')`, because `JSON.stringify(undefined)` returns
`undefined` (not a string) and the code called `.replace` on it. A memory tool call
whose result was `undefined` would have thrown into the observer (contained by the
outer try/catch, but logged as a failure and dropping the record). Guarded: non-string
serialization now becomes `"(no result)"`. This is exactly the class of robustness gap
the error arm of the 2026-09 corpus was full of.

## Tests

- `correlationKey` unit tests (`shadow-log.test.ts`): runId preferred; **the two hooks
  pair when they share a runId but hash different text** (the literal 2026-09 failure,
  now passing); sessionId fallback; text-hash degrade with `turn_src:"text"`.
- End-to-end (`index.test.ts`): register the real plugin, fire `before_prompt_build`
  and `after_tool_call` with the same `runId` — both emitted records carry the same
  `turn` and `turn_src:"runId"`. Plus an `after_tool_call`-falls-back-to-event-runId
  test.
- 32/32 vitest green (was 26); `tsc --noEmit` clean.

## What this does NOT do (honest scope)

1. **`used` is still `"unknown"`.** Co-keying is one half of finding #2; the other half
   — fill `used` from a material-use signal (RECALL-DEF telemetry) — is untouched. A
   paired turn can now be *found*, but "which engine's result was actually used" still
   has no ground-truth label. That needs a host RECALL-DEF event, which does not exist.
2. **It does not re-open the shadow window.** Findings #1 (the 404 vector outage, fixed
   in `dae4f48`) and #3 (query-embedding latency, 16–20s wall, 4–5× over the 4s
   deadline) still gate a *useful* fresh window. Co-keying makes a future corpus
   pairable; latency makes a vector-mode corpus *possible*. Both are required before
   WP-8 can decide slot takeover, and this pass only addresses the first.
3. **No slot-takeover decision.** Correctly still blocked.

## Next (unchanged priority order)

1. ~~Co-key the two hooks~~ — **done, this pass.**
2. Fill `used` from a material-use signal (WP-7 amendment, the remaining half of
   finding #2) — blocked on a host RECALL-DEF event.
3. Address query-embedding latency (finding #3, RH-2 case now logged) before any
   fresh vector-mode window: cache query embeddings, warm/local embedder, or move the
   embed off the per-call critical path.
4. Only then: re-run shadow with the vector path live, label divergent turns, decide
   takeover by the standing rule.

*Files: `host/openclaw/enso-memory/{shadow-log.ts,index.ts}` + their tests; root
`.gitignore` (added a global `node_modules/` rule — a stray `host/openclaw/node_modules/`
was untracked and unignored). No Go core, no corpus writes.*
<!-- project: github.com/clockworksoul/openclaw-memory -->
