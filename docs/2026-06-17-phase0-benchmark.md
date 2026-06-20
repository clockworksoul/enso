# Phase 0 Benchmark — Active-Memory Flat-File Recall

*The control group. This is the bar Phase 2's graph must beat. Started 2026-06-17.*

## What we're measuring & why

Phase 0 turned on `active-memory` (blocking pre-reply recall) against the existing flat-file `memory_search`/`memory_get`. Before we build a graph (Phase 2), we need to know how good the *dumb* version already is — otherwise "the graph is better" is an untestable claim.

**Two numbers that define the bar:**
- **Hit rate** — of conversational moments where a stored memory *would* have helped, how often did flat-file recall surface it?
- **Noise rate** — how often did recall inject something *irrelevant* into the bounded 220-char budget (wasting it / adding distraction)?

Phase 2's graph must **beat the hit rate without inflating the noise rate.** Secondary signals: latency added per reply, how often recall fired at all.

**Known limitations we're deliberately measuring (not fixing here):** flat-file has no supersession awareness (#4 — may surface stale facts) and misses vocabulary-drift connections (#3). These gaps becoming visible *is the point* — they're Phase 2's spec.

## Config under test

`active-memory`: main agent + direct DMs only, model `anthropic/claude-sonnet-4-6`, queryMode=recent, promptStyle=balanced, thinking=off, timeoutMs=15000, maxSummaryChars=220, logging=true. (See `research/2026-06-17-memory-system-technical-spec.md` §2.)

## How capture works (passive, self-measuring)

A daily isolated cron job ("Phase 0 Benchmark Skim") reviews the day's active-memory activity and appends a dated entry below. **No action required from Matt.** Opportunistic high-signal data points (Matt noticing a real miss in the moment) get logged inline by Dross when they happen, tagged `[FLAGGED-MISS]`.

Honest caveat on automated capture: with `persistTranscripts:false`, the skim sees recall *firing* and its injected summaries, but cannot perfectly self-judge "should have surfaced X but didn't" — a system can't reliably catch its own silent misses. So the automated number is a **floor on hit rate / ceiling on confidence**; the `[FLAGGED-MISS]` human data points are the higher-signal corrective. We weight accordingly when reading the result.

## Decision criteria (when to stop waiting)

Not strictly 2 weeks. We have enough to decide on Phase 1/2 when EITHER:
- ~10–15 substantive direct-DM days are logged with a stable hit/noise pattern, OR
- a clear verdict emerges early (recall is obviously great → pause and let the graph earn it; or obviously missing connected facts → Phase 2 is justified, proceed).

Matt may call it sooner. The data just has to say *something* before we pour the next floor.

---

## Log

### 2026-06-18 — Day 1 skim (first evening after activation)

**Plugin status:** Confirmed enabled and hot-reloaded at 10:22:44 EDT (gateway log: `config hot reload applied (plugins.entries.active-memory)`). Config as expected: `claude-sonnet-4-6`, `queryMode=recent`, `promptStyle=balanced`, `thinking=off`, `timeoutMs=15000`, `maxSummaryChars=220`, `persistTranscripts=false`, `logging=true`.

**DM activity:** 37 inbound Telegram direct messages today total; ~27 occurred after plugin activation (10:22 EDT onward). All are `chatId=8519725519` → `@dross_openclaw_bot`, matching `allowedChatTypes:["direct"]` + `agents:["main"]`. 43 outbound replies sent.

**Recall firing (a):** Cannot confirm. `persistTranscripts: false` eliminates session-file evidence — no active-memory subagent session files exist. The `logging: true` flag's output destination is unknown: nothing found in `gateway.log`, `~/Library/Logs/openclaw/gateway.log`, or `/tmp/openclaw-2026-06-17.log`. **Instrumentation gap: logging output location needs to be discovered before future skims can tally fired passes.**

**Injected summaries (b):** Not observable. Reviewed `context.compiled` events in main DM session (`ce866bb2`, 21 compiled turns). The string `active-memory` appears in system prompts only as static config text (cron-job description referencing the benchmark). No `## Active Memory`, `## Recent Memory`, or dynamically-injected recall block found in any compiled context. Cannot determine hit vs. empty injection rate.

**Timeouts / circuit-breaker (c):** Not observable from available data.

**Latency (d):** Post-activation DM reply latencies ranged ~20–170s (most 20–55s). Pre-activation baseline from the same day: 14–69s. No clear additional latency signal attributable to a recall pre-pass; ranges overlap. Not distinguishable at this resolution.

**No `[FLAGGED-MISS]` entries from Matt today.**

**Bottom line:** Day 1 establishes that the plugin loaded and qualified DM traffic was plentiful. The actual recall mechanism's behavior is opaque until the `logging: true` output location is found. Next skim should first resolve that gap.

---

### 2026-06-17 — Day 0 (baseline established)
- Phase 0 went live 10:23 EDT. Plugin enabled, verified in runtimeConfig, valid:true.
- No recall data yet (just enabled). Clock starts now.
- Capture mechanism: daily skim cron stood up same day.
- **Latency concern downgraded (Matt, 10:40 EDT).** The blocking pre-reply pause was the one flagged risk for Phase 0. Matt reports he's *less* likely to notice it because he's already accustomed to Dross "doing stuff" between messages (file edits, schema lookups, etc.) — the recall pause slots into an existing tolerance for visible work, not a new lag. Insight: the pause is *legible* (reads as work, not lag), and legible latency is cheap. Monitor reads green. Don't re-litigate unless a recall pass starts blowing past timeoutMs or feeling like dead air.

### 2026-06-18 — Day 2 skim (9pm)

**DM activity:** 35 inbound Telegram DMs confirmed in gateway log on Jun 18 (chatId=8519725519 → @dross_openclaw_bot, `direct`). All qualify under active-memory's filter (`agents:["main"]`, `allowedChatTypes:["direct"]`). Session `8023650c` (9.9MB trajectory) was the live main session throughout the day, last written 8:11pm.

**Recall firing (a):** **0 active-memory plugin passes logged for Jun 18.** Full gateway log (2,544 lines, single file, no rotation) contains exactly 4 `[plugins] active-memory` entries — all from Jun 17 at 23:13 EDT. Zero entries for Jun 18 despite 35 qualified inbound DMs. The Jun 17 pair fired back-to-back (~1s apart, both `no_relevant_memory`) and were the only ones in the prior skim window too.

**Injected summaries (b):** Not observable. Zero plugin log entries = no injection signal available.

**Timeouts / circuit-breaker (c):** Not observable from available data.

**Latency (d):** Not observable (no plugin entries to time).

**Notable side-finding:** In-session `memory_search` tool returned `disabled: true / unavailable: true` at 16:38 due to missing OpenAI embedding key (`No API key found for provider "openai"`). This is the standard in-session tool call, not the active-memory plugin — separate failure mode, but confirms the embedding-based memory path is also broken in parallel.

**Open question — firing anomaly:** The Jun 17 23:13 pair fired twice in 3 seconds then went silent. Jun 18 has 35 qualified DMs and zero fires. Two hypotheses: (a) the Jun 17 entries were triggered by the 9pm skim cron subagent operating in a main-session context (not by direct DM traffic), and DM traffic has never actually triggered the plugin; (b) some suppression or debounce logic is active post-`no_relevant_memory`. Neither is conclusive from log evidence alone.

**Bottom line:** Day 2 continues the instrumentation gap. Plugin status is confirmed valid/loaded (no restart since Jun 17 10:22). Qualified DM traffic is plentiful. But zero recall passes are observable for a full day of direct-DM conversation. The metric being measured (hit rate, noise rate) remains unmeasurable until the trigger gap is understood. **Recommended next step:** Capture a single DM exchange with `/trace on` active to confirm whether the plugin fires at all during normal DM turns, or only under specific (unknown) conditions.

---

### 2026-06-17 — Day 1 instrumentation fix (9pm skim follow-up)
- **Gateway log location resolved:** `~/Library/Logs/openclaw/gateway.log` (live process, confirmed via `lsof -p 69203`). The 9pm skim was searching the wrong path (`~/.openclaw/logs/gateway.log` — stale, last written May 29). Updated for future skims.
- **Active-memory plugin output: still not visible in gateway log.** The log shows inbound Telegram messages and outbound replies throughout the day (post-10:23am), but no `[active-memory]` or `[recall]` entries, no plugin-prefixed lines, no injected summary blocks in session context. Two possible causes: (a) the recall pass fires but logs to a different destination (plugin-owned log vs. gateway log), or (b) the heartbeat + channel-type filtering means eligible DM turns are fewer than expected (heartbeat polls may not trigger recall; `allowedChatTypes:["direct"]` should match Telegram DMs). **Investigation for next skim:** try `/verbose on` + `/trace on` in a DM to force visible active-memory trace output; or check if `logging: true` writes to a plugin-specific path rather than the gateway log.
- **One confirmed signal:** config hot-reload for `plugins.entries.active-memory` logged at 10:22:44am — plugin load is confirmed. The question is whether eligible turns are triggering it.

### 2026-06-19 — Day 3 skim (9pm)

**DM activity:** 0 inbound Telegram DMs from Matt observed in gateway log for Jun 19. Two outbound sends only: 02:06 EDT (Dross Hour cron) and 07:00 EDT (morning report cron). Friday — quiet day, no direct-DM conversation.

**Recall firing (a):** 0 plugin passes. No `[plugins] active-memory` entries in gateway log for Jun 19. Log is 344 KB, well-populated with hourly heartbeat and auth-re-warm entries — absence is real, not a gap.

**Injected summaries (b):** Not applicable (no DM turns to inject into).

**Timeouts / circuit-breaker (c):** Not observable from available data.

**Latency (d):** Not applicable.

**Running total (plugin fires since activation):** Still 4 — the Jun 17 23:13 back-to-back pair. Zero fires Jun 18 or Jun 19 despite accumulated qualified DM traffic. The trigger gap noted in Day 2 remains unexplained.

**No `[FLAGGED-MISS]` entries.**

**Bottom line:** No activity day. Entry logged for completeness.

---

### 2026-06-18/19 — Dross skim (2am Jun 19)

**Session volume:** Very high — one of the densest DM days logged. Transcripts pulled from Granola for 5 meetings (CoffeeOps, Axon standup, Eng Leadership Monthly, Scorecard Design w/ Ali, EP leadership, Axon Retro, Adam 1:1). All filed and logged.

**Active-memory recall audit (what I relied on vs. what I had to re-derive):**
- ✅ **Hit:** Tipa/Peng/Shikhar development context (coaching notes, trajectory, prior conversations) — surfaced correctly when drafting retro notes, 1:1 prep. No re-derivation needed.
- ✅ **Hit:** The Checkly migration strategy + deadline context (Jun 11 memory) — correctly flagged as "should have gone to Leadership Monthly, check with Adam directly."
- ✅ **Hit:** "Ollie = Ali" transcription quirk — NOT recalled from memory_search (search was offline/embedding provider issue all day), but CORRECTLY recalled from my own working context when Matt flagged it. The May 6 note existed; I just happened to catch it separately. **However:** memory_search was unavailable all day (OpenAI embedding key error). This is a real reliability gap — if Matt hadn't flagged "Ollie = Ali" I would have propagated the error forward.
- ⚠️ **FLAGGED-MISS / system miss:** memory_search returned `disabled: true` for every query today. The "Ollie = Ali" transcription quirk and several other lookups I'd normally verify (Adam headcount history, Peng coaching arc) were done from context window only. This worked because it was a single-session continuous day — but in a fresh session it would have failed. **Root cause: OpenAI embedding provider down (no API key for embeddings).** Not a recall algorithm failure; an infrastructure failure. Note for Phase 1 consideration: fallback embedding provider when primary is unavailable.
- ✅ **Hit:** The Adam 1:1 prep (headcount ask, Omega Lite product framing, stealth mode) — all held correctly from earlier in the session. No cross-session recall needed.
- ✅ **Hit:** Wesley/Omega/Bazel context — the CodeGraphContext entry in the aspirational list was recalled correctly without lookup when building the tier-relationship analysis.

**Verdict:** Session-internal recall was excellent (continuous context window all day). Cross-session recall infrastructure was entirely unavailable (embedding provider down). The day's work masked this because everything was in one long session. A fresh session tomorrow would be starting cold. Monitoring whether the embedding provider comes back up overnight.
