// WP-8 blocker (a): derive the RECALL-DEF "used" signal the host does not emit
// as a purpose-built event.
//
// The problem the 2026-09-24 shadow-corpus analysis surfaced: every record
// carried `used: "unknown"`, so the corpus could log WHAT Ensō recalled but
// never WHETHER any recalled memory was actually used in the reply. Without
// that label the slot-takeover gate (does Ensō's ranking beat the flat-file
// path?) is not executable — you cannot score a ranking against "was this the
// memory the turn actually needed" with no ground truth for the second half.
//
// The seam (verified 2026-10-02 against the published SDK type defs): the host
// does not emit a "memory X was used" event, but it DOES emit `llm_output`
// (PluginHookLlmOutputEvent) carrying the final `assistantTexts` keyed on the
// SAME host-authoritative `runId` the recall hooks already co-key on (per the
// 2026-09-29 amendment). Material use is therefore DERIVABLE: for each memory
// Ensō recalled on a runId, test whether its distinctive content surfaces in
// the assistant's actual reply.
//
// PRECISION OVER RECALL (the standing Ensō invariant): a false `used: "yes"`
// silently corrupts the eventual gate, so the matcher is deliberately
// conservative. It requires a run of DISTINCTIVE shared tokens, not incidental
// stop-word overlap, and it emits a confidence band + the matched evidence so
// an analysis pass can trust a strong hit and discount a weak one. This module
// CAPTURES and LABELS the signal; it does NOT decide slot takeover. That
// decision still happens later, by a human, against the labeled corpus this
// produces.

/** Outcome of testing one recalled memory's text against the reply text. */
export type MaterialUse = "yes" | "no";

export type MaterialUseVerdict = {
  used: MaterialUse;
  /**
   * Confidence in a "yes": the length of the longest distinctive shared token
   * run (consecutive content tokens appearing in order in both texts). 0 for a
   * "no". A single shared distinctive token is weak; a run of 3+ is strong.
   */
  score: number;
  /** The matched token run, for human labeling. Empty on "no". */
  evidence: string;
};

/**
 * Minimum consecutive distinctive-token run that counts as material use.
 * Two independent texts about the same topic routinely share ONE content word
 * ("latency", "omega"); sharing a run of N consecutive distinctive tokens in
 * order is a far stronger signal that the reply drew on the recalled memory.
 * Tuned conservative: raising it trades recall for precision, which is the
 * correct direction for this corpus (a missed "yes" is a lost data point; a
 * false "yes" is a corrupted one).
 */
export const MIN_RUN = 3;

// English stop words + structural memory-id tokens that carry no topical
// signal. Overlap on these must never count toward material use.
const STOP = new Set([
  "the", "a", "an", "and", "or", "but", "if", "then", "else", "for", "of", "to",
  "in", "on", "at", "by", "is", "are", "was", "were", "be", "been", "being",
  "it", "its", "this", "that", "these", "those", "with", "as", "from", "into",
  "about", "over", "under", "so", "no", "not", "yes", "do", "does", "did",
  "have", "has", "had", "will", "would", "can", "could", "should", "may",
  "might", "i", "you", "he", "she", "they", "we", "me", "him", "her", "them",
  "us", "my", "your", "his", "their", "our", "mem", "type", "id", "fact",
  "decision", "entry", "memory",
]);

/** Lowercase, split on non-alphanumerics, drop stop words and ≤2-char noise. */
export function distinctiveTokens(text: string): string[] {
  return text
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter((t) => t.length > 2 && !STOP.has(t));
}

/**
 * Longest run of consecutive distinctive tokens from `needle` that appears, in
 * order and contiguously, anywhere in `haystack`. This is a cheap approximate
 * longest-common-substring over token sequences — O(n*m) worst case on the
 * bounded, short texts here (a recalled memory snippet vs a reply), which is
 * fine and deterministic.
 */
export function longestSharedRun(needle: string[], haystack: string[]): string[] {
  if (needle.length === 0 || haystack.length === 0) {
    return [];
  }
  let best: string[] = [];
  for (let i = 0; i < needle.length; i++) {
    for (let j = 0; j < haystack.length; j++) {
      let k = 0;
      while (
        i + k < needle.length &&
        j + k < haystack.length &&
        needle[i + k] === haystack[j + k]
      ) {
        k++;
      }
      if (k > best.length) {
        best = needle.slice(i, i + k);
      }
    }
  }
  return best;
}

/**
 * Did the reply materially use the recalled memory? Conservative: requires a
 * run of MIN_RUN consecutive distinctive tokens shared, in order, between the
 * memory text and the reply. Returns the verdict plus the evidence run so a
 * labeling pass can audit every "yes".
 */
export function assessMaterialUse(memoryText: string, replyText: string): MaterialUseVerdict {
  const run = longestSharedRun(distinctiveTokens(memoryText), distinctiveTokens(replyText));
  if (run.length >= MIN_RUN) {
    return { used: "yes", score: run.length, evidence: run.join(" ") };
  }
  return { used: "no", score: 0, evidence: "" };
}
