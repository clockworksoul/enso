// Memory Ensō config tests: defaults are sane on empty config, bounds clamp
// to defaults, and the manifest schema accepts what the runtime parser reads.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  type JsonSchemaObject,
  validateJsonSchemaValue,
} from "openclaw/plugin-sdk/json-schema-runtime";
import { describe, expect, it } from "vitest";
import { DEFAULT_TIMEOUT_MS, DEFAULT_TOP_K, resolveMemoryEnsoConfig } from "./config.js";

const manifest = JSON.parse(
  fs.readFileSync(new URL("./openclaw.plugin.json", import.meta.url), "utf-8"),
) as { configSchema: JsonSchemaObject };

describe("memory-enso config", () => {
  it("defaults sanely on an empty config", () => {
    const cfg = resolveMemoryEnsoConfig({});
    expect(cfg.enabled).toBe(true);
    expect(cfg.corpusRoot).toBe(path.join(os.homedir(), ".openclaw", "workspace"));
    expect(cfg.ensoBinary).toBe("enso-recall");
    expect(cfg.topK).toBe(DEFAULT_TOP_K);
    expect(cfg.timeoutMs).toBe(DEFAULT_TIMEOUT_MS);
    expect(cfg.shadowLogDir).toBe(path.join(cfg.corpusRoot, ".enso", "shadow"));
  });

  it("clamps out-of-bounds numbers back to defaults", () => {
    const cfg = resolveMemoryEnsoConfig({ topK: 0, timeoutMs: 10 });
    expect(cfg.topK).toBe(DEFAULT_TOP_K);
    expect(cfg.timeoutMs).toBe(DEFAULT_TIMEOUT_MS);
  });

  it("expands ~ in configured paths", () => {
    const cfg = resolveMemoryEnsoConfig({ corpusRoot: "~/ws" });
    expect(cfg.corpusRoot).toBe(path.join(os.homedir(), "ws"));
  });

  it("manifest schema accepts a full config the runtime parser also accepts", () => {
    const value = {
      enabled: true,
      corpusRoot: "/tmp/ws",
      ensoBinary: "/usr/local/bin/enso-recall",
      topK: 8,
      timeoutMs: 2500,
      shadowLogDir: "/tmp/shadow",
    };
    const result = validateJsonSchemaValue({
      schema: manifest.configSchema,
      cacheKey: "memory-enso-config-test",
      value,
    });
    expect(result.ok).toBe(true);
    const cfg = resolveMemoryEnsoConfig(value);
    expect(cfg.topK).toBe(8);
    expect(cfg.shadowLogDir).toBe("/tmp/shadow");
  });
});
