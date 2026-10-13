# Ensō — Miss-Corpus Registry & Measurement Protocol

**Date:** 2026-07-09
**Status:** DRAFT — effective upon ratification alongside ADR-001
**Purpose:** make the labeled real-miss corpus a first-class, append-only,
machine-readable artifact so that (a) the RH-2 rule ("no feature without a real
case") has a registry to check against, and (b) the WP-4 benchmark gate is
computed mechanically instead of argued.
**Relationship to existing code:** this protocol does not replace
`internal/bench` — it feeds it. The registry is the *source of record* for
which misses are real; the Go fixtures in `cases.go`/`held_out_cases.go` are
*executable reconstructions* that MUST reference a registry `id`.

---

## 1. The registry file

**Location:** `internal/bench/testdata/miss_corpus.jsonl`
**Format:** JSON Lines — one JSON object per line, one line per miss.
**Governance:** **append-only (INV-2 applies to our own evidence).** A wrong
line is never edited or deleted; append a corrected line whose `supersedes`
field names the wrong line's `id`. Readers treat a superseded line as
not-current, exactly like the memory substrate does.

### 1.1 Line schema

| Field | Type | Req | Meaning |
| --- | --- | --- | --- |
| `id` | string | ✅ | Stable, kebab-case, never reused. Convention: `miss:<YYYY-MM-DD>-<slug>` (date = when observed). |
| `date_observed` | ISO-8601 date | ✅ | When the miss happened or was flagged. |
| `source` | string | ✅ | Provenance pointer: the doc/log/section where the miss is recorded (e.g. `docs/2026-06-17-phase0-benchmark.md#2026-06-18`, `dross/2026-07-02-...md`, `[FLAGGED-MISS] DM`). A miss with no provenance is not real (RH-2). |
| `miss_class` | enum | ✅ | See §2 taxonomy. |
| `query` | string | ✅ | The question/moment where recall should have surfaced the memory, as faithfully as reconstructable. |
| `utterance` | string | – | The verbatim (faithfully reconstructed) correction sentence, if one existed. **Empty is meaningful, not missing** (NEIGHBOR/FABRICATION misses often have none). |
| `stale_entry` | string | – | Substrate `mem:` id (or short description pre-P1) of the entry that was wrongly surfaced / wrongly current. |
| `expected_entry` | string | ✅ | Substrate `mem:` id (or description) of the ground-truth correct answer. |
| `relevant_ids` | string[] | – | Other entries that would be *acceptable* (non-noise) in a returned set for this query. Used by the noise metric (§4.2). Default: `[expected_entry]` only. |
| `gate_eligible` | bool | ✅ | Whether this line counts toward the WP-4 gate (§4). `INFRA`-class lines are `false`. |
| `fixture` | string | – | Name of the Go bench `Case` reconstructing this miss (e.g. `adam-headcount-stale`), once one exists. Registry-only lines have this empty. |
| `status` | enum | ✅ | `open` (no fixture yet) \| `fixtured` (executable) \| `superseded`. |
| `supersedes` | string | – | `id` of a prior line this one corrects. |
| `notes` | string | – | Anything a future reader needs; keep it short. |

### 1.2 Rules

- **R-1:** Every miss that anyone cites — in a commit message, an ADR, a design
  argument, a WP checkbox — must exist as a line here first. "There was that
  time when…" is not evidence until it has an `id`.
- **R-2:** Every Go bench `Case` must carry a registry `id` (add a `RegistryID`
  field to `bench.Case` in the next touch of `corpus.go`; budgeted under the WP
  that touches it). Fixtures without registry lines are synthetic and MUST NOT
  set `gate_eligible` semantics — they justify tests, never features (RH-2).
- **R-3:** Anyone may append; nobody edits. `git log` on the file is the audit
  trail.
- **R-4:** Infrastructure failures (embedding outages, plugin trigger gaps) ARE
  recorded — they motivate requirements (see ADR-001's use of the 429 outage) —
  but are `INFRA`-class and gate-excluded: the gate measures *ranking quality*,
  not uptime.

---

## 2. Miss-class taxonomy (closed set)

| Class | Definition | Canonical example |
| --- | --- | --- |
| `STALE` | A superseded fact surfaced (or would surface) as current. Failure mode #4. | adam-headcount; granola-ban |
| `NEIGHBOR` | The right memory existed but wasn't reached because the query's vocabulary matched a *neighbor*, not the target. Failure mode #3. | enso-repo-path |
| `FABRICATION` | Recall confabulated toward a plausible gist not supported by any stored entry. | (Jul-7 doc's class; corpus n=0 with provenance — a future line earns it) |
| `NOISE` | Recall surfaced something irrelevant, wasting the bounded injection budget. | (per Phase-0 metric definition) |
| `NON_CAPTURE` | The memory was never written at all — a capture-policy failure, not a recall failure. Gate-excluded (nothing to rank); feeds the capture policy instead. | Ensō's own origin lineage (Jul-7 doc §"Why this doc exists") |
| `INFRA` | Retrieval infrastructure failed outright. Gate-excluded. | Jun-18 OpenAI key; Jul-7 Gemini 429 |

Adding a class requires an ADR (it changes what the gate means).

---

## 3. Seed registry

The initial `miss_corpus.jsonl` content, reconstructed from the documented
record. (Descriptions stand in for `mem:` ids until the P1 corpus backfills
them; update via superseding lines then.)

```jsonl
{"id":"miss:2026-06-25-adam-headcount","date_observed":"2026-06-25","source":"internal/bench/cases.go (adam-headcount-stale) + Phase-0 log","miss_class":"STALE","query":"what's the status of the Adam headcount item?","utterance":"actually the Adam headcount ask already landed at the Jun 18 1:1","stale_entry":"desc:headcount-ask-still-open","expected_entry":"desc:headcount-landed-jun18","relevant_ids":[],"gate_eligible":true,"fixture":"adam-headcount-stale","status":"fixtured","notes":"The end-to-end proof case (README headline, 2026-06-26)."}
{"id":"miss:2026-06-25-granola-ban","date_observed":"2026-06-25","source":"internal/bench/held_out_cases.go (granola-ban-stale); Day-7 Phase-0 log","miss_class":"STALE","query":"is Granola banned — what's the transcript workflow?","utterance":"Granola still works and is the transcript source of record","stale_entry":"desc:granola-banned","expected_entry":"desc:granola-still-source-of-record","relevant_ids":[],"gate_eligible":true,"fixture":"granola-ban-stale","status":"fixtured","notes":"Held-out H1; still-affirmative restate."}
{"id":"miss:2026-06-25-leanctx-scope","date_observed":"2026-06-25","source":"internal/bench/held_out_cases.go (leanctx-scope-stale); dross/2026-07-02-enso-leanctx-scope-expansion.md","miss_class":"STALE","query":"what is LeanCTX's scope?","utterance":"LeanCTX does more than that now, the note undersells its current scope","stale_entry":"desc:leanctx-narrow-helper","expected_entry":"desc:leanctx-expanded-scope","relevant_ids":[],"gate_eligible":true,"fixture":"leanctx-scope-stale","status":"fixtured","notes":"Held-out H2; scope-expansion (wrong-by-omission). Meta-only utterance — no payload."}
{"id":"miss:2026-06-25-ed-sandoval","date_observed":"2026-06-25","source":"internal/bench/cases.go (ed-sandoval-timeline-reframe)","miss_class":"STALE","query":"whose court is the Neo4j blog post in?","utterance":"the ball is in Ed's court, not Matt's","stale_entry":"desc:blogpost-on-matt","expected_entry":"desc:blogpost-on-ed","relevant_ids":[],"gate_eligible":true,"fixture":"ed-sandoval-timeline-reframe","status":"fixtured","notes":"Reframe: underlying facts unchanged, framing corrected."}
{"id":"miss:2026-06-25-enso-repo-path","date_observed":"2026-06-25","source":"internal/bench/cases.go (enso-repo-path-neighbor)","miss_class":"NEIGHBOR","query":"where does the enso repo live locally?","utterance":"","stale_entry":"desc:parent-dir-entry","expected_entry":"desc:repo-path-entry","relevant_ids":[],"gate_eligible":true,"fixture":"enso-repo-path-neighbor","status":"fixtured","notes":"The one NEIGHBOR case; docs note corpus n=1 for this class."}
{"id":"miss:2026-06-18-ollie-ali","date_observed":"2026-06-18","source":"docs/2026-06-17-phase0-benchmark.md#2026-06-18/19-dross-skim","miss_class":"NON_CAPTURE","query":"(transcript processing) who is 'Ollie'?","utterance":"","stale_entry":"","expected_entry":"desc:ollie-equals-ali-transcription-quirk (May 6 note)","relevant_ids":[],"gate_eligible":false,"fixture":"","status":"open","notes":"Near-miss: caught from in-session context while memory_search was down; would have failed in a fresh session. Recorded as the canonical cross-session-dependence example."}
{"id":"miss:2026-06-18-embedding-key-outage","date_observed":"2026-06-18","source":"docs/2026-06-17-phase0-benchmark.md#2026-06-18-day-2","miss_class":"INFRA","query":"(all memory_search queries that day)","utterance":"","stale_entry":"","expected_entry":"","relevant_ids":[],"gate_eligible":false,"fixture":"","status":"open","notes":"OpenAI embedding key missing; cross-session recall offline all day."}
{"id":"miss:2026-07-07-gemini-429","date_observed":"2026-07-07","source":"docs/2026-07-07-mnemosyne-prior-art-comparison.md (scope-correction section)","miss_class":"INFRA","query":"(all semantic recall that evening)","utterance":"","stale_entry":"","expected_entry":"","relevant_ids":[],"gate_eligible":false,"fixture":"","status":"open","notes":"Gemini embedding quota exhausted; motivates ADR-001 internal-vectors requirement."}
{"id":"miss:2026-07-07-enso-lineage","date_observed":"2026-07-07","source":"docs/2026-07-07-mnemosyne-prior-art-comparison.md#why-this-doc-exists","miss_class":"NON_CAPTURE","query":"what prior-art system sparked Ensō?","utterance":"","stale_entry":"","expected_entry":"desc:mnemosyne-lineage (now captured in the Jul-7 doc)","relevant_ids":[],"gate_eligible":false,"fixture":"","status":"open","notes":"Origin lineage never captured; Matt had to dig through chat logs. The on-brand embarrassment that motivates the capture policy."}
```

---

## 4. Measurement protocol

All measurement is **offline replay** through `internal/bench` (the live plugin
is a black box — `persistTranscripts:false`; see `corpus.go` package doc). Each
gate-eligible case is replayed "as of" its `date_observed` against the models
below; a case's candidate set is its fixture's reconstruction of the corpus at
that instant.

### 4.1 Hit rate (the RECALL-DEF proxy)

RECALL-DEF says a memory counts as recalled only when *surfaced AND materially
used*. Offline replay cannot observe "materially used," so the protocol adopts
the strictest defensible proxy, matching the existing harness convention:

> **Hit** = the model ranks `expected_entry` **first** (top-1) for the case's
> query at the case's `as_of` instant.

**Hit rate** = hits / gate-eligible cases, reported overall **and per
miss-class**. Rationale for top-1: the injection budget is 220 chars — in
practice one memory gets used; rank-1 is the only position with a strong claim
to "materially usable." (Top-3 may be *reported* as a secondary diagnostic; the
gate reads top-1.)

### 4.2 Noise rate (the bounded-budget proxy)

> For each case, examine the model's **top-k (k = 3)** returned entries. An
> entry is **noise** if it is neither `expected_entry` nor in `relevant_ids`.
> Case noise = noise entries / min(k, returned). **Noise rate** = mean case
> noise over gate-eligible cases.

This operationalizes the Phase-0 definition ("how often did recall inject
something irrelevant into the bounded budget") without requiring live
transcripts. `relevant_ids` defaults to empty (strict); label leniently only
with a note.

### 4.3 Baselines

- **B0 — naive recency:** rank by `encoded_time` descending, no supersession
  awareness (the existing naive baseline in `corpus.go`).
- **B1 — flat lexical:** specificity/lexical match only, no decay, no
  supersession filter (the `memory_search`-equivalent; implement as a bench
  model if absent — it is a scoring variant, not a feature).

### 4.4 The WP-4 gate, computed

WP-4 ships iff **all** of:

1. Ensō hit rate > B0 hit rate **and** > B1 hit rate (overall).
2. Ensō per-class hit rate ≥ each baseline's per-class rate on `STALE` **and**
   `NEIGHBOR` (the two classes Phase 2 exists to fix; no regressing one to win
   the other).
3. Ensō noise rate ≤ max(B0 noise, B1 noise).
4. **Corpus floor:** ≥ 6 gate-eligible cases total with ≥ 2 per gated class.
   As of seeding, `NEIGHBOR` has n = 1 — the gate is **blocked on one more real
   NEIGHBOR miss**, which is correct: it is not satisfiable by manufacturing a
   case (RH-2). Report the blockage honestly; do not lower the floor.

Any class with n < 2 at gate time is reported as *low-confidence* and cannot be
the sole basis of a pass.

### 4.5 Recording

Gate runs append a dated results block to
`docs/2026-06-17-phase0-benchmark.md`'s log (it is the designated running
measurement record): corpus size per class, all three models' hit/noise
numbers, verdict, and the git SHA measured. The verdict line is copied into
`ENSO-STATUS.md`.

---

## 5. Intake workflow (how a miss becomes a case)

1. **Flag** — Matt or Dross notices a miss; a `[FLAGGED-MISS]` note lands in the
   Phase-0 log or a dross note *the same day* (provenance).
2. **Register** — append a line here (`status: open`). This step, not the flag,
   is what makes it citable.
3. **Fixture** — when the relevant WP needs it, reconstruct the pre-miss corpus
   state as a Go `Case` referencing the registry `id`; flip `status: fixtured`
   via a superseding line. Fixture faithfulness rule: timestamps and utterances
   are reconstructed from the provenance source, never tuned to make a model
   look good (the `held_out_cases.go` "DISCIPLINE — faithful, not tuned" comment
   is the governing precedent).
4. **Never prune** — superseded and gate-excluded lines stay forever (INV-2).
