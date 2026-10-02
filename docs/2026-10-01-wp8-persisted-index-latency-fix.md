# WP-8 blocker (b) — persisted index, latency fix SHIPPED (2026-10-01, Dross Hour)

*Status going in: the 2026-09-30 pass diagnosed blocker (b) precisely — the 16–20s
vector-mode wall is `enso-recall` re-embedding the whole corpus on every call, not the
query embed — and measured a persisted-index fix at ~461ms warm (~40x). It explicitly
stopped at the seam: "No fix committed … should be its own reviewed change." This pass is
that change: implemented, tested, and proven end-to-end against the live corpus.*

## What shipped

`cmd/enso-recall` now keeps an on-disk KùzuDB index at `<root>/.enso/index.kuzu`
(override with `-index`, opt out with `-index -`). Per call:

- **WARM** (index present, no `memory/*.md` file newer than it): `graphstore.Open` the
  persisted graph and attach the embedder **only for the single query embed** — no
  `Append` runs, so the corpus is never re-embedded. Stored embeddings (node properties
  per WP-4/ADR-002) are read back by the existing `loadEmbeddings` path.
- **COLD** (index absent, stale, `-rebuild`, or in-memory `-index -`): load the Markdown
  corpus and rebuild the on-disk index from it, embedding at append time, then recall.

The key insight that keeps this clean: on the warm path the embedder is attached but
`Append` is never called, so `g.embedder.Embed` fires exactly once (the query) inside
`vectorSeeds`. Vector mode is preserved; the corpus re-embed is eliminated.

## Live proof (2026-10-01, live corpus `~/.openclaw/workspace`, 68 entries, throwaway index)

Query: `"omega livetopology aggregate"`, `-k 3`, `GEMINI_API_KEY` set (vector mode):

| Call | Wall | `elapsed_ms` | `index_built` | mode | results |
|---|---|---|---|---|---|
| COLD (builds+embeds index) | 19.43s | 19108 | true | vector | 3 |
| WARM (reuses index) | **0.79s** | **764** | false | vector | 3 (identical) |

**~25x faster warm, ~5x under the 4s shadow deadline, still full `vector` mode.** This is
on a single warm-network run; treat it as "clearly sub-second and clearly sub-deadline,"
not a calibrated SLA — the order of magnitude (764ms vs 19,108ms) is the point and is not
in doubt. It directly reverses the 2026-09-24 conclusion that a fresh vector-mode window
"will be all timeouts": once the index is reused, it won't.

## Why no schema bump / why the deployed bridge gets it for free

Two additive JSON fields were added — `index_path` and `index_built` — and the schema
version stays **1**. The host bridge (`enso-bridge.ts`) version-gates strictly
(`out.version !== SUPPORTED_SCHEMA_VERSION` → fail) but reads only known fields and ignores
extras, so a superset output is safe; a version bump would have forced a lockstep two-file
contract change. The bridge invokes `enso-recall -root … -query … -k …` with **no**
`-index` flag, so the default `<root>/.enso/index.kuzu` activates automatically — the
existing deployment picks up the speedup with zero bridge change.

## Staleness guard (the one new soft-failure mode)

Persisting the index reintroduces a cache-staleness question the pure-rebuild-every-call
design avoided. The guard: the index is fresh iff its mtime is `>=` the newest mtime of any
file under `<root>/memory`. Any corpus file newer than the index → cold rebuild. The guard
fails **safe toward rebuilding**: a staleness-check *error* is loud (returned, never
silently treated as fresh), and a wrong "fresh" verdict degrades to "slightly out-of-date
recall," never corruption — the corpus stays canonical and the next rebuild heals the index
(INV-1, the kill-the-graph drill made routine). A corpus with no `memory/` dir leaves the
newest time at zero, so any present index counts as fresh (nothing to be stale against).

## Scope & invariants

- **Corpus is never written.** The index lives under `<root>/.enso/` (the dir the shadow
  logs already use), which is derived cache, not canonical memory. `TestRecallIsReadOnly`
  now scopes its byte-identical check to `memory/` and excludes `.enso/`;
  `TestPersistedIndexLeavesCorpusUntouched` separately pins that building/reusing the index
  modifies nothing under `memory/`.
- **No `internal/core`, `internal/mdstore`, or `internal/graphstore` change.** The fix is
  entirely in the WP-7 bridge binary (`cmd/enso-recall/main.go`). `OpenRebuiltWith` already
  accepted a real `dbPath` and already removed a stale index before rebuild — this change
  just stops passing `""` and adds the warm-open + staleness paths around it.
- **No corpus-format or ADR change.** Embeddings were always derived data meant to live in
  the index (ADR-002); this uses the persisted path that already existed.

## Tests (all green: `make check` + `-race` on cmd/enso-recall + graphstore)

- `TestPersistedIndexColdThenWarm` — first call `index_built=true` + file persisted; second
  call `index_built=false` with identical top result and corpus count.
- `TestPersistedIndexStaleTriggersRebuild` — `os.Chtimes` a memory file into the future →
  next call rebuilds.
- `TestPersistedIndexLeavesCorpusUntouched` — `memory/` byte-identical across cold+warm.
- `TestForceRebuildFlag` — `-rebuild` rebuilds a fresh index.
- `TestInMemoryIndexOptOut` — `-index -` reports empty `index_path`, always rebuilds, writes
  no default index file.
- Existing `TestRecallJSONShape` / `TestRecallEmptyQueryRecentMode` /
  `TestRecallMissingRootIsLoud` updated to the `runConfig` signature, still pinning the v1
  contract and the loud-on-missing-root path.

## What this changes for WP-8

- Blocker (b) is now **shipped and proven**, not just measured: warm vector recall is
  sub-second and under the deadline, with the deployed bridge picking it up automatically.
- Blocker (a) — co-key (done Sep 29) + fill `used` from a host material-use (RECALL-DEF)
  signal — remains the **true long pole** and is unchanged by this pass. It is blocked on a
  host event Ensō does not emit yet; it is not a latency problem.
- Option 2 from the Sep-30 doc (batch the cold-path append embeds via a `BatchEmbed` method
  on the `Embedder` interface) is **still worth doing** to shrink the one-time ~19s cold
  build toward ~1–2s, and is **deliberately NOT in this change** — it touches the `Embedder`
  interface and deserves its own review. The warm path (the per-call RH-2 case) is fully
  answered without it; the cold build is paid once (or whenever the corpus changes), so it
  is not on the hot path.

## Honest caveats

- The ~19s cold build scales linearly with corpus size (~275ms/entry), so it gets worse as
  the corpus grows — which is the argument for option 2's append batching eventually, and
  the argument for persisting the index *now* (the warm path makes the growing cold cost a
  once-per-change event instead of a per-call event).
- The staleness guard is mtime-based, which is coarse (a `touch` with no content change
  forces a rebuild). That is the conservative direction and acceptable; a content-hash guard
  is a possible future refinement with no current need (YAGNI).
- 68 entries / one machine / one run. The speedup is an order-of-magnitude result, not a
  benchmark; the mechanism (skip the 68 sequential embeds) is what guarantees it, not the
  specific millisecond count.

<!-- project: github.com/clockworksoul/enso -->
