# Ensō — Development Specification v1.0

**Date:** 2026-07-09
**Status:** DRAFT — effective upon ratification of ADR-001 at the Jul-13 review
**Audience:** the implementor (human or model). This document is written to be
executed by an implementor with *less context than its authors*. Where this spec
is silent, the implementor does not improvise — see §2, guard RH-10.
**Normative references (in the repo, `docs/`):** unified spec (build contract),
tech spec (grammar/mechanism), design doc (rationale), hexagon doc (portability),
Phase-0 benchmark doc, Mnemosyne prior-art doc, neuro-grounding doc, ADR-001.
**Precedence:** ADR-001 > unified spec > tech spec > design doc. Newer supersedes
older on conflict. This spec adds *work packaging and discipline*; it introduces
no new architecture.

---

## 0. Scope of this specification

Ensō is a **complete, framework-blind memory replacement** (ADR-001, scope b):
a structured-Markdown substrate (canonical), a Go core library (reference
behavior), a KùzuDB graph index with internal vectors (derived), and disposable
per-framework adapters. This spec covers **work packages WP-0 through WP-4**.
Phase 3 activation (WP-5) is *defined but locked* — see §9.
*(Amended 2026-07-18: WP-5's lock was explicitly overridden by Matt and WP-5
closed; WP-6 — the ADR-001 b′ capture-detection restoration — was added as §11
with Matt's scope sign-off. Both events are recorded in `ENSO-STATUS.md`.)*

**This spec's one-sentence success condition:** on a real recall case, Ensō
surfaces the current, connected, correctly-ranked memory where flat-file search
surfaces a stale or disconnected one — measured, not asserted.

---

## 1. Binding invariants (verbatim from the unified spec; violations fail review)

- **INV-1** — Markdown is canonical and lossless. The graph is a derived,
  rebuildable cache. Nothing is stored *only* in the graph.
- **INV-2** — History is append-only; nothing is ever destroyed. Supersession
  flags; it never deletes. Decay changes *ranking*, never *existence*.
- **PORT-INV** — the memory is fully valuable and reconstructable with zero
  framework present. `git clone` + a human = complete state.
- **AMEND-1** — the on-disk format is a public, documented contract. Every field
  that ships in a file is described in the format spec, or it does not ship.
- **RECALL-DEF** — "recalled" = surfaced AND materially used in a reply. A search
  hit is a non-event for strength purposes.
- **Fail-safe / upgrade-safe** — any bug degrades to current behavior, never
  bricks; no framework fork; core imports no framework types, ever.

---

## 2. Rabbit-hole guards (RH-1 … RH-10)

These exist because this project has *already* documented its failure modes: a
detection layer grew to half the codebase ahead of its substrate (deleted Jul-8),
and a "quick" vocabulary broadening produced a 72% false-positive rate that had
to be walked back (Jul-1 precision doc). The guards below are mechanical, not
aspirational. **The implementor's performance is judged by DoD checkboxes closed
per work package, not by lines of code, novelty, or cleverness.**

- **RH-1 — One work package at a time, in order.** Work outside the currently
  open WP is out of bounds, including "while I'm in here" refactors. If adjacent
  code needs fixing, file a note in `ENSO-STATUS.md` and continue.
- **RH-2 — The n ≥ 1 rule.** No detector, extractor, heuristic, optimization, or
  tunable is built or tuned without at least one *real, documented* corpus case
  it addresses (a `[FLAGGED-MISS]`, a benchmark failure, or a logged outage).
  Synthetic cases justify tests, never features.
- **RH-3 — Benchmark gates are hard gates.** WP-4 does not merge unless it beats
  the baseline defined in its DoD. "It should be better" is not evidence.
- **RH-4 — No speculative surfaces.** No daemon, no MCP face, no dashboard, no
  web UI, no CLI beyond what a WP's DoD explicitly names. A second host must
  exist before its adapter does.
- **RH-5 — No background processes.** All decay/staleness is computed lazily on
  read from stored fields + the clock. The only write triggered by reading is
  the (Phase-3, locked) material-recall bump.
- **RH-6 — Budget tripwires.** If a WP exceeds **1.5× its LoC budget** (per-WP
  below, excluding tests and testdata) or requires a **new third-party
  dependency not on the allowlist (§3)**, STOP. Write up the situation in
  `ENSO-STATUS.md` and escalate to Matt. Exceeding a budget is not failure;
  concealing it is.
- **RH-7 — Format changes are ceremonies.** Any change to the on-disk format
  requires, in one commit: tech-spec §3.1/§6 update, golden-file regeneration
  (`UPDATE_GOLDEN=1`), round-trip tests green, and §10 hash re-pin via
  `scripts/enso-spec-drift.sh`. A format change without all four is a defect.
- **RH-8 — Tunables stay tuned-by-nobody.** Every numeric knob is marked
  `// TUNABLE` and keeps its documented default. Calibration is Phase-3 work
  against a labeled corpus (RH-2 applies). Hand-tweaking constants to make one
  test pass is prohibited.
- **RH-9 — Deleted code returns only by evidence.** The detection/correction
  layer (git history, pre-`cd8e1a2`) is restored only under ADR-001 corollary
  (b′): real misses first, restoration second, and never before WP-4 closes.
- **RH-10 — When the spec is silent, stop.** Ambiguity is resolved by asking
  Matt, not by inventing. Record the question and the answer in
  `ENSO-STATUS.md` so the spec's silence gets patched.

**The boredom clause (read this twice):** the highest-value work in this plan is
WP-0 (fixing a build) and WP-1 (reconciling one field). The most *interesting*
work (graph traversal, decay texture) is deliberately last and gated. If the
implementor notices itself drawn toward recall-ranking experiments, novel
detectors, embeddings research, or "a quick visualization," that pull is the
signal to re-read RH-1 and return to the open checkbox.

---

## 3. Environment and dependency allowlist

- **Language:** Go (module `github.com/clockworksoul/enso`). Core stays
  stdlib-only.
- **Allowlist by ring:**
  - `internal/core/` — **stdlib only.** No exceptions, ever (PORT-INV).
  - `internal/mdstore/`, `internal/memstore/`, `internal/bench/` — stdlib only.
  - `internal/graphstore/` (WP-3) — the official KùzuDB Go binding, pinned.
  - vector supplement (WP-4) — **one** of: KùzuDB-native vector index or a
    sqlite-vec binding. Chosen at WP-4 open, recorded as ADR-002. Not both.
- **Local gate:** `make check` (fmt-check + vet + build + test + spec-drift)
  must be green at every commit. `make test-race` green at every WP close.
- **Anything else is an RH-6 escalation.**

---

## 4. WP-0 — Repair the build (bench refactor)

**Why first:** `internal/bench` still references symbols deleted in `cd8e1a2`
(`core.Entry.Correct`, `core.Correction`, `core.CorrectRestate`,
`core.CorrectReframe`, `core.DetectCorrection`, `confirm.Propose`). The module
does not compile. Nothing else can be verified until it does.

**The work:**
1. In `cases.go` and `held_out_cases.go`, replace each `original.Correct(...)`
   call with direct supersession-triple construction using surviving API:
   `core.NewEntry` for the corrected entry, then
   `closed, edge := original.Supersede(corrected.ID, supersededAt)`. Preserve
   each case's documented timestamps and semantics (the comments describing
   *why* each case exists are load-bearing — keep them, updating only mechanism
   references).
2. In `held_out_test.go`, delete the detector-replay assertions
   (`DetectCorrection`, `confirm.Propose`) — that layer is gone (ADR-001). Keep
   and adapt the *recall-side* assertions: given the pre-built supersession
   triples, the Ensō ranking surfaces the current entry where the naive baseline
   surfaces the stale one.
3. Prune now-dead references in `corpus.go` comments.

**Explicit non-goals:** re-implementing any detection; "improving" ranking;
adding cases; touching `core/` or `mdstore/` production code.

**LoC budget:** net-negative to +150.

**Definition of done:**
- [ ] `go build ./...` and `go test ./...` green; `make check` green; `make test-race` green
- [ ] Zero references to deleted symbols anywhere (`grep -rn 'Correct\|DetectCorrection\|confirm\.' internal/ --include='*.go'` returns only innocent matches)
- [ ] The end-to-end value claim still passes: Ensō ranking beats the naive recency baseline on the real-miss cases (`bench_test.go` equivalent of the former `TestBenchmark_CorrectionCaptureIsLoadBearing`, reframed as capture-as-given)
- [ ] No production (non-test, non-bench) code changed
- [ ] `ENSO-STATUS.md` "Codebase state" section updated; commit message references this WP

---

## 5. WP-1 — Format reconciliation & documentation hygiene

**Why:** AMEND-1 is currently violated (undocumented `provenance` field in the
golden file) and the README documents deleted code as live. Both are
public-contract defects; both are cheap.

**The work:**
1. **`provenance` field decision (needs Matt's one-line call, RH-10):** either
   (a) add `provenance` to tech spec §3.1 grammar + §6 schema table (documented
   values, default, semantics — e.g. `live | imported | synthetic`), or (b)
   remove it from `mdstore` marshal/parse and the golden file. Execute whichever
   Matt picks, per RH-7 ceremony.
2. **README repair:** layout table and Status section reflect post-`cd8e1a2`
   reality (no `confirm/`, no `DetectCorrection`; the "headline" paragraph
   rewritten to the surviving claim: supersession-aware ranking beats naive
   recency on real cases; capture-detection deferred per ADR-001 b′).
3. **Drift table:** add the Jul-7 Mnemosyne doc and ADR-001 to unified-spec §10;
   re-pin all hashes; `make drift` green.

**Explicit non-goals:** grammar redesign; new fields beyond the `provenance`
decision; README marketing polish.

**LoC budget (code):** ±60. (Docs excluded from budget.)

**Definition of done:**
- [ ] Golden file, marshal/parse, tech spec §3.1/§6, and round-trip tests are mutually consistent on every field (a field exists in all four or none)
- [ ] `parse(serialize(e)) == e` property tests green, including unknown-key preservation
- [ ] README contains no reference to deleted packages/symbols
- [ ] `make drift` green with the expanded §10 table
- [ ] Matt has signed the `provenance` call (recorded in ENSO-STATUS.md)

---

## 6. WP-2 — Phase 1 completion: the corpus goes live

**Why:** the format is the durable artifact. This WP takes the proven-in-tests
substrate and makes it the *actual* place memories land, closing the Phase-1
checklist in `ENSO-STATUS.md`.

**Pre-resolved decisions (from the docs' recorded leans; Matt ratifies at WP open):**
- **S-1 (entry location): inline** in `memory/YYYY-MM-DD.md` — structured blocks
  interleaved with prose; the parser skips non-entry content (capture fidelity
  beats parse cleanliness; tech spec §3.5 lean).
- **S-reserved:** existing `DefaultTemporal` / `DefaultRecallParams` per-type
  values stand as `// TUNABLE` placeholders. Not calibrated here (RH-8).

**The work:**
1. Harden `mdstore.FSStore` for the inline decision: parse entries embedded in
   prose files; **loud** errors on malformed entries (log + surface, never
   skip — failure mode #2); unknown keys preserved on rewrite.
2. Single-writer discipline per hexagon Option C: advisory file locking around
   append (stdlib `os` + platform lock or lock-file convention — keep it boring).
3. Append path implements the supersession ceremony exactly (tech spec §3.3):
   new entry → SUPERSEDES edge → re-append old entry with `valid_until` set.
   Never in-place edit history (INV-2).
4. A minimal one-shot ingestion command, `cmd/enso-append` (or a `make` target
   wrapping a small main), that accepts one entry's fields and appends it via
   `FSStore` — the smallest runnable surface that lets real capture start.
   **This is the only `cmd/` allowed in this WP (RH-4).**
5. Write the substrate **format README** (AMEND-1): the grammar, field
   semantics, and supersession convention, readable standalone with no code.
6. Begin real capture: the first batch of genuine structured entries (starting
   with the ADR-001 decisions themselves) written into the corpus.

**Explicit non-goals:** any graph or vector code; recall-quality work; automatic
capture-decision logic (a human/agent decides what to write; this WP only makes
*writing it* reliable); concurrency beyond the single lock.

**LoC budget:** +600.

**Definition of done (mirrors ENSO-STATUS.md P1 checklist, tightened):**
- [ ] Matt has ratified the grammar as frozen (S-schema) and S-1 inline
- [ ] Parser round-trips a representative real sample losslessly, including prose-interleaved files and unknown keys
- [ ] Reserved P3 fields present in every written entry with per-type defaults
- [ ] Malformed-entry handling is loud: a corrupt block yields a surfaced error naming file + line, and no silent skip (test-pinned)
- [ ] Concurrent-append test: two writers, no interleaved/corrupt entries
- [ ] Format README exists; a reader with no repo access can hand-write a valid entry from it alone (Matt spot-checks)
- [ ] ≥ 10 real entries and ≥ 1 real supersession triple exist in the live corpus
- [ ] 1 week of stable grammar (no format churn) before WP-3 opens
- [ ] `make check` + `make test-race` green
- [ ] **P1 exit measurement recorded:** does structured corpus + active-memory already beat the P0 flat-file baseline on the real-miss set? If **yes**, STOP — WP-3 requires Matt's explicit "proceed anyway" (tech spec §3.8: let the graph *earn* its keep)

---

## 7. WP-3 — Phase 2, part 1: the graph store adapter (KùzuDB)

**Opens only when:** WP-2 DoD closed, including the exit measurement, and Matt
green-lights per its final checkbox.

**The work:**
1. `internal/graphstore/`: a second implementation of the existing `core.Store`
   port backed by embedded KùzuDB. The core is not modified — that is the entire
   point of the port (hexagon doc §4). Schema: the six node types + four edge
   types (tech spec §6) as Cypher DDL.
2. **Rebuild is a pure function** (resolves S-5 initial policy): full rebuild
   from the Markdown corpus on open — `markdown → graph`, deterministic,
   idempotent. Incremental sync is *deferred* until full rebuild is measured too
   slow on the real corpus (RH-2: no incremental machinery without a documented
   latency case).
3. Write path: log-first (Markdown append via `FSStore`), then graph upsert;
   a failed graph write leaves the corpus authoritative and is repaired by the
   next rebuild (unified spec §5).
4. Recall v1 (traversal + staleness only): given query terms, match entry nodes
   lexically (existing `Specificity` tokenizer — **no vectors in this WP**),
   traverse 1–2 hops over `RELATES_TO`/`ABOUT`/`OWNS`, filter supersession
   (`valid_until` closed or inbound `SUPERSEDES` ⇒ not current), rank by the
   existing `Rank`/`RankBySpecificity`.

**Explicit non-goals:** vectors/embeddings (WP-4); Neo4j; decay *bump* writes
(Phase 3, locked — reading `StrengthAt` for ranking is already in `Rank` and is
fine); OpenClaw plugin wiring; query-language cleverness beyond the traversal
described.

**LoC budget:** +900.

**Definition of done:**
- [ ] `graphstore` passes the same `Store`-contract test suite as `mdstore`/`memstore` (shared conformance tests — write them if absent, they count in budget)
- [ ] `rebuild(corpus)` is deterministic: two rebuilds from the same corpus yield identical graphs (test-pinned)
- [ ] Kill-the-graph drill: delete the KùzuDB file, rebuild from Markdown, recall results identical (INV-1 proven, not assumed)
- [ ] Supersession filtering: on every real supersession triple in the corpus, the stale entry is never returned as current
- [ ] Traversal demonstrably reaches ≥ 1 real vocabulary-drift case: a query whose terms match only a *neighbor* still surfaces the target via an edge (n ≥ 1 real case per RH-2; if none exists in the corpus yet, this box blocks until one is logged — do not manufacture it)
- [ ] Core untouched: `git diff --stat internal/core/` empty for this WP
- [ ] `make check` + `make test-race` green

---

## 8. WP-4 — Phase 2, part 2: internal vectors + the benchmark gate

**Why:** ADR-001 makes embedding-provider independence a requirement. The vector
index is the *doorfinder* — it finds entry nodes; the graph walks the house. It
lives inside Ensō so a provider quota failure is never again in the critical path.

**The work:**
1. **ADR-002 (one page, Matt ratifies):** vector engine choice — KùzuDB-native
   index vs sqlite-vec sidecar — and the embedding source. The embedding source
   must satisfy: available locally or degradable (a provider outage degrades
   recall to WP-3 lexical+traversal, never to zero — fail-safe invariant).
2. Implement: embed entry `content` at append/rebuild; recall v2 = vector
   match → entry nodes → WP-3 traversal → supersession filter → rank.
3. Provider-outage test: with embeddings unavailable, recall returns WP-3
   results (degrade, don't fail).
4. **Run the gate.** Comparison set: the labeled real-miss corpus (per ADR-001,
   substituting for the instrumentation-gapped Phase-0 log). Baselines: (i)
   naive recency, (ii) flat-file `memory_search`-equivalent lexical search over
   the same corpus.

**Explicit non-goals:** training or fine-tuning anything; multi-provider
embedding abstraction layers; caching frameworks; relevance-feedback loops
(that is Mnemosyne's online-learning idea — roadmap, post-validation only).

**LoC budget:** +500.

**Definition of done:**
- [ ] ADR-002 written and ratified before implementation starts
- [ ] Recall v2 beats both baselines on the real-miss set — specifically on connected-fact retrieval (#3) and staleness suppression (#4) — **without inflating the noise rate** (irrelevant results per query ≤ baseline). Numbers recorded in the benchmark doc
- [ ] Provider-outage degradation test green
- [ ] Kill-the-graph drill still green (vectors are derived too; INV-1)
- [ ] **Gate verdict recorded in ENSO-STATUS.md.** If the gate fails: WP-4 does not merge; findings are written up; work STOPS pending Matt (RH-3). Failing honestly closes this WP's process even though the feature doesn't ship
- [ ] `make check` + `make test-race` green

---

## 9. WP-5 — Phase 3 activation (DEFINED BUT LOCKED)

Recorded so the implementor knows what *not* to start. Opens only when: WP-4
shipped through its gate, **and** DM-days of live data show relevance-drift
misses that supersession alone can't fix (README "Next steps" #5 — that evidence
is the key; no evidence, no WP-5).

When it opens, the order inside is already ranked (neuro doc §4a): the
spacing-aware bump is **already implemented** (`BumpOnRecall`,
`α_eff = α·(1 − e^(−Δt/Tau))`) — WP-5 *wires* it on RECALL-DEF events only,
which requires solving materially-used detection (tech spec S-3, the central
problem); then floor-modulated spike height; then update-on-recall. Power-law
tail and global normalization stay deferred; interference/LTD and context-keyed
multi-trace are **out of scope for v1 entirely**.

---

## 10. Standing out-of-scope list (applies to every WP)

Orchestration/actors/work-queues, P2P networking, CRDT editors, dashboards,
gRPC (the Mnemosyne sprawl — rejected by design); MCP daemon and any second-host
adapter (until a second host exists); OpenClaw core forks; a `Reference` node
type; online-learned ranking weights; NLP mining of prose; background
jobs of any kind; deleting or rewriting corpus history under any justification.

---

## 11. WP-6 — Capture detection restoration (ADR-001 b′) — ADDED 2026-07-18

**Added by amendment after WP-0…WP-5 closed; scope selected by Matt 2026-07-18
(recorded in ENSO-STATUS.md).** This is the RH-9 event: the deleted
detection/correction layer returns, and it returns exactly the way corollary b′
prescribed — real misses first, restoration second, and only after WP-4's gate
closed (it did, 2026-07-18, PASS).

**The evidence basis (n ≥ 1 satisfied four times over):** the four real
correction utterances preserved verbatim on the real-miss cases —
`adam-headcount` ("actually the Adam headcount ask already landed…"),
`ed-sandoval` ("the ball is in Ed's court, not Matt's"), and the two held-out
misses `granola-ban` ("Granola still works and is the transcript source of
record") and `leanctx-scope` ("LeanCTX does more than that now, the note
undersells its current scope") — plus the Jul-1 precision probe (13/18 false
positives on the first-cut vocabulary, hardened to 0/18 before deletion).

**The work (restore the post-Jul-1 HARDENED versions from git history, adapted
to the surviving API):**
1. `core/detect.go` — the lexical sensor (`DetectCorrection`): the
   precision-hardened signal vocabulary, detect-don't-decide, audit-trail
   signals. The `Correction`/`Entry.Correct` chokepoint does NOT return; the
   surviving supersession ceremony (`NewEntry` + `Supersede` / `FSStore
   .Supersede`) is the only committed path.
2. `core/contradict.go` — the resolver-side contradiction check
   (`DetectContradiction`): three-evidence rule (utterance affirms; STORED
   entry negates; shared subject tokens), for the bare-reaffirmation class the
   lexical sensor deliberately cannot catch.
3. `core/propose.go` (new, small) — `ProposeSupersession`: utterance + loaded
   corpus → a ranked, evidence-named proposal of WHICH current entry is
   contradicted/corrected. Pure function; never writes. The operator (human or
   host policy) confirms and executes the ceremony.
4. Tests restored/adapted: the detection table, the 18-sentence FP probe
   (locked), contradiction tests, and a NEW end-to-end loop in `bench`: real
   utterance → proposal → confirm → ceremony → graph recall surfaces the
   current entry. This closes the Jul-14 caveat ("proves the filter *uses* the
   edge, not that the live system *creates* it") — capture now creates the edge.

**Explicit non-goals:** `internal/confirm/` (an approval surface is host-side;
RH-4); auto-commit under any confidence (no reconsolidation — a wrong write is
permanent); fabrication probes / harvest harness (stay deleted); NEIGHBOR/NOISE
detection (no utterances exist for them — RH-2); semantic/embedding detection;
vocabulary broadening beyond the restored hardened set without a new real case.

**LoC budget:** +800 (restoration counts as new lines).

**Definition of done:**
- [ ] All 4 real utterances detected AND resolved to their documented stale entry by `ProposeSupersession` over their case corpora
- [ ] The Jul-1 FP probe locked: 0/18 innocent sentences fire, at BOTH the sensor and the proposal level
- [ ] End-to-end capture loop green: proposal → confirmed ceremony → the WP-3/4 graph recall surfaces the current entry (edge created by capture, not by fixture)
- [ ] No auto-commit path exists (test-pinned: proposing writes nothing)
- [ ] `make check` + `make test-race` green; verdict in ENSO-STATUS.md

## 12. Reporting cadence

At each WP close: update `ENSO-STATUS.md` (checkboxes + one-paragraph verdict),
run `make check` + `make test-race`, re-pin drift hashes if any `docs/` source
changed, and commit with the WP identifier in the message. Between closes, any
RH-6/RH-10 stop is written up the day it happens. Silence is the one failure
this project was built to prevent; do not exhibit it.

---

*Build the house before the roof. The fun parts are behind gates because the
gates are what make the fun parts trustworthy.*
