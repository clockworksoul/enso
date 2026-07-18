# ADR-002 — Vector Engine and Embedding Source (WP-4)

**Date drafted:** 2026-07-18
**Status:** RATIFIED — Matt Titmus, 2026-07-18 (project-completion session; recorded in `ENSO-STATUS.md`)
**Deciders:** Matt Titmus (ratifier), Claude (author)
**Related:** `docs/adr/ADR-001-scope-ratification.md` (vector matching moves *inside* Ensō);
`docs/enso-development-spec.md` §8 (WP-4 requires this ADR before implementation);
`internal/graphstore/vectors.go` (implementation)

---

## Context

WP-4 adds the internal vector supplement: the *doorfinder* that locates entry
nodes whose vocabulary the query misses; the graph walks from there. ADR-001
requires embedding-provider independence — a provider quota failure must never
again sit in the critical path. The dev spec (§3, §8) allows exactly one
engine: **KùzuDB-native vector index** or **a sqlite-vec binding** — not both —
and requires the embedding source to be *available locally or degradable*
(outage ⇒ recall degrades to WP-3 lexical+traversal, never to zero).

## Decision

**Engine: KùzuDB — one storage engine, no sidecar.** Entry-content embeddings
are stored as node properties in the same embedded KùzuDB database that holds
the graph (derived data; INV-1: killed and rebuilt with the graph, present
nowhere in the Markdown substrate). Similarity is **exact cosine over the
candidate set**, computed in the adapter.

- The KùzuDB **ANN index** (`CREATE_VECTOR_INDEX`, via the VECTOR extension —
  verified statically linked into the pinned go-kuzu v0.11.3 binding, so it
  loads with **no network fetch**) is **deferred** under RH-2: no documented
  latency case exists, the current corpus is ~10² vectors where exact scan is
  microseconds, and an ANN index adds a rebuild/update lifecycle with zero
  measured benefit today. When a latency case is logged, the upgrade is
  internal to `graphstore` and invisible at the port.
- sqlite-vec is rejected: it would be a second storage engine and a second
  pinned dependency purely to hold data KùzuDB can already hold.

**Embedding source: `gemini-embedding-001`**, the model already used to build
the benchmark's semantic baseline (pre-computed corpus embeddings live in
`internal/bench/testdata/git_history_embeddings.jsonl`). Two implementations of
the `graphstore.Embedder` port:

- `GeminiEmbedder` — production path, stdlib HTTP.
- `MapEmbedder` — deterministic local table (pre-computed corpus embeddings,
  tests). Also the recommended shape for any future local/offline model.

**Degradation contract (the load-bearing clause):**

- *Recall-time outage:* the query cannot be embedded ⇒ recall returns exactly
  the WP-3 lexical+traversal result, with `RecallResult.Mode = degraded` and
  the error surfaced in `RecallResult.Degraded`. Test-pinned
  (`TestVectorOutageDegradesToLexical`).
- *Append-time outage:* the record is stored **without** a vector — durability
  outranks index quality (same principle as the log-first write path); the next
  rebuild re-embeds. A vectorless record remains fully lexically recallable.
- Embeddings never gate correctness: supersession filtering and ranking are
  unchanged core logic; vectors only widen the seed set.

## Consequences

- One engine, one pinned dependency ring (`graphstore` = go-kuzu + stdlib).
- Provider outages degrade recall quality, never availability or correctness
  (fail-safe invariant holds; measured: degraded mode is byte-identical to the
  WP-3 pipeline).
- Gate evidence (2026-07-18, `TestWP4VectorGate`, 79-case real-miss corpus):
  recall v2 P@1 = **1.00** vs naive recency **0.63** and flat lexical **0.57**,
  mean noise-above 0.000, zero stale surfacings; doorfinder coverage rises from
  29/79 (lexical-only) to 37/79 with vectors — **8 real no-lexical-overlap
  cases** are found by the vector door alone.
- The knobs (`vectorSeedK = 5`, `vectorMinSim = 0.60`) are `// TUNABLE`
  Phase-1 priors, uncalibrated by design (RH-8).
