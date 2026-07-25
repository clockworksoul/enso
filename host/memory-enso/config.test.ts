// Memory Ensō config tests: defaults are sane on empty config, bounds clamp
// to defaults, and the manifest schema accepts what the runtime parser reads.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { DEFAULT_TIMEOUT_MS, DEFAULT_TOP_K, resolveMemoryEnsoConfig } from "./config.js";

const manifest = JSON.parse(
  fs.readFileSync(new URL("./openclaw.plugin.json", import.meta.url), "utf-8"),
) as { configSchema: Record<string, unknown> };

/**
 * Minimal standalone JSON Schema check for this file's own tests. The
 * monorepo's `openclaw/plugin-sdk/json-schema-runtime` validator is not part
 * of the public package export map, so this is a small local substitute
 * scoped to exactly the shape memory-enso's configSchema uses (object,
 * additionalProperties, properties w/ type/minimum/maximum). Not a general
 * JSON Schema validator — do not reuse elsewhere.
 */
function validatesAgainstConfigSchema(schema: Record<string, unknown>, value: Record<string, unknown>): boolean {
  const props = schema.properties as Record<string, { type: string; minimum?: number; maximum?: number }>;
  if (schema.additionalProperties === false) {
    for (const key of Object.keys(value)) {
      if (!(key in props)) return false;
    }
  }
  for (const [key, def] of Object.entries(props)) {
    if (!(key in value)) continue;
    const v = value[key];
    if (def.type === "string" && typeof v !== "string") return false;
    if (def.type === "boolean" && typeof v !== "boolean") return false;
    if (def.type === "integer") {
      if (typeof v !== "number" || !Number.isInteger(v)) return false;
      if (def.minimum !== undefined && v < def.minimum) return false;
      if (def.maximum !== undefined && v > def.maximum) return false;
    }
  }
  return true;
}

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
    expect(validatesAgainstConfigSchema(manifest.configSchema, value)).toBe(true);
    const cfg = resolveMemoryEnsoConfig(value);
    expect(cfg.topK).toBe(8);
    expect(cfg.shadowLogDir).toBe("/tmp/shadow");
  });
});
