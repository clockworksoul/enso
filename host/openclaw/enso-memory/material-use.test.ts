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
