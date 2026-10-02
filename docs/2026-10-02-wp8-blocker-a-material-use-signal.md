# WP-8 blocker (a) — the RECALL-DEF `used` signal, derived from `llm_output`

*2026-10-02 (Dross Hour). Enso host adapter (`host/openclaw/enso-memory/`) only.
No `internal/core`/`mdstore`/`graphstore` change, no corpus writes, no Go change.*

## The blocker, precisely

WP-8's deliverable is to decide whether Ensō should take the live `memory` slot,
by comparing — on real turns — whether Ensō's recall ranking surfaces the memory
the turn actually *needed* better than the flat-file path does. That comparison
has two halves: (1) what each side recalled (logged since WP-7), and (2) whether
any recalled memory was **materially used** in the reply. Half (2) is the
RECALL-DEF signal. Every shadow record since WP-7 carried `used: "unknown"`
because nothing filled it. The 2026-09-24 first analysis named this the core
unexecutable gap; the 2026-09-29 co-keying amendment fixed the *pairing* half of
finding #2 (recall ↔ flat-file now share `runId`) and left **"fill `used` from a
host material-use signal the host does not emit yet"** as the single remaining
long pole.

## The finding: the host DOES emit the reply, keyed on the co-key

"The host does not emit a material-use event" is true only of a *purpose-built*
"memory X was used" event. Verified against the published SDK type defs
(`openclaw/dist/plugin-sdk/`, 2026-10-02):

- **`message_sent` / `message_sending`** carry the outbound reply `content`, but
  the SDK explicitly documents that `runId` is **not plumbed through the outbound
  delivery path** — so those hooks cannot co-key against the recall hooks. Only
  `sessionKey` is shared there, and it "cannot disambiguate concurrent turns in
  the same session." A dead end for pairing.
- **`llm_output` / `PluginHookLlmOutputEvent`** is the right seam. It fires on
  the agent-runtime path where `runId` **is** populated (the SDK lists
  `agent_end`/`llm_input`/`llm_output` as the hooks that already receive the run
  id), and it carries `assistantTexts: string[]` — the model's actual reply.

So material use is **derivable, not absent**: for each memory Ensō recalled on a
`runId`, test whether its distinctive content surfaces in that turn's
`assistantTexts`. The co-key the 2026-09-29 amendment already standardized on
(`runId`) is exactly the join key `llm_output` exposes.

## What shipped

A third observer in the host adapter, parallel to the two WP-7 hooks, same three
rules (observation-only, fail-safe, no memory logic in TS):

1. **`material-use.ts`** — a standalone, deterministic matcher. `assessMaterialUse(memoryText, replyText)`
   returns `yes/no` + a confidence `score` + the matched `evidence` run.
   Precision-gated: it requires a run of **≥3 consecutive distinctive tokens**
   shared in order between the memory and the reply (stop words + the structural
   memory-id vocabulary `mem/type/fact/decision/…` are stripped first). A single
   shared topic word ("latency", "omega") is **not** a match — that is the exact
   false-positive class the gate must not absorb.
2. **In-memory recall→reply bridge** (`index.ts`). `before_prompt_build` already
   recalls; it now stashes the recalled `{id, content}` keyed by the turn's
   `runId` in a bounded map (256 turns, oldest-evicted). Content stays in memory
   only — the persistent JSONL still logs ids+scores, never memory text.
3. **`llm_output` observer.** Looks up the pending recall for the event's
   `runId`, scores each memory against the joined `assistantTexts`, writes one
   new **`reply_use`** record (per-id verdicts + an aggregate `used: "yes"|"no"`),
   and evicts the turn so a duplicate `llm_output` cannot double-label.

### Record format change (additive, append-only)

- `used` widened `"unknown"` → `"unknown" | "yes" | "no"`. Recall/flatfile
  records still write `"unknown"` (they are written *before* the reply exists);
  the new `reply_use` record carries the real verdict. The analysis pass joins
  recall → flatfile → reply_use on `turn` (runId). This honors the append-only
  spirit: no already-written line is mutated: the label arrives as a new record.
- New `kind: "reply_use"` and a `reply_use: ReplyUseVerdict[]` field.

## Precision over recall — the deliberate limits (stop at the seam)

This CAPTURES and LABELS the signal. It does **not** decide slot takeover.

- The matcher is a lexical-overlap heuristic. A reply that *paraphrases* a memory
  without reusing a distinctive 3-token run scores `no` (a false negative — a lost
  data point, the safe direction). A reply that happens to contain a memory's
  distinctive phrase for an unrelated reason scores `yes` (a false positive). The
  `score`+`evidence` fields exist precisely so a human labeling pass can trust a
  strong hit and audit/discount a weak one — the verdict is *reviewable*, not a
  black box.
- No semantic/embedding match: that would reintroduce the embed latency this
  path exists to avoid, and would trade auditability for recall. YAGNI until a
  real case shows the lexical matcher missing paraphrase reuse at a rate that
  changes the gate verdict.
- **The gate decision is unchanged:** after a fresh shadow window with this label
  live, a human reads the labeled corpus and decides takeover by the standing
  rule. This pass makes that corpus *producible*, nothing more.

## Why this needs a real shadow window before it proves anything

Like the 2026-09-29 amendment, this is validated by tests against the real SDK
shape, not yet by production data — the previous shadow window (Jul 25–Aug 11)
predates both the co-keying and this label and ran entirely in degraded lexical
mode (the 3-week embedding outage). WP-8 stays **BLOCKED** until a fresh window
with (co-keying + this `used` label + the now-fixed vector path + the Oct-1
persisted index) runs long enough to label divergent turns. But the blocker is
no longer "the signal is unobtainable" — it is now only "collect the window."
Both of blocker (a)'s named sub-parts (co-key the hooks; fill `used`) are done.

## Verification

- `material-use.test.ts`: 12 tests pinning the matcher both directions —
  genuine reuse `yes`, topic-only / stop-word-only / id-echo / empty-reply `no`,
  and the exact MIN_RUN boundary (3 → yes, 2 → no).
- `index.test.ts`: 5 new tests — end-to-end recall→llm_output labels a used
  memory `yes` and an unused one `no` (via a fake bridge with controllable
  `content`), the all-`no` case, no-record-when-nothing-recalled, duplicate
  `llm_output` does not double-label, malformed event swallowed.
- `vitest run` 49/49 green · `tsc --noEmit` clean · repo `make check` +
  `-race` green · drift IN SYNC (7 sources).

## Files

- `host/openclaw/enso-memory/material-use.ts` (new) — the matcher.
- `host/openclaw/enso-memory/material-use.test.ts` (new) — matcher tests.
- `host/openclaw/enso-memory/shadow-log.ts` — `used` widened; `reply_use` kind +
  `ReplyUseVerdict`.
- `host/openclaw/enso-memory/index.ts` — pending-recall bridge + `llm_output`
  observer.
- `host/openclaw/enso-memory/index.test.ts` — reply-use observer tests.
