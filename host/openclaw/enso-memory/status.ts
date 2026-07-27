// Plugin self-reported health status (2026-07-27, post-incident): a single
// small JSON file the plugin overwrites on EVERY recall attempt, success or
// failure. This exists because the shadow JSONL log (append-only, growing,
// one record per turn) is the wrong shape for "is Ensō healthy right now?" —
// answering that from the log means tailing and computing a heuristic over
// however many records happened to land recently. A status file the plugin
// itself maintains turns "report the error through the plugin" into
// something literal: the plugin is the authoritative source of its own
// health, and an external checker just reads a few bytes instead of parsing
// a log.
//
// Deliberately NOT a new alerting channel (no webhook, no direct message,
// no phone-home): this stays inside WP-7's rule that the plugin only spawns,
// parses, and logs/records facts. Turning a fact into an alert that reaches
// Matt is external-checker work (scripts/enso-shadow-check.sh), same
// division of responsibility the shadow JSONL already had.

import fs from "node:fs";
import path from "node:path";

export type PluginStatus = {
  /** "ok" after any successful recall; "degraded" after a failure. */
  state: "ok" | "degraded";
  /** Consecutive failures since the last success (resets to 0 on success). */
  consecutiveErrors: number;
  /** RFC3339 UTC timestamp of the most recent successful recall, if any. */
  lastSuccessAt: string | null;
  /** RFC3339 UTC timestamp of the most recent failed recall, if any. */
  lastErrorAt: string | null;
  /** Error detail from the most recent failure, if any. */
  lastError: string | null;
};

export const STATUS_FILE_NAME = "status.json";

function statusPath(shadowLogDir: string): string {
  return path.join(shadowLogDir, STATUS_FILE_NAME);
}

/**
 * Reads the current status, defaulting to a fresh "ok, never observed"
 * state if the file doesn't exist yet (first run) or is unreadable/corrupt
 * (never let a bad status file break the write path below).
 */
export function readStatus(shadowLogDir: string): PluginStatus {
  try {
    const raw = fs.readFileSync(statusPath(shadowLogDir), "utf-8");
    const parsed = JSON.parse(raw) as Partial<PluginStatus>;
    return {
      state: parsed.state === "degraded" ? "degraded" : "ok",
      consecutiveErrors:
        typeof parsed.consecutiveErrors === "number" && parsed.consecutiveErrors >= 0
          ? parsed.consecutiveErrors
          : 0,
      lastSuccessAt: typeof parsed.lastSuccessAt === "string" ? parsed.lastSuccessAt : null,
      lastErrorAt: typeof parsed.lastErrorAt === "string" ? parsed.lastErrorAt : null,
      lastError: typeof parsed.lastError === "string" ? parsed.lastError : null,
    };
  } catch {
    return { state: "ok", consecutiveErrors: 0, lastSuccessAt: null, lastErrorAt: null, lastError: null };
  }
}

/**
 * Records one recall outcome, overwriting the status file (not appending —
 * this file only ever describes the CURRENT state, unlike the JSONL log).
 * Atomic write (temp file + rename) so a concurrent reader never observes a
 * half-written file. Never throws: a status-file write failure is exactly
 * the kind of thing that must not escalate into a broken turn, matching the
 * shadow-log's own fail-safe contract; callers should wrap this the same
 * way they wrap appendShadowRecord.
 */
export function recordOutcome(shadowLogDir: string, ts: string, ok: boolean, error?: string): void {
  const prev = readStatus(shadowLogDir);
  const next: PluginStatus = ok
    ? { state: "ok", consecutiveErrors: 0, lastSuccessAt: ts, lastErrorAt: prev.lastErrorAt, lastError: prev.lastError }
    : {
        state: "degraded",
        consecutiveErrors: prev.consecutiveErrors + 1,
        lastSuccessAt: prev.lastSuccessAt,
        lastErrorAt: ts,
        lastError: error ?? "unknown error",
      };

  fs.mkdirSync(shadowLogDir, { recursive: true });
  const target = statusPath(shadowLogDir);
  const tmp = `${target}.${process.pid}.tmp`;
  fs.writeFileSync(tmp, JSON.stringify(next), "utf-8");
  fs.renameSync(tmp, target);
}
