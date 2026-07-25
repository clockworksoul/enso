// Shadow-log tests: JSONL bucketing by UTC day, stable turn correlation, and
// bounded text retention.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  appendShadowRecord,
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
