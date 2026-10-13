// Memory Ensō plugin configuration: defensive parsing with boring defaults.
// The plugin must behave sanely on an empty config object — shadow mode
// does not inject context unless live recall is explicitly opted into. A
// misconfiguration must never break a turn.

import os from "node:os";
import path from "node:path";

export type MemoryEnsoConfig = {
  enabled: boolean;
  liveRecall: boolean;
  liveRecallMinScore: number;
  corpusRoot: string;
  ensoBinary: string;
  markRecalledBinary: string;
  topK: number;
  timeoutMs: number;
  shadowLogDir: string;
};

export const DEFAULT_TOP_K = 5;
export const DEFAULT_TIMEOUT_MS = 4000;
// Blended scores are [0,1]; 0.5 exceeds the 0.4 decay-only ceiling, so
// freshness alone cannot qualify a memory for live context by default.
export const DEFAULT_LIVE_RECALL_MIN_SCORE = 0.5;

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
}

function asString(value: unknown, fallback: string): string {
  return typeof value === "string" && value.trim() !== "" ? value : fallback;
}

function asBoundedInt(value: unknown, fallback: number, min: number, max: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return fallback;
  }
  const n = Math.floor(value);
  return n < min || n > max ? fallback : n;
}

function expandHome(p: string): string {
  return p.startsWith("~/") || p === "~" ? path.join(os.homedir(), p.slice(1)) : p;
}

export function resolveMemoryEnsoConfig(raw: unknown): MemoryEnsoConfig {
  const cfg = asRecord(raw);
  const corpusRoot = expandHome(
    asString(cfg.corpusRoot, path.join(os.homedir(), ".openclaw", "workspace")),
  );
  return {
    enabled: cfg.enabled !== false,
    liveRecall: cfg.liveRecall === true,
    liveRecallMinScore:
      typeof cfg.liveRecallMinScore === "number" &&
      Number.isFinite(cfg.liveRecallMinScore) &&
      cfg.liveRecallMinScore >= 0 && cfg.liveRecallMinScore <= 1
        ? cfg.liveRecallMinScore
        : DEFAULT_LIVE_RECALL_MIN_SCORE,
    corpusRoot,
    ensoBinary: expandHome(asString(cfg.ensoBinary, "enso-recall")),
    markRecalledBinary: expandHome(asString(cfg.markRecalledBinary, "enso-mark-recalled")),
    topK: asBoundedInt(cfg.topK, DEFAULT_TOP_K, 1, 50),
    timeoutMs: asBoundedInt(cfg.timeoutMs, DEFAULT_TIMEOUT_MS, 250, 60_000),
    shadowLogDir: expandHome(asString(cfg.shadowLogDir, path.join(corpusRoot, ".enso", "shadow"))),
  };
}
