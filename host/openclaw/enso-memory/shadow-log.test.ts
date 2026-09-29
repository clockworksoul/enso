// Shadow-log tests: JSONL bucketing by UTC day, stable turn correlation, and
// bounded text retention.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  appendShadowRecord,
  correlationKey,
  MAX_LOGGED_TEXT_CHARS,
  truncateText,
  turnKey,
  type ShadowRecord,
} from "./shadow-log.js";

const tmpDirs: string[] = [];

function tmpDir(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "enso-shadow-test-"));
  tmpDirs.push(dir);
  return dir;
}

afterEach(() => {
  for (const dir of tmpDirs.splice(0)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

function record(ts: string, kind: ShadowRecord["kind"]): ShadowRecord {
  return { ts, kind, turn: turnKey("q"), used: "unknown" };
}

describe("shadow-log", () => {
  it("appends JSONL bucketed by UTC day, creating the directory", () => {
    const dir = path.join(tmpDir(), "nested", "shadow");
    appendShadowRecord(dir, record("2026-07-18T10:00:00.000Z", "enso_recall"));
    appendShadowRecord(dir, record("2026-07-18T11:00:00.000Z", "flatfile_result"));
    appendShadowRecord(dir, record("2026-07-19T09:00:00.000Z", "enso_error"));

    const day1 = fs.readFileSync(path.join(dir, "2026-07-18.jsonl"), "utf-8").trim().split("\n");
    expect(day1).toHaveLength(2);
    const parsed = JSON.parse(day1[0] ?? "") as ShadowRecord;
    expect(parsed.kind).toBe("enso_recall");
    expect(parsed.used).toBe("unknown");
    expect(fs.existsSync(path.join(dir, "2026-07-19.jsonl"))).toBe(true);
  });

  it("turnKey is stable and prompt-sensitive", () => {
    expect(turnKey("same prompt")).toBe(turnKey("same prompt"));
    expect(turnKey("same prompt")).not.toBe(turnKey("different prompt"));
    expect(turnKey("x")).toHaveLength(16);
  });

  it("truncateText bounds retained text", () => {
    const long = "a".repeat(MAX_LOGGED_TEXT_CHARS * 2);
    expect(truncateText(long)).toHaveLength(MAX_LOGGED_TEXT_CHARS);
    expect(truncateText("short")).toBe("short");
  });
});

describe("correlationKey (WP-8 co-keying)", () => {
  it("prefers runId over both sessionId and the text fallback", () => {
    const key = correlationKey({ runId: "run-1", sessionId: "sess-1" }, "some prompt");
    expect(key).toEqual({ turn: "run-1", source: "runId" });
  });

  it("THE FIX: the two hooks pair when they share a runId but hash different text", () => {
    // This is exactly the 2026-09 unpairability failure: the Ensō side hashes the
    // whole prompt, the flat-file side hashes just the tool query. With a shared
    // runId both now produce the SAME turn key.
    const ensoSide = correlationKey({ runId: "run-42" }, "long assembled prompt with lots of context");
    const flatSide = correlationKey({ runId: "run-42" }, "granola");
    expect(ensoSide.turn).toBe(flatSide.turn);
    expect(ensoSide.turn).toBe("run-42");
    // ...whereas the legacy text hashes would NOT have matched.
    expect(turnKey("long assembled prompt with lots of context")).not.toBe(turnKey("granola"));
  });

  it("falls back to sessionId when runId is absent", () => {
    expect(correlationKey({ sessionId: "sess-9" }, "q")).toEqual({
      turn: "sess-9",
      source: "sessionId",
    });
    expect(correlationKey({ runId: "   ", sessionId: "sess-9" }, "q").source).toBe("sessionId");
  });

  it("degrades to a text hash when the host supplies no id (legacy, unpairable)", () => {
    const key = correlationKey(undefined, "q");
    expect(key.source).toBe("text");
    expect(key.turn).toBe(turnKey("q"));
    expect(correlationKey({}, "q").source).toBe("text");
  });
});
