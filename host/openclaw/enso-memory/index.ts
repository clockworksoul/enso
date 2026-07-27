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
import { appendShadowRecord, truncateText, turnKey, type ShadowRecord } from "./shadow-log.js";
import { recordOutcome } from "./status.js";

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
): Promise<ShadowRecord> {
  const ts = nowISO();
  const turn = turnKey(queryText);
  const outcome = await runEnsoRecall(cfg, queryText);
  if (!outcome.ok) {
    const record: ShadowRecord = {
      ts,
      kind: "enso_error",
      turn,
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
  return record;
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

    // Shadow side A — Ensō's answer for the same turn the slot owner serves.
    // Observation-only: the handler NEVER returns a value, so the host cannot
    // interpret it as a prompt modification.
    api.on("before_prompt_build", async (event: unknown, ctx: unknown) => {
      try {
        const prompt = asText((event as { prompt?: unknown } | undefined)?.prompt);
        if (prompt.trim() === "") {
          return;
        }
        const session = asText((ctx as { sessionKey?: unknown } | undefined)?.sessionKey);
        await shadowRecall(api, cfg, prompt, session === "" ? undefined : session);
      } catch (error) {
        api.logger.warn(`memory-enso: shadow hook contained failure: ${String(error)}`);
      }
      // deliberate: no return value, ever (observation only)
    });

    // Shadow side B — what the flat-file path actually returned, captured
    // from the slot owner's tool calls on the same turn.
    api.on("after_tool_call", async (event: unknown) => {
      try {
        const ev = (event ?? {}) as {
          toolName?: unknown;
          params?: unknown;
          result?: unknown;
          durationMs?: unknown;
          isError?: unknown;
        };
        const toolName = asText(ev.toolName);
        if (!OBSERVED_MEMORY_TOOLS.has(toolName)) {
          return;
        }
        const query = asText((ev.params as { query?: unknown } | undefined)?.query);
        safeAppend(api, cfg, {
          ts: nowISO(),
          kind: "flatfile_result",
          turn: turnKey(query),
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
        const record = await shadowRecall(api, cfg, query, undefined);
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
