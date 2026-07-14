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

**Current WP: WP-2 CLOSED** — Phase 1 corpus is live. WP-1 closed 2026-07-11. WP-2 closed 2026-07-12.
**Next: P1 exit measurement** — does structured corpus + active-memory beat the P0 flat-file baseline?

**P1 exit honesty pass (2026-07-13, Dross Hour):** the harness reports P@1=1.00 > 0.63, but a new **supersession-blind contrast** column reveals only **1 of 7 active passes (Granola) actually exercises supersession** — Adam/Ed pass because the correction *out-specifies* the stale entry, so the shipped specificity ranker wins without the filter. Corpus now has 3 real supersession triples (Granola + Adam + Ed, written into `memory/2026-07-13.md`). **Load-bearing conclusion for the WP-3 gate:** the 37% headline gap is vs *recency*; supersession's true marginal lift is over the *specificity* ranker P1 already ships, and shows up only on the specificity-indistinguishable Granola shape. **Before WP-3, re-baseline the 79-case git-history benchmark against `EnsoSpecificityModel` (not `BaselineModel`)** to get that number. Doc: `docs/2026-07-13-p1-exit-supersession-blind-contrast.md`.

**WP-3 GATE MEASURED (2026-07-14, Dross Hour) — the 7-case worry did NOT generalize:** re-baselined the full 79-case git-history corpus against the specificity ranker via new `TestGitHistorySupersessionGate` + `SpecificityBlindModel`. **specificity-only (no supersession filter) = 45/79 = 0.57; full pipeline = 79/79 = 1.00; supersession marginal lift = +0.43 (34 cases).** All 34 losses are same-subject `"current status of X?"` pairs where stale and current describe the *same* project/topic, so specificity scores them equally and provably cannot break the tie — only the `SUPERSEDES` edge/`ValidUntil` can. **Verdict: the graph is NOT gold-plating.** WP-3's acceptance bar on this corpus is now concrete: recover the 34 same-subject supersession cases specificity tops out at 0.57 on. Caveat: the 1.00 assumes the edge already exists (corpus builds it); it proves the filter *uses* the edge correctly, not that the live system *creates* it (capture is ADR-001-deferred, out of scope). Doc: `docs/2026-07-14-wp3-gate-specificity-rebaseline.md`. **Also: the suite caught a real live-corpus parse bug** — `memory/2026-07-13.md` had a malformed uppercase mem: ID that failed the whole daily file (dropping the sensitive comp note + both supersession triples); fixed the slug, corpus loads clean.

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
