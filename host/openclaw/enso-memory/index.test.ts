/**
 * Memory Ensō plugin entry tests — the WP-7 fail-safe DoD box lives here:
 * a bridge failure inside the shadow hook is contained (logged as an
 * enso_error record, nothing thrown, nothing returned), so a broken shadow
 * can never break a turn.
 *
 * NOTE (2026-07-25 lift-and-shift): the original monorepo version of this
 * file used `openclaw/plugin-sdk/plugin-test-api`'s `createTestPluginApi`,
 * which is a monorepo-internal path not present in the published `openclaw`
 * package's export map. This standalone copy inlines a tiny fake api object
 * scoped to exactly what memory-enso's `register()` calls (`config`,
 * `logger`, `on`, `registerTool`) — not a general test harness, just enough
 * to keep these behavioral tests portable outside the OpenClaw monorepo.
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import type { OpenClawPluginApi } from "./api.js";
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

/** Registers the plugin against a minimal fake api and captures hooks + tools. */
function registerPlugin(config: Record<string, unknown>) {
  const hooks = new Map<string, HookHandler>();
  const tools: Array<
    () => { name: string; execute: (id: string, params: unknown) => Promise<unknown> }
  > = [];
  const warnings: string[] = [];

  const fakeApi = {
    // NOTE (2026-07-26): the plugin reads api.pluginConfig (scoped config),
    // not api.config (whole-host config) -- this fake mirrors the real SDK
    // shape so a test-harness drift can't mask the same bug that shipped to
    // the real gateway for ~24h (fixed in index.ts the same day).
    config: {},
    pluginConfig: config,
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
    registerTool(factory: never) {
      tools.push(factory as (typeof tools)[number]);
    },
  } as unknown as OpenClawPluginApi;

  plugin.register(fakeApi);
  return { hooks, tools, warnings };
}

function readRecords(shadowLogDir: string): ShadowRecord[] {
  if (!fs.existsSync(shadowLogDir)) {
    return [];
  }
  const out: ShadowRecord[] = [];
  // .jsonl only: status.json (the 2026-07-27 plugin-health file) lives in
  // the same directory now and must not be mistaken for a divergence record.
  for (const f of fs.readdirSync(shadowLogDir).filter((name) => name.endsWith(".jsonl"))) {
    const lines = fs.readFileSync(path.join(shadowLogDir, f), "utf-8").trim().split("\n");
    for (const line of lines) {
      if (line !== "") {
        out.push(JSON.parse(line) as ShadowRecord);
      }
    }
  }
  return out;
}

function readStatusFile(shadowLogDir: string): unknown {
  const p = path.join(shadowLogDir, "status.json");
  if (!fs.existsSync(p)) {
    return undefined;
  }
  return JSON.parse(fs.readFileSync(p, "utf-8"));
}

describe("memory-enso plugin entry", () => {
  it("registers both observation hooks, the reply-use observer, and the manual tool", () => {
    const { hooks, tools } = registerPlugin({ shadowLogDir: tmpDir() });
    expect(hooks.has("before_prompt_build")).toBe(true);
    expect(hooks.has("after_tool_call")).toBe(true);
    expect(hooks.has("llm_output")).toBe(true);
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

  it("WP-8 FIX end-to-end: both hooks emit the SAME turn key for the same runId", async () => {
    // The 2026-09 shadow corpus had 0/820∩53 turn overlap because the two hooks
    // hashed different strings (prompt vs tool query). With runId threaded from
    // both hook contexts, an Ensō-side record and a flat-file record from the
    // same host turn now correlate — the precondition WP-8 divergence labeling
    // needs. (Ensō side uses a bogus binary so it logs an enso_error record
    // without a live recall; the turn key is set before the bridge runs.)
    const shadowLogDir = tmpDir();
    const { hooks } = registerPlugin({
      shadowLogDir,
      ensoBinary: "/nonexistent/enso-recall",
      corpusRoot: tmpDir(),
      timeoutMs: 500,
    });
    const runId = "run-abc123";

    // Ensō side: whole assembled prompt, keyed on runId from the agent ctx.
    await hooks.get("before_prompt_build")?.(
      { prompt: "a long assembled prompt containing the granola question and much more" },
      { sessionKey: "s1", sessionId: "sess-1", runId },
    );
    // Flat-file side: just the tool query, keyed on the SAME runId from tool ctx.
    await hooks.get("after_tool_call")?.(
      { toolName: "memory_search", params: { query: "granola" }, durationMs: 5 },
      { sessionKey: "s1", sessionId: "sess-1", runId },
    );

    const records = readRecords(shadowLogDir);
    expect(records).toHaveLength(2);
    const ensoRec = records.find((r) => r.kind !== "flatfile_result");
    const flatRec = records.find((r) => r.kind === "flatfile_result");
    expect(ensoRec?.turn).toBe(runId);
    expect(flatRec?.turn).toBe(runId);
    expect(ensoRec?.turn).toBe(flatRec?.turn);
    expect(ensoRec?.turn_src).toBe("runId");
    expect(flatRec?.turn_src).toBe("runId");
  });

  it("after_tool_call falls back to the event runId when the ctx lacks one", async () => {
    const shadowLogDir = tmpDir();
    const { hooks } = registerPlugin({ shadowLogDir });
    await hooks.get("after_tool_call")?.(
      { toolName: "memory_search", params: { query: "granola" }, runId: "run-from-event" },
      {},
    );
    const records = readRecords(shadowLogDir);
    expect(records).toHaveLength(1);
    expect(records[0]?.turn).toBe("run-from-event");
    expect(records[0]?.turn_src).toBe("runId");
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

describe("WP-8 blocker (a): reply-use RECALL-DEF labeling (llm_output observer)", () => {
  // A fake enso-recall binary that returns two results whose `content` fields
  // are controllable, so the material-use matcher has real text to score
  // without a live Go build. Written to a temp dir and marked executable.
  function fakeBridge(results: Array<{ id: string; content: string }>): string {
    const dir = tmpDir();
    const bin = path.join(dir, "enso-recall");
    const payload = {
      version: 1,
      query: "",
      as_of: "",
      mode: "lexical",
      degraded: "",
      elapsed_ms: 1,
      corpus_entries: results.length,
      results: results.map((r) => ({
        id: r.id,
        type: "fact",
        content: r.content,
        specificity: 0.5,
        strength: 0.5,
      })),
    };
    fs.writeFileSync(
      bin,
      `#!/usr/bin/env node\nprocess.stdout.write(${JSON.stringify(JSON.stringify(payload))});\n`,
      { mode: 0o755 },
    );
    return bin;
  }

  async function runTurn(
    hooks: Map<string, HookHandler>,
    runId: string,
    prompt: string,
    assistantTexts: string[],
  ) {
    await hooks.get("before_prompt_build")?.({ prompt }, { sessionKey: "s1", runId });
    await hooks.get("llm_output")?.({ runId, sessionId: "sess-1", assistantTexts }, {});
  }

  it("END TO END: labels a materially-used memory yes and an unused one no", async () => {
    const shadowLogDir = tmpDir();
    const ensoBinary = fakeBridge([
      { id: "mem:2026-09-20-omega", content: "Omega livetopology aggregate rewrite shipped" },
      { id: "mem:2026-03-20-owen", content: "Owen birthday is March twentieth" },
    ]);
    const { hooks } = registerPlugin({ shadowLogDir, ensoBinary, corpusRoot: tmpDir() });

    await runTurn(
      hooks,
      "run-1",
      "what happened with omega livetopology?",
      ["The omega livetopology aggregate rewrite is done and stacked on your branch."],
    );

    const replyUse = readRecords(shadowLogDir).find((r) => r.kind === "reply_use");
    expect(replyUse).toBeDefined();
    expect(replyUse?.turn).toBe("run-1");
    expect(replyUse?.turn_src).toBe("runId");
    expect(replyUse?.used).toBe("yes"); // at least one memory was used
    const byId = Object.fromEntries((replyUse?.reply_use ?? []).map((v) => [v.id, v]));
    expect(byId["mem:2026-09-20-omega"]?.used).toBe("yes");
    expect(byId["mem:2026-09-20-omega"]?.evidence).toContain("livetopology");
    expect(byId["mem:2026-03-20-owen"]?.used).toBe("no"); // never mentioned in the reply
  });

  it("labels used=no when the reply drew on none of the recalled memories", async () => {
    const shadowLogDir = tmpDir();
    const ensoBinary = fakeBridge([
      { id: "mem:x", content: "Omega livetopology aggregate rewrite shipped" },
    ]);
    const { hooks } = registerPlugin({ shadowLogDir, ensoBinary, corpusRoot: tmpDir() });
    await runTurn(hooks, "run-2", "unrelated question", ["Sure, the weather looks clear today."]);
    const replyUse = readRecords(shadowLogDir).find((r) => r.kind === "reply_use");
    expect(replyUse?.used).toBe("no");
    expect(replyUse?.reply_use?.[0]?.used).toBe("no");
  });

  it("writes no reply_use record when the turn recalled nothing for that runId", async () => {
    const shadowLogDir = tmpDir();
    const { hooks } = registerPlugin({ shadowLogDir });
    // llm_output fires for a runId that never went through before_prompt_build.
    await hooks.get("llm_output")?.(
      { runId: "orphan-run", sessionId: "sess-1", assistantTexts: ["hi"] },
      {},
    );
    expect(readRecords(shadowLogDir).some((r) => r.kind === "reply_use")).toBe(false);
  });

  it("consumes the pending turn so a duplicate llm_output does not double-label", async () => {
    const shadowLogDir = tmpDir();
    const ensoBinary = fakeBridge([{ id: "mem:x", content: "omega livetopology aggregate" }]);
    const { hooks } = registerPlugin({ shadowLogDir, ensoBinary, corpusRoot: tmpDir() });
    await runTurn(hooks, "run-3", "q", ["omega livetopology aggregate confirmed"]);
    // second llm_output for the same runId: nothing left to label.
    await hooks.get("llm_output")?.(
      { runId: "run-3", sessionId: "sess-1", assistantTexts: ["omega livetopology aggregate"] },
      {},
    );
    expect(readRecords(shadowLogDir).filter((r) => r.kind === "reply_use")).toHaveLength(1);
  });

  it("swallows a malformed llm_output event (observation-only, never throws)", async () => {
    const shadowLogDir = tmpDir();
    const { hooks } = registerPlugin({ shadowLogDir });
    const returned = await hooks.get("llm_output")?.(undefined, {});
    expect(returned).toBeUndefined();
    expect(readRecords(shadowLogDir).some((r) => r.kind === "reply_use")).toBe(false);
  });
});

describe("summarizeToolResult", () => {
  it("bounds and flattens arbitrary results", () => {
    const s = summarizeToolResult({ a: "x".repeat(2000), b: "line\nbreak" });
    expect(s.length).toBeLessThanOrEqual(500);
    expect(s).not.toContain("\n");
  });
});
