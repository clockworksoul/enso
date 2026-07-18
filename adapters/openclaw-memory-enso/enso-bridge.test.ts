// Bridge tests run against FAKE enso-recall binaries (shell scripts) so every
// failure mode the fail-safe rule must contain is exercised: healthy output,
// invalid JSON, unknown schema version, nonzero exit, and timeout.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import type { MemoryEnsoConfig } from "./config.js";
import { runEnsoRecall, SUPPORTED_SCHEMA_VERSION } from "./enso-bridge.js";

const tmpDirs: string[] = [];

function fakeBinary(script: string): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "enso-bridge-test-"));
  tmpDirs.push(dir);
  const file = path.join(dir, "enso-recall");
  fs.writeFileSync(file, `#!/bin/sh\n${script}\n`, { mode: 0o755 });
  return file;
}

function cfgWith(binary: string, timeoutMs = 2000): MemoryEnsoConfig {
  return {
    enabled: true,
    corpusRoot: "/tmp/does-not-matter",
    ensoBinary: binary,
    topK: 5,
    timeoutMs,
    shadowLogDir: "/tmp/does-not-matter-either",
  };
}

afterEach(() => {
  for (const dir of tmpDirs.splice(0)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

const healthyJSON = JSON.stringify({
  version: SUPPORTED_SCHEMA_VERSION,
  query: "q",
  as_of: "2026-07-18T00:00:00Z",
  mode: "lexical",
  degraded: "",
  elapsed_ms: 3,
  corpus_entries: 2,
  results: [{ id: "mem:2026-07-04-x", type: "Fact", content: "c", specificity: 1, strength: 0.5 }],
});

describe("enso-bridge", () => {
  it("parses healthy output", async () => {
    const bin = fakeBinary(`echo '${healthyJSON}'`);
    const out = await runEnsoRecall(cfgWith(bin), "q");
    expect(out.ok).toBe(true);
    if (out.ok) {
      expect(out.output.results[0]?.id).toBe("mem:2026-07-04-x");
      expect(out.output.mode).toBe("lexical");
    }
  });

  it("fails typed on invalid JSON", async () => {
    const bin = fakeBinary(`echo 'not json'`);
    const out = await runEnsoRecall(cfgWith(bin), "q");
    expect(out.ok).toBe(false);
    if (!out.ok) {
      expect(out.error).toContain("invalid JSON");
    }
  });

  it("refuses an unknown schema version", async () => {
    const bumped = healthyJSON.replace(`"version":${SUPPORTED_SCHEMA_VERSION}`, '"version":99');
    const bin = fakeBinary(`echo '${bumped}'`);
    const out = await runEnsoRecall(cfgWith(bin), "q");
    expect(out.ok).toBe(false);
    if (!out.ok) {
      expect(out.error).toContain("schema version");
    }
  });

  it("surfaces stderr on nonzero exit", async () => {
    const bin = fakeBinary(`echo 'enso-recall: boom' >&2; exit 1`);
    const out = await runEnsoRecall(cfgWith(bin), "q");
    expect(out.ok).toBe(false);
    if (!out.ok) {
      expect(out.error).toContain("boom");
    }
  });

  it("times out a hung binary within the configured deadline", async () => {
    const bin = fakeBinary(`sleep 30`);
    const started = Date.now();
    const out = await runEnsoRecall(cfgWith(bin, 300), "q");
    expect(out.ok).toBe(false);
    expect(Date.now() - started).toBeLessThan(5000);
  });

  it("fails typed when the binary does not exist", async () => {
    const out = await runEnsoRecall(cfgWith("/nonexistent/enso-recall"), "q");
    expect(out.ok).toBe(false);
  });
});
