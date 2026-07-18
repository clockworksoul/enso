/**
 * Memory Ensō plugin entry tests — the WP-7 fail-safe DoD box lives here:
 * a bridge failure inside the shadow hook is contained (logged as an
 * enso_error record, nothing thrown, nothing returned), so a broken shadow
 * can never break a turn.
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import plugin, { summarizeToolResult } from "./index.js";
import type { ShadowRecord } from "./shadow-log.js";

type HookHandler = (event: unknown, ctx: unknown) => Promise<unknown>;

const tmpDirs: string[] = [];

function tmpDir(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "enso-plugin-test-"));
  tmpDirs.push(dir);
  return dir;
}

afterEach(() => {
  for (const dir of tmpDirs.splice(0)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

/**
 * Registers the plugin against a minimal fake api and captures hooks + tools.
 * The published SDK does not export the monorepo's plugin-test-api helper, so
 * this fake covers exactly the surface the plugin uses (config, logger, on,
 * registerTool) — anything more would be untested fiction anyway.
 */
function registerPlugin(config: Record<string, unknown>) {
  const hooks = new Map<string, HookHandler>();
  const tools: Array<
    () => { name: string; execute: (id: string, params: unknown) => Promise<unknown> }
  > = [];
  const warnings: string[] = [];
  const api = {
    id: "memory-enso-test",
    config,
    logger: {
      info() {},
      warn(message: string) {
        warnings.push(message);
      },
      error() {},
      debug() {},
    },
    on(name: string, handler: HookHandler) {
      hooks.set(name, handler);
    },
    registerTool(factory: unknown) {
      tools.push(factory as (typeof tools)[number]);
    },
  } as never;
  plugin.register(api);
  return { hooks, tools, warnings };
}

function readRecords(shadowLogDir: string): ShadowRecord[] {
  if (!fs.existsSync(shadowLogDir)) {
    return [];
  }
  const out: ShadowRecord[] = [];
  for (const f of fs.readdirSync(shadowLogDir)) {
    const lines = fs.readFileSync(path.join(shadowLogDir, f), "utf-8").trim().split("\n");
    for (const line of lines) {
      if (line !== "") {
        out.push(JSON.parse(line) as ShadowRecord);
      }
    }
  }
  return out;
}

describe("memory-enso plugin entry", () => {
  it("registers both observation hooks and the manual tool", () => {
    const { hooks, tools } = registerPlugin({ shadowLogDir: tmpDir() });
    expect(hooks.has("before_prompt_build")).toBe(true);
    expect(hooks.has("after_tool_call")).toBe(true);
    expect(tools).toHaveLength(1);
    expect(tools[0]?.().name).toBe("enso_recall");
  });

  it("registers nothing when disabled", () => {
    const { hooks, tools } = registerPlugin({ enabled: false });
    expect(hooks.size).toBe(0);
    expect(tools).toHaveLength(0);
  });

  it("CONTAINS a bridge failure: hook returns nothing, logs enso_error, does not throw", async () => {
    const shadowLogDir = tmpDir();
    const { hooks } = registerPlugin({
      shadowLogDir,
      ensoBinary: "/nonexistent/enso-recall",
      corpusRoot: tmpDir(),
      timeoutMs: 500,
    });
    const handler = hooks.get("before_prompt_build");
    expect(handler).toBeDefined();

    const returned = await handler?.(
      { prompt: "what happened with granola?" },
      {
        sessionKey: "s1",
      },
    );
    // Observation-only: NEVER a modification, even on failure.
    expect(returned).toBeUndefined();

    const records = readRecords(shadowLogDir);
    expect(records).toHaveLength(1);
    expect(records[0]?.kind).toBe("enso_error");
    expect(records[0]?.session).toBe("s1");
    expect(records[0]?.used).toBe("unknown");
  });

  it("records flat-file results for observed memory tools only", async () => {
    const shadowLogDir = tmpDir();
    const { hooks } = registerPlugin({ shadowLogDir });
    const handler = hooks.get("after_tool_call");

    await handler?.(
      {
        toolName: "memory_search",
        params: { query: "granola" },
        result: { content: [{ type: "text", text: "found 2 notes" }] },
        durationMs: 12,
        isError: false,
      },
      {},
    );
    await handler?.({ toolName: "web_search", params: { query: "granola" } }, {});

    const records = readRecords(shadowLogDir);
    expect(records).toHaveLength(1);
    expect(records[0]?.kind).toBe("flatfile_result");
    expect(records[0]?.flatfile?.tool).toBe("memory_search");
    expect(records[0]?.flatfile?.summary).toContain("found 2 notes");
  });

  it("skips empty prompts without spawning or logging", async () => {
    const shadowLogDir = tmpDir();
    const { hooks } = registerPlugin({ shadowLogDir, ensoBinary: "/nonexistent/enso-recall" });
    await hooks.get("before_prompt_build")?.({ prompt: "   " }, {});
    expect(readRecords(shadowLogDir)).toHaveLength(0);
  });

  it("manual tool reports unavailability instead of throwing", async () => {
    const { tools } = registerPlugin({
      shadowLogDir: tmpDir(),
      ensoBinary: "/nonexistent/enso-recall",
      timeoutMs: 500,
    });
    const tool = tools[0]?.();
    const result = (await tool?.execute("t1", { query: "granola" })) as {
      content: Array<{ text: string }>;
    };
    expect(result.content[0]?.text).toContain("unavailable");
  });
});

describe("summarizeToolResult", () => {
  it("bounds and flattens arbitrary results", () => {
    const s = summarizeToolResult({ a: "x".repeat(2000), b: "line\nbreak" });
    expect(s.length).toBeLessThanOrEqual(500);
    expect(s).not.toContain("\n");
  });
});
