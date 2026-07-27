// status.ts tests: overwrite semantics, consecutive-error counting, and the
// "never throw" contract a status-file write failure must uphold.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { readStatus, recordOutcome, STATUS_FILE_NAME } from "./status.js";

const tmpDirs: string[] = [];

function tmpDir(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "enso-status-test-"));
  tmpDirs.push(dir);
  return dir;
}

afterEach(() => {
  for (const dir of tmpDirs.splice(0)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

describe("status", () => {
  it("readStatus defaults to ok/never-observed when no file exists yet", () => {
    const status = readStatus(tmpDir());
    expect(status).toEqual({
      state: "ok",
      consecutiveErrors: 0,
      lastSuccessAt: null,
      lastErrorAt: null,
      lastError: null,
    });
  });

  it("recordOutcome writes ok state on success and resets consecutiveErrors", () => {
    const dir = tmpDir();
    recordOutcome(dir, "2026-07-27T10:00:00.000Z", false, "boom");
    recordOutcome(dir, "2026-07-27T10:01:00.000Z", false, "boom again");
    recordOutcome(dir, "2026-07-27T10:02:00.000Z", true);

    const status = readStatus(dir);
    expect(status.state).toBe("ok");
    expect(status.consecutiveErrors).toBe(0);
    expect(status.lastSuccessAt).toBe("2026-07-27T10:02:00.000Z");
    // lastError/lastErrorAt are historical context, not cleared by a success.
    expect(status.lastErrorAt).toBe("2026-07-27T10:01:00.000Z");
    expect(status.lastError).toBe("boom again");
  });

  it("recordOutcome accumulates consecutiveErrors across repeated failures", () => {
    const dir = tmpDir();
    recordOutcome(dir, "2026-07-27T10:00:00.000Z", false, "e1");
    recordOutcome(dir, "2026-07-27T10:01:00.000Z", false, "e2");
    recordOutcome(dir, "2026-07-27T10:02:00.000Z", false, "e3");

    const status = readStatus(dir);
    expect(status.state).toBe("degraded");
    expect(status.consecutiveErrors).toBe(3);
    expect(status.lastError).toBe("e3");
    expect(status.lastSuccessAt).toBeNull();
  });

  it("overwrites rather than appends: the file never grows past one record", () => {
    const dir = tmpDir();
    for (let i = 0; i < 20; i++) {
      recordOutcome(dir, `2026-07-27T10:${String(i).padStart(2, "0")}:00.000Z`, i % 2 === 0);
    }
    const raw = fs.readFileSync(path.join(dir, STATUS_FILE_NAME), "utf-8");
    // Exactly one JSON object, not JSONL -- parses as a single value and the
    // file has no embedded newlines from repeated appends.
    expect(() => JSON.parse(raw)).not.toThrow();
    expect(raw.includes("\n")).toBe(false);
  });

  it("readStatus tolerates a corrupt/unreadable file by returning the default", () => {
    const dir = tmpDir();
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(path.join(dir, STATUS_FILE_NAME), "{not json", "utf-8");
    const status = readStatus(dir);
    expect(status.state).toBe("ok");
    expect(status.consecutiveErrors).toBe(0);
  });

  it("creates the shadow log directory on demand", () => {
    const dir = path.join(tmpDir(), "nested", "shadow");
    expect(fs.existsSync(dir)).toBe(false);
    recordOutcome(dir, "2026-07-27T10:00:00.000Z", true);
    expect(fs.existsSync(path.join(dir, STATUS_FILE_NAME))).toBe(true);
  });
});
