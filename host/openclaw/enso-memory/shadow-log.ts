// The divergence log: one JSONL record per observed event, bucketed by UTC
// day. This file is the WP-7 deliverable the future slot-takeover gate reads
// — it must capture BOTH sides (what Ensō said, what flat-file search said)
// with enough context to label misses later, and it must never throw into
// the hook path (append failures are reported to the caller, who logs and
// moves on).

import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

/** How much raw query/prompt text a record retains (context for labeling). */
export const MAX_LOGGED_TEXT_CHARS = 500;

/**
 * Where the `turn` correlation key came from, so an analysis pass can tell a
 * host-authoritative pairing (`runId`/`sessionId`, both hooks see the SAME
 * per-turn id) apart from the legacy text-hash fallback (the two hooks hash
 * DIFFERENT strings — prompt vs. tool query — so they never coincide). This is
 * the WP-8 unpairability fix (see docs/2026-09-24-wp8-shadow-corpus-first-analysis.md,
 * finding #2): the 2026-09 corpus had 0/820∩53 turn overlap precisely because
 * both sides fell back to `text` hashing with no shared id ever threaded in.
 */
export type TurnKeySource = "runId" | "sessionId" | "text";

/**
 * Per-memory material-use verdict for a `reply_use` record. One entry per id
 * Ensō surfaced on this turn, labeling whether its content was materially used
 * in the assistant's actual reply. See material-use.ts for the matcher.
 */
export type ReplyUseVerdict = {
  id: string;
  used: "yes" | "no";
  /** Longest distinctive shared-token run length (0 for "no"). */
  score: number;
  /** The matched token run, for human labeling (empty for "no"). */
  evidence?: string;
};

export type ShadowRecord = {
  /** RFC3339 UTC timestamp of the observation. */
  ts: string;
  /** Which observer wrote this: enso shadow recall, flat-file result, reply-use label, or error. */
  kind: "enso_recall" | "flatfile_result" | "enso_error" | "reply_use";
  /**
   * Correlates records from the same turn. Preferred value is the
   * host-authoritative `runId` (both `before_prompt_build` and `after_tool_call`
   * see the SAME one), then `sessionId`, degrading to a sha256[:16] hash of the
   * observed text only when the host supplied no id. `turn_src` records which.
   */
  turn: string;
  /** Provenance of `turn`: pairable (runId/sessionId) vs legacy text hash. */
  turn_src?: TurnKeySource;
  /** Session identity when the host exposed one. */
  session?: string;
  /** Truncated raw text (query/prompt) for human labeling. */
  text?: string;
  /** Ensō side: ranked ids + scores + pipeline mode + latencies. */
  enso?: {
    mode: string;
    degraded?: string;
    elapsed_ms: number;
    spawn_ms: number;
    corpus_entries: number;
    results: Array<{ id: string; specificity: number; strength: number }>;
  };
  /** Flat-file side: a bounded summary of what memory_search returned. */
  flatfile?: {
    tool: string;
    duration_ms?: number;
    is_error?: boolean;
    summary: string;
  };
  /** Bridge/observer failure detail for kind == enso_error. */
  error?: string;
  /**
   * Reply-use side (kind == reply_use): per-id material-use verdicts for the
   * memories Ensō surfaced on this turn, derived from the `llm_output` reply
   * text. Present only on reply_use records. See material-use.ts.
   */
  reply_use?: ReplyUseVerdict[];
  /**
   * RECALL-DEF signal: whether a surfaced memory was materially used in the
   * reply. WP-7 wrote "unknown" everywhere (no reply-text seam yet). WP-8
   * blocker (a) adds the `reply_use` record kind, whose aggregate verdict is
   * "yes" if ANY surfaced id matched, else "no"; recall/flatfile records stay
   * "unknown" (they are written before the reply exists) and are joined to the
   * reply_use record on `turn` (runId) by the analysis pass.
   */
  used: "unknown" | "yes" | "no";
};

export function turnKey(promptText: string): string {
  return createHash("sha256").update(promptText).digest("hex").slice(0, 16);
}

/** Minimal shape shared by both hook contexts that carries per-turn ids. */
export type TurnIdContext = {
  runId?: unknown;
  sessionId?: unknown;
};

function nonEmptyString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

/**
 * The WP-8 co-keying fix. Both `before_prompt_build` (PluginHookAgentContext)
 * and `after_tool_call` (PluginHookToolContext) expose the SAME host-authoritative
 * `runId` and `sessionId` for a given turn — so keying both sides on `runId`
 * makes their records pairable, which text hashing structurally could not do.
 * Falls back to `sessionId`, then to a hash of `fallbackText`, so a host that
 * supplies neither id still produces a (legacy, unpairable) record instead of
 * crashing. Returns the key AND its provenance so analysis can trust or discount
 * the pairing.
 */
export function correlationKey(
  ctx: TurnIdContext | undefined,
  fallbackText: string,
): { turn: string; source: TurnKeySource } {
  const runId = nonEmptyString(ctx?.runId);
  if (runId !== undefined) {
    return { turn: runId, source: "runId" };
  }
  const sessionId = nonEmptyString(ctx?.sessionId);
  if (sessionId !== undefined) {
    return { turn: sessionId, source: "sessionId" };
  }
  return { turn: turnKey(fallbackText), source: "text" };
}

export function truncateText(s: string): string {
  return s.length <= MAX_LOGGED_TEXT_CHARS ? s : s.slice(0, MAX_LOGGED_TEXT_CHARS);
}

/** Appends one record to <dir>/YYYY-MM-DD.jsonl, creating the dir on demand. */
export function appendShadowRecord(dir: string, record: ShadowRecord): void {
  fs.mkdirSync(dir, { recursive: true });
  const day = record.ts.slice(0, 10);
  const file = path.join(dir, `${day}.jsonl`);
  fs.appendFileSync(file, `${JSON.stringify(record)}\n`, "utf-8");
}
