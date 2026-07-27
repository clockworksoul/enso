// The process bridge to the Ensō Go core (WP-7, Matt's signed call: one
// spawn of the enso-recall binary per call, no long-lived sidecar until a
// real latency case is logged — the elapsed_ms field in every record is that
// datum).
//
// PORT-INV: no recall/ranking/supersession logic lives here. This file only
// spawns, bounds, and parses. Any failure — missing binary, timeout, bad
// JSON, unknown schema version — returns a typed failure the caller LOGS and
// swallows; the shadow path must never affect the turn.

import { execFile } from "node:child_process";
import type { MemoryEnsoConfig } from "./config.js";

/** Schema version of enso-recall stdout this bridge understands. */
export const SUPPORTED_SCHEMA_VERSION = 1;

export type EnsoRecallResult = {
  id: string;
  type: string;
  content: string;
  specificity: number;
  strength: number;
};

export type EnsoRecallOutput = {
  version: number;
  query: string;
  as_of: string;
  mode: string;
  degraded: string;
  elapsed_ms: number;
  corpus_entries: number;
  results: EnsoRecallResult[];
};

export type BridgeOutcome =
  | { ok: true; output: EnsoRecallOutput; spawnMs: number }
  | { ok: false; error: string; spawnMs: number };

/** Runs one enso-recall invocation, hard-bounded by cfg.timeoutMs. */
export function runEnsoRecall(cfg: MemoryEnsoConfig, query: string): Promise<BridgeOutcome> {
  const started = Date.now();
  const args = ["-root", cfg.corpusRoot, "-query", query, "-k", String(cfg.topK)];
  return new Promise((resolve) => {
    execFile(
      cfg.ensoBinary,
      args,
      { timeout: cfg.timeoutMs, maxBuffer: 4 * 1024 * 1024, windowsHide: true },
      (error, stdout, stderr) => {
        const spawnMs = Date.now() - started;
        if (error) {
          // Node's execFile error.message ALREADY embeds stderr for a
          // nonzero-exit failure ("Command failed: <cmd>\n<stderr>"), so
          // appending stderr again below double-prints the same text.
          // Only append stderr separately when it's NOT already contained
          // in error.message (e.g. a spawn-level failure like ENOENT,
          // where stderr is empty anyway and this is a no-op).
          const trimmedStderr = stderr.trim();
          const alreadyIncluded = trimmedStderr !== "" && error.message.includes(trimmedStderr);
          const detail = trimmedStderr !== "" && !alreadyIncluded ? `: ${trimmedStderr}` : "";
          resolve({ ok: false, error: `${error.message}${detail}`, spawnMs });
          return;
        }
        let parsed: unknown;
        try {
          parsed = JSON.parse(stdout);
        } catch {
          resolve({ ok: false, error: "enso-recall emitted invalid JSON", spawnMs });
          return;
        }
        const out = parsed as EnsoRecallOutput;
        if (out.version !== SUPPORTED_SCHEMA_VERSION) {
          resolve({
            ok: false,
            error: `unsupported enso-recall schema version ${String(out.version)} (bridge supports ${SUPPORTED_SCHEMA_VERSION})`,
            spawnMs,
          });
          return;
        }
        if (!Array.isArray(out.results)) {
          resolve({ ok: false, error: "enso-recall output missing results array", spawnMs });
          return;
        }
        resolve({ ok: true, output: out, spawnMs });
      },
    );
  });
}
