// Material-use matcher tests (WP-8 blocker a). The whole point of this module
// is PRECISION: a false "yes" corrupts the eventual slot-takeover gate. These
// tests pin both directions — a genuine reuse scores "yes", and the known
// false-positive shapes (topic overlap, stop-word overlap, id echo) score "no".
import { describe, expect, it } from "vitest";
import {
  assessMaterialUse,
  distinctiveTokens,
  longestSharedRun,
  MIN_RUN,
} from "./material-use.js";

describe("distinctiveTokens", () => {
  it("drops stop words, short noise, and structural id tokens", () => {
    expect(distinctiveTokens("The Omega livetopology service is on at it")).toEqual([
      "omega",
      "livetopology",
      "service",
    ]);
  });

  it("drops the memory-id structural vocabulary (mem/type/fact/decision)", () => {
    expect(distinctiveTokens("mem: decision type fact omega")).toEqual(["omega"]);
  });

  it("lowercases and splits on punctuation", () => {
    expect(distinctiveTokens("AuraDB-runbook, step_3!")).toEqual(["auradb", "runbook", "step"]);
  });
});

describe("longestSharedRun", () => {
  it("finds the longest contiguous in-order token run", () => {
    expect(
      longestSharedRun(
        ["alpha", "beta", "gamma", "delta"],
        ["zzz", "beta", "gamma", "delta", "yyy"],
      ),
    ).toEqual(["beta", "gamma", "delta"]);
  });

  it("returns the single longest run, not a scattered union", () => {
    // 'alpha' and 'delta' both match but are not contiguous -> run length 1.
    expect(longestSharedRun(["alpha", "x", "delta"], ["alpha", "y", "delta"])).toHaveLength(1);
  });

  it("empty on no overlap or empty input", () => {
    expect(longestSharedRun(["a", "b"], ["c", "d"])).toEqual([]);
    expect(longestSharedRun([], ["a"])).toEqual([]);
    expect(longestSharedRun(["a"], [])).toEqual([]);
  });
});

describe("assessMaterialUse", () => {
  it("YES: the reply reuses a distinctive phrase from the recalled memory", () => {
    const memory = "Omega livetopology aggregate rewrite shipped on the dross branch";
    const reply =
      "I looked it up — the omega livetopology aggregate rewrite is already done and stacked.";
    const v = assessMaterialUse(memory, reply);
    expect(v.used).toBe("yes");
    expect(v.score).toBeGreaterThanOrEqual(MIN_RUN);
    expect(v.evidence).toContain("livetopology");
  });

  const memoryId = "mem:2026-08-10-hold-bug-report-internal-context-envelope";
  const memory = "Matt decided on 2026-08-10 to hold off filing an OpenClaw bug report about the internal context envelope pending a verified repro.";

  it.each([
    "The recall block above your message was the internal-context envelope note, the Dross reconciliation, and the Night Shift entry. None of them was relevant.",
    `the plugin credited the note about the "internal context envelope" bug as materially used in my last reply and marked it ... my reply mentioned that note only to say it wasn't relevant`,
    "The internal context envelope came up.",
  ])("NO: mentioning the memory's topic is insufficient: %s", (reply) => {
    expect(assessMaterialUse(memory, reply, { memoryId }))
      .toEqual({ used: "no", score: 0, evidence: "" });
  });

  it("YES: the held filing decision supplies content beyond the slug", () => {
    const verdict = assessMaterialUse(memory, "Filing was held pending a verified repro.", { memoryId });
    expect(verdict).toEqual({ used: "yes", score: 3, evidence: "pending verified repro" });
  });

  it("NO: quoting the user's prompt provides no new evidence", () => {
    const promptTokens = new Set(distinctiveTokens("Was filing pending a verified repro?"));
    expect(assessMaterialUse(memory, "It was pending a verified repro.", { memoryId, promptTokens }))
      .toEqual({ used: "no", score: 0, evidence: "" });
  });

  it("counts only novel non-slug tokens within a full shared run", () => {
    const context = {
      memoryId: "mem:2026-08-10-topic",
      promptTokens: new Set(["echo"]),
    };
    expect(assessMaterialUse("alpha topic beta echo", "alpha topic beta echo", context).used).toBe("no");
    expect(assessMaterialUse("alpha topic beta echo gamma", "alpha topic beta echo gamma", context))
      .toEqual({ used: "yes", score: 5, evidence: "alpha topic beta echo gamma" });
  });

  it.each(["topic", "echo"])("does not splice excluded %s tokens out to create a run", (excluded) => {
    expect(assessMaterialUse("alpha beta gamma", `alpha ${excluded} beta gamma`, {
      memoryId: "mem:2026-08-10-topic",
      promptTokens: new Set(["echo"]),
    }).used).toBe("no");
  });

  it("can find a qualifying run even when the longest run is excluded", () => {
    expect(assessMaterialUse(
      "internal context envelope bug report; pending verified repro",
      "Internal context envelope bug report. Pending a verified repro.",
      { memoryId },
    )).toEqual({ used: "yes", score: 3, evidence: "pending verified repro" });
  });

  it.each([
    "not relevant", "irrelevant", "nothing relevant", "none of them", "unrelated",
    "doesn't apply", "doesn’t apply", "did not apply", "no bearing", "not useful", "off-topic", "false positive",
  ])("NO: a genuine-looking run in a dismissal sentence (%s)", (marker) => {
    expect(assessMaterialUse(memory, `Pending a verified repro — ${marker.toUpperCase()}.`, { memoryId }))
      .toEqual({ used: "no", score: 0, evidence: "" });
  });

  it("recognizes a dismissal in the same sentence across a line wrap", () => {
    expect(assessMaterialUse(memory, "Pending a verified repro\nis not relevant.", { memoryId }).used).toBe("no");
  });

  it("scopes dismissals to their sentence, allowing a separate genuine use", () => {
    expect(assessMaterialUse(memory, "The weather is unrelated. Filing was held pending a verified repro.", { memoryId }))
      .toEqual({ used: "yes", score: 3, evidence: "pending verified repro" });
  });

  it("does not manufacture evidence across sentence boundaries", () => {
    expect(assessMaterialUse(memory, "Pending. Verified repro.", { memoryId }).used).toBe("no");
  });

  it("NO: same topic, no distinctive run (shared single word only)", () => {
    // Both mention 'latency' but the reply draws on nothing distinctive from
    // the memory — this is the classic false-positive the matcher must reject.
    const memory = "The persisted index warm path cut recall latency to under one second";
    const reply = "Latency looks fine to me, nothing to worry about there.";
    expect(assessMaterialUse(memory, reply).used).toBe("no");
  });

  it("NO: pure stop-word / structural overlap never counts", () => {
    const memory = "mem: decision type fact the a an and of to in on at";
    const reply = "The decision is a fact and of course it is on the table.";
    expect(assessMaterialUse(memory, reply).used).toBe("no");
  });

  it("NO: empty reply (turn produced no assistant text)", () => {
    expect(assessMaterialUse("omega livetopology aggregate rewrite", "").used).toBe("no");
  });

  it("boundary: a run of exactly MIN_RUN distinctive tokens is YES", () => {
    const shared = "sarracenia butterwort bladderwort"; // 3 distinctive tokens
    expect(assessMaterialUse(shared, `Matt's bog garden has ${shared} growing.`).used).toBe("yes");
  });

  it("boundary: a run of MIN_RUN-1 distinctive tokens is NO", () => {
    const shared = "sarracenia butterwort"; // 2 distinctive tokens < MIN_RUN
    expect(assessMaterialUse(shared, `The ${shared} are thriving.`).used).toBe("no");
  });
});
