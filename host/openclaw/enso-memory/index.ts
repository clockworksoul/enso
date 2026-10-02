/**
 * OpenClaw Ensō Shadow Memory Plugin (WP-7)
 *
 * Runs Ensō recall ALONGSIDE the active memory plugin on every turn and logs
 * divergence — the live evidence for whether Ensō should ever take the
 * memory slot. Three hard rules, all test-pinned on the Ensō side and
 * enforced structurally here:
 *
 *   1. OBSERVATION ONLY. The hooks never return a modification; the memory
 *      slot, the prompt, and the user experience are untouched.
 *   2. FAIL-SAFE. Any bridge failure (missing binary, timeout, bad JSON) is
 *      logged as an enso_error record and swallowed. A broken shadow must
 *      never break a turn.
 *   3. NO MEMORY LOGIC HERE (PORT-INV). Recall, ranking, supersession, and
 *      decay live in the Go core behind the enso-recall binary; this
 *      extension only spawns, parses, and logs.
 */

import { Type } from "typebox";
import { definePluginEntry, type OpenClawPluginApi } from "./api.js";
import { resolveMemoryEnsoConfig, type MemoryEnsoConfig } from "./config.js";
import { runEnsoRecall } from "./enso-bridge.js";
import {
  appendShadowRecord,
  correlationKey,
  truncateText,
  type ReplyUseVerdict,
  type ShadowRecord,
  type TurnIdContext,
} from "./shadow-log.js";
import { assessMaterialUse } from "./material-use.js";
import { recordOutcome } from "./status.js";

/**
 * WP-8 blocker (a): bounded in-memory bridge from recall time to reply time.
 *
 * `before_prompt_build` recalls memories but the reply does not exist yet; the
 * `llm_output` hook has the reply but not the recalled memory CONTENT (the
 * persistent log stores only ids+scores, to stay bounded and content-free).
 * So at recall time we stash the full recalled content keyed by the turn's
 * runId, and at `llm_output` time we consume it to score material use, then
 * evict. This keeps memory content out of the JSONL while still deriving the
 * RECALL-DEF label. The map is bounded and evicts oldest-first so a turn that
 * never reaches `llm_output` (error, cancel) cannot leak unboundedly.
 */
type PendingRecall = { at: number; memories: Array<{ id: string; content: string }> };
const MAX_PENDING_TURNS = 256;

function rememberRecall(
  pending: Map<string, PendingRecall>,
  turn: string,
  memories: Array<{ id: string; content: string }>,
): void {
  if (memories.length === 0) {
    return;
  }
  pending.set(turn, { at: Date.now(), memories });
  while (pending.size > MAX_PENDING_TURNS) {
    const oldest = pending.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    pending.delete(oldest);
  }
}

/** Tool names whose results the flat-file observer records for comparison. */
const OBSERVED_MEMORY_TOOLS = new Set(["memory_search", "memory_recall", "memory_get"]);

function nowISO(): string {
  return new Date().toISOString();
}

function asText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** Bounded, single-line summary of a tool result for the divergence log. */
export function summarizeToolResult(result: unknown): string {
  let text: string;
  try {
    text = typeof result === "string" ? result : JSON.stringify(result);
  } catch {
    text = "(unserializable tool result)";
  }
  // JSON.stringify(undefined) returns undefined (not a string), so guard before
  // calling string methods -- a memory tool call whose result is undefined must
  // still log cleanly, not throw into the observer.
  if (typeof text !== "string") {
    text = "(no result)";
  }
  return truncateText(text.replace(/\s+/g, " "));
}

function safeAppend(api: OpenClawPluginApi, cfg: MemoryEnsoConfig, record: ShadowRecord): void {
  try {
    appendShadowRecord(cfg.shadowLogDir, record);
  } catch (error) {
    // The log itself failing must not escalate; warn once per occurrence.
    api.logger.warn(`memory-enso: shadow log append failed: ${String(error)}`);
  }
}

// 2026-07-27 (post-incident): report health through the plugin itself, not
// just through the append-only JSONL log. recordOutcome overwrites one small
// status.json with the CURRENT state on every recall attempt, so an external
// checker can answer "is Ensō healthy right now" by reading a few bytes
// instead of tailing and heuristically scoring a growing log. Same fail-safe
// contract as safeAppend: a status-file write failure must never escalate.
function safeRecordOutcome(
  api: OpenClawPluginApi,
  cfg: MemoryEnsoConfig,
  ts: string,
  ok: boolean,
  error?: string,
): void {
  try {
    recordOutcome(cfg.shadowLogDir, ts, ok, error);
  } catch (err) {
    api.logger.warn(`memory-enso: status file write failed: ${String(err)}`);
  }
}

/** Shared by the shadow hook and the manual tool: recall + log, never throw. */
async function shadowRecall(
  api: OpenClawPluginApi,
  cfg: MemoryEnsoConfig,
  queryText: string,
  session: string | undefined,
  turnCtx: TurnIdContext | undefined,
  pending?: Map<string, PendingRecall>,
): Promise<ShadowRecord> {
  const ts = nowISO();
  // WP-8 co-keying: prefer the host-authoritative runId (shared with the
  // after_tool_call side) over a hash of the prompt text, which the flat-file
  // side can never reproduce.
  const { turn, source } = correlationKey(turnCtx, queryText);
  const outcome = await runEnsoRecall(cfg, queryText);
  if (!outcome.ok) {
    const record: ShadowRecord = {
      ts,
      kind: "enso_error",
      turn,
      turn_src: source,
      ...(session !== undefined ? { session } : {}),
      text: truncateText(queryText),
      error: outcome.error,
      used: "unknown",
    };
    safeAppend(api, cfg, record);
    safeRecordOutcome(api, cfg, ts, false, outcome.error);
    return record;
  }
  const record: ShadowRecord = {
    ts,
    kind: "enso_recall",
    turn,
    turn_src: source,
    ...(session !== undefined ? { session } : {}),
    text: truncateText(queryText),
    enso: {
      mode: outcome.output.mode,
      ...(outcome.output.degraded !== "" ? { degraded: outcome.output.degraded } : {}),
      elapsed_ms: outcome.output.elapsed_ms,
      spawn_ms: outcome.spawnMs,
      corpus_entries: outcome.output.corpus_entries,
      results: outcome.output.results.map((r) => ({
        id: r.id,
        specificity: r.specificity,
        strength: r.strength,
      })),
    },
    used: "unknown",
  };
  safeAppend(api, cfg, record);
  safeRecordOutcome(api, cfg, ts, true);
  // WP-8 blocker (a): stash recalled content keyed by the turn so the
  // llm_output observer can score material use. Only pairable (runId/sessionId)
  // turns are worth stashing — a text-hashed key cannot be reproduced by the
  // llm_output side, which hashes nothing.
  if (pending !== undefined && source !== "text") {
    rememberRecall(
      pending,
      turn,
      outcome.output.results.map((r) => ({ id: r.id, content: r.content })),
    );
  }
  return record;
}

/**
 * WP-8 blocker (a): the RECALL-DEF observer. Fires on `llm_output` — the one
 * hook that carries BOTH the host-authoritative runId (co-key with the recall
 * side) AND the assistant's actual reply text. Looks up the memories Ensō
 * recalled on this runId, scores each against the reply, and writes a single
 * `reply_use` record labeling material use. Observation-only and fail-safe:
 * any failure is swallowed so a broken label never touches the turn.
 */
function observeReplyUse(
  api: OpenClawPluginApi,
  cfg: MemoryEnsoConfig,
  pending: Map<string, PendingRecall>,
  event: unknown,
): void {
  const ev = (event ?? {}) as { runId?: unknown; sessionId?: unknown; assistantTexts?: unknown };
  const runId = typeof ev.runId === "string" && ev.runId.trim() !== "" ? ev.runId : undefined;
  const sessionId =
    typeof ev.sessionId === "string" && ev.sessionId.trim() !== "" ? ev.sessionId : undefined;
  const turn = runId ?? sessionId;
  if (turn === undefined) {
    return;
  }
  const recalled = pending.get(turn);
  if (recalled === undefined) {
    return; // Ensō surfaced nothing on this turn (or it was a text-hashed key).
  }
  pending.delete(turn); // one reply per turn; consume so the map stays bounded.
  const replyText = Array.isArray(ev.assistantTexts)
    ? ev.assistantTexts.filter((t): t is string => typeof t === "string").join("\n")
    : "";
  const verdicts: ReplyUseVerdict[] = recalled.memories.map((m) => {
    const v = assessMaterialUse(m.content, replyText);
    return {
      id: m.id,
      used: v.used,
      score: v.score,
      ...(v.evidence !== "" ? { evidence: v.evidence } : {}),
    };
  });
  const anyUsed = verdicts.some((v) => v.used === "yes");
  safeAppend(api, cfg, {
    ts: nowISO(),
    kind: "reply_use",
    turn,
    turn_src: runId !== undefined ? "runId" : "sessionId",
    reply_use: verdicts,
    used: anyUsed ? "yes" : "no",
  });
}

export default definePluginEntry({
  id: "memory-enso",
  name: "Ensō Shadow Memory",
  description:
    "Shadow-mode observer for the Ensō memory system: logs Ensō recall vs flat-file recall divergence without touching the turn.",
  register(api: OpenClawPluginApi) {
    // NOTE (2026-07-26 bugfix): api.config is the whole-host OpenClawConfig,
    // not this plugin's scoped config -- api.pluginConfig is the correct field
    // (maps to plugins.entries.memory-enso.config). Using api.config here for
    // the first ~24h after wiring silently meant every field fell back to its
    // default; corpusRoot/shadowLogDir defaults happened to match the real
    // configured paths (masking the bug), but the default ensoBinary ("enso-recall",
    // bare, not on PATH) does not, so every shadow call failed ENOENT and 100%
    // of records were enso_error until this fix.
    const cfg = resolveMemoryEnsoConfig(api.pluginConfig);
    if (!cfg.enabled) {
      api.logger.info("memory-enso: disabled by config; shadow observation off");
      return;
    }
    api.logger.info(
      `memory-enso: shadow observation on (corpus: ${cfg.corpusRoot}, log: ${cfg.shadowLogDir})`,
    );

    // WP-8 blocker (a): per-turn bridge from recall (before_prompt_build) to
    // reply (llm_output). Keyed by the host runId both hooks share.
    const pendingRecalls = new Map<string, PendingRecall>();

    // Shadow side A — Ensō's answer for the same turn the slot owner serves.
    // Observation-only: the handler NEVER returns a value, so the host cannot
    // interpret it as a prompt modification.
    api.on("before_prompt_build", async (event: unknown, ctx: unknown) => {
      try {
        const prompt = asText((event as { prompt?: unknown } | undefined)?.prompt);
        if (prompt.trim() === "") {
          return;
        }
        const agentCtx = ctx as { sessionKey?: unknown } & TurnIdContext | undefined;
        const session = asText(agentCtx?.sessionKey);
        await shadowRecall(
          api,
          cfg,
          prompt,
          session === "" ? undefined : session,
          agentCtx,
          pendingRecalls,
        );
      } catch (error) {
        api.logger.warn(`memory-enso: shadow hook contained failure: ${String(error)}`);
      }
      // deliberate: no return value, ever (observation only)
    });

    // Shadow side B — what the flat-file path actually returned, captured
    // from the slot owner's tool calls on the same turn.
    api.on("after_tool_call", async (event: unknown, ctx: unknown) => {
      try {
        const ev = (event ?? {}) as {
          toolName?: unknown;
          params?: unknown;
          result?: unknown;
          durationMs?: unknown;
          isError?: unknown;
          runId?: unknown;
        };
        const toolName = asText(ev.toolName);
        if (!OBSERVED_MEMORY_TOOLS.has(toolName)) {
          return;
        }
        const query = asText((ev.params as { query?: unknown } | undefined)?.query);
        // WP-8 co-keying: the tool context carries the SAME runId as the
        // before_prompt_build side; the after_tool_call EVENT also carries runId
        // as a fallback. Prefer the context, then the event, then text hashing.
        const toolCtx = (ctx ?? {}) as TurnIdContext & { sessionKey?: unknown };
        const idCtx: TurnIdContext = {
          runId: toolCtx.runId ?? ev.runId,
          sessionId: toolCtx.sessionId,
        };
        const { turn, source } = correlationKey(idCtx, query);
        safeAppend(api, cfg, {
          ts: nowISO(),
          kind: "flatfile_result",
          turn,
          turn_src: source,
          ...(query !== "" ? { text: truncateText(query) } : {}),
          flatfile: {
            tool: toolName,
            ...(typeof ev.durationMs === "number" ? { duration_ms: ev.durationMs } : {}),
            ...(typeof ev.isError === "boolean" ? { is_error: ev.isError } : {}),
            summary: summarizeToolResult(ev.result),
          },
          used: "unknown",
        });
      } catch (error) {
        api.logger.warn(`memory-enso: tool observer contained failure: ${String(error)}`);
      }
    });

    // Shadow side C (WP-8 blocker a) — the RECALL-DEF label. llm_output is the
    // only hook carrying both the populated runId (co-key with side A) and the
    // assistant's reply text, so material use is derived here, never earlier.
    // Observation-only: no return value, failures swallowed.
    api.on("llm_output", async (event: unknown) => {
      try {
        observeReplyUse(api, cfg, pendingRecalls, event);
      } catch (error) {
        api.logger.warn(`memory-enso: reply-use observer contained failure: ${String(error)}`);
      }
    });

    // Manual side-by-side check: `enso_recall` runs the same bridge on demand
    // and shows the ranked answer (still logged, still read-only).
    api.registerTool(() => ({
      name: "enso_recall",
      label: "Ensō Recall (shadow)",
      description:
        "Run Ensō structured-memory recall side-by-side with normal memory. Read-only; results are also written to the shadow divergence log.",
      parameters: Type.Object({
        query: Type.String({ description: "Recall query" }),
      }),
      async execute(_toolCallId: string, params: unknown) {
        const query = asText((params as { query?: unknown } | undefined)?.query);
        if (query.trim() === "") {
          return {
            content: [{ type: "text", text: "enso_recall: query is required." }],
            details: { count: 0, error: "query is required" },
          };
        }
        const record = await shadowRecall(api, cfg, query, undefined, undefined);
        if (record.kind === "enso_error") {
          return {
            content: [
              { type: "text", text: `Ensō recall unavailable: ${record.error ?? "unknown error"}` },
            ],
            details: { count: 0, error: record.error ?? "unknown error" },
          };
        }
        const lines = (record.enso?.results ?? []).map(
          (r, i) =>
            `${i + 1}. ${r.id} (specificity ${r.specificity.toFixed(2)}, strength ${r.strength.toFixed(2)})`,
        );
        const header = `Ensō recall (${record.enso?.mode ?? "?"} mode, ${record.enso?.elapsed_ms ?? "?"}ms core, ${record.enso?.spawn_ms ?? "?"}ms total):`;
        return {
          content: [
            {
              type: "text",
              text: lines.length > 0 ? `${header}\n${lines.join("\n")}` : `${header}\n(no results)`,
            },
          ],
          details: { count: lines.length, mode: record.enso?.mode ?? "unknown" },
        };
      },
    }));
  },
});
