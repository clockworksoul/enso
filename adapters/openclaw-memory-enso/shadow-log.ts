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

export type ShadowRecord = {
  /** RFC3339 UTC timestamp of the observation. */
  ts: string;
  /** Which observer wrote this: enso shadow recall or flat-file result. */
  kind: "enso_recall" | "flatfile_result" | "enso_error";
  /** Correlates records from the same turn: sha256[:16] of the prompt text. */
  turn: string;
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
   * RECALL-DEF placeholder: whether the surfaced memory was materially used.
   * Always "unknown" in WP-7 — materially-used detection is out of scope and
   * the field exists so the log format does not change when it arrives.
   */
  used: "unknown";
};

export function turnKey(promptText: string): string {
  return createHash("sha256").update(promptText).digest("hex").slice(0, 16);
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
