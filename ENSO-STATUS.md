# Ensō — Current Status

*Single source of truth for where we are and what done looks like. Updated 2026-07-11.*
*Authoritative spec: `docs/2026-06-20-enso-unified-spec.md`. ADRs: `docs/`.*

---

## Architecture at a glance

| Phase | Piece | Fixes | Status |
|---|---|---|---|
| P0 | **Trigger** — active-memory plugin | #1 silent non-retrieval | ✅ Live since Jun 17 |
| P1 | **Corpus** — structured Markdown format | #2 unauditable consolidation, partial #4 staleness | 🔨 Next |
| P2 | **Index** — KùzuDB graph plugin in `memory` slot | #3 vocabulary drift, #4 staleness at retrieval | ⬜ Blocked on P1 |
| P3 | **Texture** — leaky-integrator decay | ranking quality, temporal texture | ⬜ Blocked on P2 |

---

## Phase 0 — LIVE ✅

Turned on the stock `active-memory` plugin (Jun 17). Benchmarking flat-file recall.
This is the **control group**: every later phase must beat it or it doesn't ship.

Benchmark log: `docs/2026-06-17-phase0-benchmark.md`

---

## Phase 1 — Structured Markdown Corpus 🔨

**What it is:** Start writing memories in a structured, parseable, append-only format with supersession conventions and reserved temporal fields. The format is the durable artifact — retrofit is expensive, so we spec it to the millimetre now.

**Target:** The corpus active-memory already searches becomes structured, auditable, and supersession-aware — without a graph yet.

### Definition of done

- [ ] Matt has ratified the entry grammar (§3 of tech spec) and open decisions below
- [ ] First batch of real structured entries written (starting with today's key decisions)
- [ ] Parser round-trips a representative sample losslessly (`parse(serialize(e)) == e`)
- [ ] Reserved P3 fields (`last_ref_time`, `S_last`, `S_floor`, `lambda`, `S_cap`) present in the format with sane defaults — even though inert until P3
- [ ] `go test ./...` green including mdstore round-trip tests
- [ ] 1 week of stable grammar (no churn) + enough entries that a graph index would be worth building
- [ ] P1 exit decision: does structured Markdown + active-memory already beat the P0 baseline? If yes, **pause** — let P2 earn its keep rather than build it reflexively

### Open decisions (need Matt's call before building)

| # | Decision | Lean | Notes |
|---|---|---|---|
| **S-1** | Entry location: inline in `memory/YYYY-MM-DD.md` vs dedicated `memory/structured/` store | **Inline (a)** | Capture fidelity — fewer places = more likely to actually write entries. Final call at P1 start. |
| **S-schema** | Ratify the entry grammar as written in tech spec §3 | Pending | Matt must sign off. The format is permanent; changing it later requires a migration. |
| **S-reserved** | Init values for `S_last`, `S_floor`, `lambda`, `S_cap` per node type | Placeholders ok | Values get tuned in P3; we just need the keys present with any defensible default. |

### Key invariants (non-negotiable, from unified spec §1)

- **INV-1:** Markdown is canonical and lossless. The graph (P2) is a derived cache. Memory survives without it.
- **INV-2:** Append-only. Supersession flags entries; it never deletes them. "What did Dross know about X, and when?" must always be answerable.
- **PORT-INV:** `git clone` + no framework = human can reconstruct complete state. Framework-meaningful fields go in adapters, never the substrate.

---

## Phase 2 — KùzuDB Graph Plugin ⬜

**Blocked on:** P1 grammar ratified + stable corpus exists to index.

**Target:** Replace the `memory` slot with a KùzuDB-backed plugin that does graph traversal instead of flat-file search.

### Definition of done

- [ ] KùzuDB plugin registered in `plugins.slots.memory`
- [ ] Plugin parses P1 Markdown corpus → graph nodes/edges on startup
- [ ] `memory_recall` returns results with supersession filtering (stale entries suppressed)
- [ ] **Beats the P0 baseline** on real recall cases — specifically: connected-fact retrieval (#3) and staleness suppression (#4) — without inflating noise rate
- [ ] Graceful degradation: plugin quarantine falls back to flat-file search cleanly

**Gate:** Does not ship unless it beats P0. No exceptions.

---

## Phase 3 — Leaky-Integrator Decay ⬜

**Blocked on:** P2 benchmark data exists.

**Target:** Turn the graph into a temporally textured one — recency and importance as first-class ranking signals.

### Definition of done

- [ ] `StrengthAt` / `BumpOnRecall` wired into P2 recall ranking (math already written in `internal/core/recall.go`)
- [ ] Spacing-aware bump (`α_eff`) implemented first (highest engineering value)
- [ ] Recall bump fires only on RECALL-DEF: surfaced AND materially used in a reply — not just returned by search
- [ ] Runaway Hebbian feedback tested: `S_cap` ceiling + novelty bonus prevent rich-get-richer monopoly
- [ ] **Beats the P2 baseline** on ranking quality metrics

---

## Codebase state (as of 2026-07-11) — WP-0 + WP-1 COMPLETE

**WP-0 closed:** `go build ./...` was broken because `internal/bench` referenced deleted symbols (`Entry.Correct`, `core.Correction`, `CorrectRestate`, `CorrectReframe`, `DetectCorrection`, `confirm.Propose`). Fixed by:
- Rewrote `bench/cases.go` and `bench/held_out_cases.go` to build supersession triples directly via `core.NewEntry` + `Entry.Supersede`. All case semantics and timestamps preserved.
- Rewrote `bench/held_out_test.go`: deleted `TestHeldOut_DetectorGeneralizes` (dead — detection layer gone); kept `TestHeldOut_RecallGeneralizes` (generalization gate, still passes 2/2).
- Pruned dead symbol references from comments in `bench/corpus.go`, `bench/mutation_test.go`, `bench/cases.go`.
- `make check` + `make test-race` green. Ensō 2/2 STALE seed + 2/2 held-out. Capture is load-bearing (0.50 without edges). NEIGHBOR documented 0/1 as expected.

**State of codebase:**
- **Kept:** entry model + store port + mdstore adapter (P1 infrastructure), decay math + ranking (P3 math), benchmark harness with real cases
- **Deleted (Jul 8):** detection/correction layer (`core/correction.go`, `core/detect.go`, `core/contradict.go`, `internal/confirm/`), fabrication probes, synthetic expectations, harvest harness
- **Resolved gap (verified 2026-07-11):** reserved P3 fields (`last_ref_time`, `S_last`, `S_floor`, `lambda`, `S_cap`) ARE present and mutually consistent across the golden file, `marshal.go`, `parse.go`, and `core/types.go`. No work needed — this open-gap note is retired.

**Current WP: WP-5 OPEN** (2026-07-18, Matt's lock-override). WP-4 closed 2026-07-18 — **GATE PASSED** (verdict below). WP-3 closed 2026-07-18. WP-1 closed 2026-07-11. WP-2 closed 2026-07-12; P1 exit measured (pass) 2026-07-13/14.

## WP-4 CLOSED — internal vectors + benchmark gate (2026-07-18) — **GATE VERDICT: PASS**

**Gate numbers (`TestWP4VectorGate`, 79-case git-history real-miss corpus, RH-3 hard gate):**

| Pipeline | P@1 | Mean noise-above | Stale surfaced |
|---|---|---|---|
| **Recall v2 (vector → traversal → supersession → rank)** | **79/79 = 1.00** | **0.000** | **0** |
| Baseline (i) naive recency | 50/79 = 0.63 | 0.367 | — |
| Baseline (ii) flat lexical search (`memory_search`-equivalent) | 45/79 = 0.57 | 0.430 | — |

Recall v2 beats both baselines on staleness suppression without inflating noise — the
WP-4 merge condition, met and recorded. **Doorfinder honesty metric:** the correct entry
is a *seed* on only 29/79 cases lexically; vectors raise that to 37/79 — **8 real
no-lexical-overlap cases found by the vector door alone** (P@1 parity would have hidden
that those cases were previously carried by decay coincidence, not by finding the door).

**ADR-002 ratified & written** (`docs/adr/ADR-002-vector-engine.md`, added to the drift
table + §9/§10, hashes re-pinned): KùzuDB is the single engine — embeddings stored as
node properties, exact cosine in the adapter; KùzuDB's ANN index (VECTOR extension,
verified statically linked, no network fetch) deferred per RH-2 until a latency case is
logged. Embedding source gemini-embedding-001 (`GeminiEmbedder` prod / `MapEmbedder`
deterministic-local). **Degradation contract test-pinned:** recall-time outage returns
byte-identical WP-3 lexical+traversal results with `Mode=degraded` + surfaced error
(`TestVectorOutageDegradesToLexical`); append-time outage stores the record vectorless
(durability outranks index quality; rebuild re-embeds). Kill-the-graph still green with
vectors (`TestVectorRebuildDeterministic` — vectors are derived, INV-1).

**DoD:** ✅ ADR-002 written + ratified before implementation · ✅ gate beaten on both
baselines, numbers recorded · ✅ provider-outage degradation green · ✅ kill-the-graph
with vectors green · ✅ gate verdict recorded here · ✅ `make check` + `make test-race`
green. **Budget:** ~220 production LoC vs +500 — in budget. No new dependencies.

## WP-3 CLOSED — KùzuDB graph store adapter (2026-07-18)

**Verdict:** the graph adapter is live behind the unchanged `core.Store` port and meets
every DoD box on the real corpus. `internal/graphstore` implements the port on embedded
KùzuDB (pinned `github.com/kuzudb/go-kuzu v0.11.3`, the allowlisted binding): six node
tables + Entity + four rel tables generated from the core enums (tech spec §6);
append-only record fidelity via a global `seq` key (supersession re-append copies coexist
exactly as in Markdown); rebuild is a pure function (`OpenRebuilt`: corpus → graph,
deterministic + idempotent, test-pinned); write path is log-first (`LogFirst`: Markdown
first, graph failure = typed `GraphLagError`, never a lost write); recall v1 =
lexical seeds (shared `core.Tokenize`/`Specificity` — no Cypher re-implementation)
→ 1–2-hop Cypher traversal over RELATES_TO/OWNS/ABOUT → supersession/staleness filter →
`core.RankBySpecificity`, in a two-tier order where edge-connected entries outrank
unconnected noise (the connected-fact fix made observable) and tiers collapse to exact
P1 ranking when no edges apply (parity by construction).

- **DoD:** ✅ shared `core.Store` conformance suite (`internal/storetest`, new) passes for
  mdstore + memstore + graphstore · ✅ deterministic rebuild test-pinned · ✅ kill-the-graph
  drill green (delete db → rebuild from Markdown → recall identical; INV-1 proven) ·
  ✅ supersession gate on the REAL corpus: `TestWP3GraphSupersessionGate` = **79/79 = 1.00,
  zero stale surfacings** (parity with the Jul-14 full-pipeline bar; recovers all 34
  same-subject cases specificity tops out at 0.57 on) · ✅ traversal reaches the real
  2026-06-23 enso-repo-path NEIGHBOR miss via its real OWNS edge (n ≥ 1 per Matt's signed
  call), plus a fixture-level proof that a connected child outranks fresher unconnected
  noise · ✅ `git diff --stat internal/core/` empty · ✅ `make check` + `make test-race` green.
- **Budget:** 699 non-comment production LoC (960 physical) vs +900 — in budget. Dependency:
  go-kuzu v0.11.3 (+ transitive uuid/decimal), allowlisted per §3.
- **Learned the hard way (recorded for WP-4):** (1) KùzuDB rel creation requires
  single-label endpoints — resolve per-table, never unlabeled; (2) an embedded database
  per benchmark case must be closed in-loop, not via `t.Cleanup`, or 79 open instances
  exhaust the process; (3) use-after-Close of the cgo handles segfaults — the store now
  guards every op with a closed flag and returns a loud Go error instead.

**Matt's signed calls (2026-07-18, project-completion session):**
1. **WP-3 "proceed anyway" — SIGNED.** P1 exit passed (P@1 1.00 > 0.63), which per WP-2's
   final DoD box stops work pending Matt. Matt explicitly green-lit WP-3 and WP-4 through
   its benchmark gate. (Evidence basis: Jul-14 rebaseline — supersession edge is +0.43
   load-bearing over specificity on 34 same-subject cases.)
2. **WP-5 evidence lock OVERRIDDEN — SIGNED.** The spec locks WP-5 behind DM-days of live
   relevance-drift misses. Matt explicitly chose "everything incl. WP-5": wire the
   RECALL-DEF bump per spec §9 ordering (spacing-aware bump → floor-modulated spike →
   update-on-recall), runaway-Hebbian test, ranking-quality measurement. Power-law tail,
   global normalization, interference/LTD, multi-trace remain out of scope.
3. **ADR-002 RATIFIED — SIGNED.** Vector engine: KùzuDB-native vector index (no second
   dependency). Embedding source: gemini-embedding-001 (already the bench baseline;
   precomputed corpus embeddings live in-repo), degrading to WP-3 lexical+traversal on
   provider outage — never to zero. Recorded as `docs/adr/ADR-002-vector-engine.md`.
4. **WP-3 vocabulary-drift box — SIGNED.** The real 2026-06-23 `enso-repo-path` NEIGHBOR
   miss (real OWNS edge) counts as the n ≥ 1 case for the traversal DoD box, even though
   its query also matches the target lexically — traversal is what solves the documented
   miss, which satisfies the box's intent. No manufactured case needed.

**Environment note (2026-07-18):** `TestP1Exit`/`TestP1ExitSummary` now SKIP loudly when
the live corpus root (`~/.openclaw/workspace` or `ENSO_CORPUS_ROOT`) is absent. Before
this, an absent corpus scored P@1=0.00 and tripped the regression guard, failing
`make check` on every machine but Matt's. The measurement runs at full strength wherever
the corpus exists; the skip names the missing root. Test-only change.

**P1 exit honesty pass (2026-07-13, Dross Hour):** the harness reports P@1=1.00 > 0.63, but a new **supersession-blind contrast** column reveals only **1 of 7 active passes (Granola) actually exercises supersession** — Adam/Ed pass because the correction *out-specifies* the stale entry, so the shipped specificity ranker wins without the filter. Corpus now has 3 real supersession triples (Granola + Adam + Ed, written into `memory/2026-07-13.md`). **Load-bearing conclusion for the WP-3 gate:** the 37% headline gap is vs *recency*; supersession's true marginal lift is over the *specificity* ranker P1 already ships, and shows up only on the specificity-indistinguishable Granola shape. **Before WP-3, re-baseline the 79-case git-history benchmark against `EnsoSpecificityModel` (not `BaselineModel`)** to get that number. Doc: `docs/2026-07-13-p1-exit-supersession-blind-contrast.md`.

**WP-3 GATE MEASURED (2026-07-14, Dross Hour) — the 7-case worry did NOT generalize:** re-baselined the full 79-case git-history corpus against the specificity ranker via new `TestGitHistorySupersessionGate` + `SpecificityBlindModel`. **specificity-only (no supersession filter) = 45/79 = 0.57; full pipeline = 79/79 = 1.00; supersession marginal lift = +0.43 (34 cases).** All 34 losses are same-subject `"current status of X?"` pairs where stale and current describe the *same* project/topic, so specificity scores them equally and provably cannot break the tie — only the `SUPERSEDES` edge/`ValidUntil` can. **Verdict: the graph is NOT gold-plating.** WP-3's acceptance bar on this corpus is now concrete: recover the 34 same-subject supersession cases specificity tops out at 0.57 on. Caveat: the 1.00 assumes the edge already exists (corpus builds it); it proves the filter *uses* the edge correctly, not that the live system *creates* it (capture is ADR-001-deferred, out of scope). Doc: `docs/2026-07-14-wp3-gate-specificity-rebaseline.md`. **Also: the suite caught a real live-corpus parse bug** — `memory/2026-07-13.md` had a malformed uppercase mem: ID that failed the whole daily file (dropping the sensitive comp note + both supersession triples); fixed the slug, corpus loads clean.

**`enso-lint` write-time guard shipped (2026-07-17, Dross Hour):** the malformed-`mem:`-block failure class has now bitten the benchmark THREE times in four days (Jul-13 uppercase ID, Jul-14 same class, Jul-15 missing `encoded_time`) — each dropped a whole daily file's entries from the corpus silently until an accidental benchmark run surfaced it (INV-1 quietly violated: a "written" memory not actually in the canonical corpus). Root cause is *timing*: the daily files live in the workspace repo and get committed without anything on the write path ever calling `mdstore.Load`/`Parse`. Built `cmd/enso-lint` — a fast standalone validator that runs the SAME `mdstore.Parse` (no grammar re-impl, can't drift from `Load`) on specific files or a whole memory dir, reports ALL located failures in one run, exits nonzero on any bad block. Verified: real corpus clean (193 files, 35 entries, 4 edges, 0 failed); a deliberately-broken block caught with `FILE:LINE:msg` + exit 1. `cmd/`-only, zero core/mdstore diff, `make check` green (6 sources in sync), +6 test funcs. Follow-up (ask Matt first, NOT auto-installed): a workspace pre-commit hook that runs `enso-lint -q` on staged `memory/*.md` so the guard fires on every consolidation commit. Doc: `docs/2026-07-17-enso-lint-writetime-guard.md`.

**WP-3 RECALL-BUMP DIVERGENCE (2026-07-16, Dross Hour) — decay is NOT a recency proxy:** the Jul-15 probe ended admitting decay-only and recency both score 0.63 and recover the identical cases (static replay: `LastRefTime`==`EncodedTime`, so strength-order==write-order), naming "build the RECALL-DEF case before the layer" as the seam. Built it: new `RecallBumpModel` (decay strength AFTER applying material-recall bumps) + 3 tests. On a constructed relevance-recall scenario (durable-and-recalled Fact A vs fresh-but-cold Fact B), **recency picks B (wrong), decay-without-bump ALSO picks B (control confirms Jul-15 coincidence), decay+12 spaced recalls picks A (S=0.7557 > 0.3361, RIGHT).** Divergence attributable to the bump alone (no-op-without-events test pins it). Second axis: spacing effect proven separately (3 spaced recalls S=0.307 > 3 massed S=0.219) — information recency structurally cannot encode. **Load-bearing: decay's value over recency is now demonstrated not assumed, and the load-bearing P3 primitive is the RECALL-DEF bump moving `LastRefTime`, not the decay curve (which alone==recency on any never-recalled corpus).** Caveat: n=1 hand-built — proves the divergence is possible + pins mechanism, does NOT prove corpus-scale. **Next seam: a real observed recency-vs-relevance miss from the live Phase-0 log + material-recall telemetry (plugin doesn't emit it yet) before wiring `BumpOnRecall` into production.** Also surfaced+fixed a live-corpus parse bug (`memory/2026-07-15.md` `mem:` block missing required `encoded_time` → failed whole daily file). Doc: `docs/2026-07-16-wp3-recall-bump-divergence.md`. Bench-only, `make check` in sync. Enso `65dc196`.

**WP-3 DECAY EDGE-INDEPENDENCE PROBE (2026-07-15, Dross Hour) — separated the two mechanisms the 1.00 conflates:** the Jul-14 gate proved the edge is +0.43 load-bearing but scored the full pipeline at 1.00 with the edge pre-built by the corpus; it never isolated **decay**, which is *edge-independent* (`StrengthAt` reads only `LastRefTime`, no capture needed). New `DecayBlindModel` + `TestWP3DecayEdgeIndependentContribution` measure it: **decay-only = 50/79 = 0.63, edge-free.** Of the 34 same-subject cases specificity can't break, **decay-only RECOVERS 7 (distinct-date pairs) for free from timestamps alone; the remaining 27 are ALL same-day supersessions** (`stale_date==current_date` → identical `LastRefTime` → decay provably powerless, only the edge breaks the tie). **Load-bearing conclusion:** P3 decay is NOT just a tiebreaker or gold-plating — it independently closes the distinct-date staleness class; the edge's *irreducible* contribution is the **27 same-day flips**, which is the precise, minimal capture bar (ADR-001 deferred layer earns its cost specifically on same-day status flips, not the broad "any stale belief" class). Three-way WP-3 scoreboard: specificity 0.57 → +decay(edge-free) 7 → +edge(capture) 27 → 1.00. **Next seam:** recency/decay coincide on this replay (no material-recall bump fires); showing decay's value *over* recency needs a RECALL-DEF bump case — build the case before the layer. Doc: `docs/2026-07-15-wp3-decay-edge-independence-probe.md`. Bench-only, `make check` in sync.

**`provenance` call signed (Matt, 2026-07-10):** remove it. No production consumer exists; synthetic/real separation is already enforced at the bench level (separate RealCases vs. SyntheticExpectations buckets). YAGNI. Remove from golden file, marshal tests, and any doc references.

---

## WP-1 CLOSED — format reconciliation & documentation hygiene (2026-07-11)

**Verdict:** All three WP-1 items complete; DoD green.
1. **`provenance` field** — removed from `mdstore` marshal/parse + golden file (commit `ec2c3f1`, Matt's Jul-10 call). Verified: zero `provenance` references remain in `internal/` or the tech spec. AMEND-1 restored (field exists in all four surfaces or none — it's in none).
2. **README repair** — the layout `internal/bench`/`cmd/enso` rows, the `confirm` note, the entire Status section, and Next steps were rewritten to post-`cd8e1a2` / ADR-001 reality. The dead detect→resolve→commit `Propose`/`endtoend_test`/reframe-extractor narrative is gone; replaced by the surviving claim (supersession-aware ranking beats naive recency on real misses) + ADR-001 b′ deferral of the detection layer + WP-ordered Next steps. Verified: zero deleted-symbol references remain in README.
3. **Drift table** — added `2026-07-07-mnemosyne-prior-art-comparison.md` and `adr/ADR-001-scope-ratification.md` as tracked contract sources in `scripts/enso-spec-drift.sh` (SOURCES array), unified-spec §9 (delegated-depth list), and §10 (pinned sha256 table). `make drift` IN SYNC across all 6 sources.

**DoD:** ✅ field consistency (golden/marshal/spec/round-trip) · ✅ `parse(serialize(e))==e` + unknown-key preservation green · ✅ README has no deleted-symbol refs · ✅ `make drift` green with expanded §10 · ✅ `provenance` call signed & recorded. `make check` + `make test-race` green. LoC (code): ±0 (docs + one shell SOURCES line; within the ±60 budget).

**Next (WP-2, needs Matt at open):** ratify grammar frozen (S-schema) + S-1 inline; harden `mdstore.FSStore` for prose-interleaved entries with loud errors; supersession-append ceremony + single-writer lock; `cmd/enso-append`; format README; ≥10 real entries + ≥1 real supersession triple; then the P1 exit measurement (does structured corpus already beat the P0 flat-file baseline?).

**WP-2 opening brief ready (2026-07-11, Dross Hour):** `docs/2026-07-11-wp2-opening-brief.md` — the ratification packet for the Jul-13 review. Consolidates the three sign-offs (S-schema grammar-freeze verbatim-as-shipped, S-1 inline, S-reserved placeholders-stand) into a ~15-min decision, surfaces the one open question (Q-A: type-enum tolerance rule = loud warning vs hard error), lays out the +600-LoC build order, a real first-supersession-triple candidate (Granola keep→uninstall), and a pre-flight checklist. Zero production code touched (RH-1/RH-4). Read it first at the review.

**Q-A resolved into a ~2-min confirm (2026-07-12, Dross Hour):** `docs/2026-07-12-wp2-qa-type-enum-tolerance.md` — grounds Q-A in what the code actually does today. Finding: the current parser **hard-rejects** an unknown `type` (`Entry.Validate` → `ParseError` at `parse.go:236` / `types.go:179`), so the brief's "loud warning" lean is a *loosening*, not the status quo, and "consistent with unknown keys preserved" is a false analogy (unknown *keys* → `Extra` soft; unknown *type values* → hard reject, because `type` is load-bearing for decay/specificity ranking). Revised recommendation: **KEEP the hard error** — under a closed-set, self-authored, single-writer, append-only corpus, loud-on-write is the stronger forward-compat story (enum extension = deliberate 2-line change vs. a typo silently mis-ranking a memory). Also flags the one missing test to add in WP-2 either way (no parser-level unknown-type test exists yet). Zero production code touched.

---

## Phase gate rule

*Each phase must beat the prior benchmark or it doesn't ship.*
*Build the house before the roof. Don't gold-plate the roof we haven't reached.*
