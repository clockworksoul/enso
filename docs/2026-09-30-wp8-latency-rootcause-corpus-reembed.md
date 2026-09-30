# WP-8 blocker (b) — the vector-mode latency case is NOT the query embed (2026-09-30, Dross Hour)

*Status going in: WP-8 blocked on two narrowed blockers. (a) co-key + fill `used` —
co-keying done Sep 29. (b) "query-embedding latency (16-20s wall, 4-5x over the 4s
deadline) before any fresh vector-mode window." This pass attacks (b) and finds the
Sep-24 diagnosis of it was wrong.*

## TL;DR

The 16-20s vector-mode wall time is **real and reproduced (20.2s today)** — but it is
**not** "almost entirely the synchronous Gemini query-embedding round-trip" as the
2026-09-24 analysis inferred. It is **`enso-recall` re-embedding the entire corpus from
scratch on every single invocation**, because the binary rebuilds a fresh in-memory
graph (`dbPath=""`) with the embedder attached, and append-time embedding calls Gemini
once per entry, sequentially.

- Query embed alone: **0.38s** (direct curl, HTTP 200, `gemini-embedding-2`).
- Corpus re-embed at rebuild: **19,274 ms** (68 entries × ~275ms sequential).
- The recall step itself (incl. the single query embed): **320 ms**.
- Same rebuild with the embedder OFF: **51 ms**.

**A persisted index makes a warm recall 461 ms total — a ~40x speedup, comfortably under
the 4s deadline.** The fix was already anticipated by the architecture (embeddings are
derived data meant to live in the KùzuDB index); `enso-recall` just never uses the
persisted path.

This is a textbook stale-assumption / shared-premise trap — exactly the failure class
Ensō exists to catch. The Sep-24 measurement was honest but reasoned from CPU% ("~7% CPU,
therefore it's I/O-bound on the one embed call") to the wrong culprit; it never isolated
the phases.

## Reproduction (all today, 2026-09-30, live corpus at `~/.openclaw/workspace`, 269 daily files / 68 entries)

### 1. Direct query embed is fast
```
POST .../gemini-embedding-2:embedContent  ->  HTTP 200  total=0.384s
```
So `gemini-embedding-2` is healthy and fast. The `001->2` retirement fix (`dae4f48`) is
holding. This already rules out "the query embed is the 16-20s."

### 2. `enso-recall` in vector mode: 20.2s wall, 6% CPU
```
time enso-recall -root ~/.openclaw/workspace -query "...omega livetopology..." -k 5
  -> mode: vector, results: 5
  -> 0.82s user 0.39s system 6% cpu  20.223 total
```
1.2s of CPU inside 20.2s of wall = ~19s spent waiting on the network, but at 6% CPU it is
clearly many small blocking calls, not compute. That is the tell the Sep-24 pass had too;
the missing step was attributing *which* calls.

### 3. Phase-timed probe (throwaway `cmd/zztmp-latprobe`, since deleted) pins it exactly
```
PHASE load:                45.0 ms   (68 entries, 4 edges)
PHASE rebuild+embed:    19273.9 ms   <-- corpus embed at append time
PHASE recall:             319.6 ms   (mode=vector, results=59)
PHASE rebuild(noemb):      51.4 ms   <-- same rebuild, embedder OFF
TOTAL:                  19714.3 ms
```
`rebuild+embed` is 99% of the wall time. `recall` — which contains the one query embed the
Sep-24 doc blamed — is 320ms.

### 4. Persisted-index warm path: 461 ms
```
COLD build+embed+persist: 18691.9 ms   (one-time, if ever)
WARM open(persisted):         43.7 ms
WARM recall:                 417.2 ms   (mode=vector, results=59)
WARM TOTAL (open+recall):    460.9 ms
```
Reopening the on-disk KùzuDB index (which already holds every entry's stored embedding as
a node property, per WP-4/ADR-002) skips the re-embed entirely. 461ms end-to-end.

## Why this happens (code path)

- `cmd/enso-recall/main.go` calls `graphstore.OpenRebuiltWith(ctx, "", emb, entries, edges)`
  — note `dbPath=""`, a **fresh in-memory graph every call**.
- `OpenRebuiltWith` attaches the embedder, then `g.Append(ctx, entries, nil)`.
- `graphstore.go:271`: `Append` calls `g.embedder.Embed(ctx, e.Content)` **once per entry,
  in a `for _, e := range entries` loop** — 68 sequential Gemini round-trips.
- The comment on `OpenRebuilt` says "a full rebuild of a real corpus is measured in
  milliseconds at current scale." That is true **without** an embedder (51ms measured) and
  was the WP-3 reality; it silently stopped being true the moment WP-4 attached a network
  embedder to the same append path. Nobody re-measured the rebuild cost with embeddings on
  in the one-shot binary — the WP-7 latency datum (474ms) was taken with the 404'd
  embedder, i.e. effectively lexical, so it never saw this.

## The fix options, in preference order

1. **Persist the index and reuse stored embeddings (recommended).** Point `enso-recall` at
   an on-disk `<root>/index.kuzu`, rebuild only when the corpus is newer than the index
   (mtime check), and on the warm path just `Open` + recall. Measured warm total: **461ms.**
   Embeddings are already persisted as node properties, so no new storage design is needed —
   this is using the persisted path that already exists. Cold build still pays the ~18s once
   (or whenever the corpus changes materially); see #2 to shrink that too.

2. **Batch the append-time embeds.** `cmd/embed-corpus` already uses
   `batchEmbedContents` (up to 100 texts/call) — the append path does not. Even keeping the
   per-call rebuild, batching 68 entries into one request would cut the cold ~18s toward
   ~1-2s. This helps the cold-build cost in #1 and is independently worth doing; the
   `Embedder` interface would need a `BatchEmbed([]string)` method (or a batch-aware append
   loop).

3. **Take the embed off the per-call critical path entirely (sidecar).** WP-7 deliberately
   deferred a sidecar "until a real latency case is logged (RH-2)." This is now a logged
   case — but #1 already brings warm recall to 461ms with a boring one-shot binary, so the
   sidecar is still not justified. Persisting the index is the cheaper answer to the same
   RH-2 case. Keep the sidecar deferred.

## What this changes for WP-8

- Blocker (b) is **solved in principle and measured**: warm vector recall is 461ms, ~9x
  under the 4s deadline. The buildable fix is "persist the index in `enso-recall`" (option
  1), optionally plus "batch the append embeds" (option 2) to make cold builds cheap. Both
  are host-adapter/bridge changes, no `internal/core` or corpus-format change.
- A fresh vector-mode shadow window will **not** be all timeouts once `enso-recall` reuses a
  persisted index — reversing the Sep-24 conclusion that "the moment the vector path is
  exercised it will breach the deadline."
- Blocker (a) — co-key (done Sep 29) + fill `used` from a material-use signal (still blocked
  on a host RECALL-DEF event) — remains the true long pole. Latency (b) was the smaller of
  the two, and is now the smaller *and* answered.

## Scope of this pass

- **Read-only.** No production code changed. No corpus writes (verified: `~/.openclaw/
  workspace/memory/` git status shows only the unrelated nightly dreaming-pipeline files).
- The phase-timing probe was a throwaway `cmd/zztmp-latprobe`, created and deleted within
  this pass; enso working tree is clean.
- No fix committed. The recommendation (persist the index in `enso-recall` + batch appends)
  is a real code change to the WP-7 bridge and should be its own reviewed change, not
  slipped in under a diagnosis pass — and the append-loop batching touches the `Embedder`
  interface, which deserves Matt's eyes. Stopped at the seam.

## Honest caveats

- 68 entries today. The per-entry cost (~275ms) scales linearly, so the cold re-embed gets
  *worse* as the corpus grows — which makes persisting the index more urgent over time, not
  less, and makes "rebuild is milliseconds" increasingly wrong in the binary.
- The 461ms warm number is a single measurement on a warm-network machine; treat it as
  "clearly sub-second and clearly sub-deadline," not a calibrated SLA. The point is the
  order of magnitude, which is not in doubt (461ms vs 18,692ms).
- Persisting the index reintroduces a cache-staleness question (when is the index stale vs
  the corpus?) that the pure-rebuild-every-call design deliberately avoided. An mtime/newest-
  file check is the obvious guard; the kill-the-graph drill already proves the index is
  safely rebuildable, so a wrong staleness call degrades to "rebuild," never to corruption.
