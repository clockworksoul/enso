# Q-A Resolution Memo — type-enum tolerance (loud warning vs hard error)

*Prepared 2026-07-12 (Dross Hour). Purpose: turn the one genuinely-open question
in the WP-2 opening brief (§2, Q-A) from a cold debate into a ~2-minute confirm,
by grounding it in **exactly what the code does today**. Zero production code
touched (RH-1/RH-4). Read alongside `docs/2026-07-11-wp2-opening-brief.md` §2.*

---

## 0. TL;DR

The opening brief's Q-A lean — "treat an unrecognized `type` as a **loud parse
warning, not a hard error**, consistent with 'unknown keys preserved'" — is
**not** the current behavior, and the "consistent with unknown keys" reasoning
**conflates two different code paths**. Right now:

- **Unknown *keys*** are preserved to `Extra` (forward-compat). ✅ soft.
- **Unknown *type values*** are a **HARD parse error** today. ❌ not soft.

So Q-A is really: *do we keep the current hard-reject, or deliberately loosen it
to a warning?* This memo lays out both, with the exact one-change cost each way,
so Matt just picks a direction at the review.

**My revised recommendation: KEEP THE HARD ERROR (status quo). Do not loosen.**
Rationale in §4 — the forward-compat argument that motivated the lean doesn't
actually apply to a closed, self-authored, single-writer corpus, and the failure
mode a warning creates (a typo'd type silently becoming a second-class citizen in
ranking) is exactly the silent-degradation class this whole project exists to
kill.

---

## 1. What the code does today (verified, with locations)

Two *different* mechanisms handle "something the schema didn't name," and they
diverge:

### (a) Unknown KEY → preserved (soft, forward-compat)

`internal/mdstore/parse.go` routes any key not in the known set into
`Entry.Extra` (the `// Unknown keys → Extra (forward-compat, §3.4)` branch).
Pinned by `TestParse_UnknownKeyToExtra` (`parse_test.go:139`): an unknown key
does **not** fail the parse; it round-trips.

### (b) Unknown TYPE VALUE → hard parse error (loud, reject)

The chain is:

1. `parse.go` `toEntry()` constructs the entry then calls `e.Validate()`
   (`parse.go:236`).
2. `core.Entry.Validate()` checks `!e.Type.Valid()` and returns
   `type: invalid node type %q` (`internal/core/types.go:179-181`).
3. `NodeType.Valid()` is a closed-set lookup against `ValidNodeTypes`
   (`types.go:33-38`) — the six types only.
4. `toEntry()` wraps any `Validate()` failure into a located `ParseError`
   (`parse.go:236-237`), which aborts the whole parse.

Pinned (at the domain layer) by `TestEntry_Validate` "bad type"
(`types_test.go:70`, `e.Type = "Bogus"` must error) and `TestNodeType_Valid`
(`types_test.go:14`, `"Nonsense"` invalid).

**Coverage gap worth noting:** there is currently **no test driving an unknown
type through the *parser* path** (`parse_test.go` only exercises the six valid
types + the unknown-*key* case). So the parser-level hard-reject is real but
un-pinned end-to-end. Whichever direction Q-A goes, a parser-level test should be
added in WP-2 to lock it (see §5).

---

## 2. The conflation in the brief's reasoning

The brief §2 justifies the "warning" lean as "consistent with 'unknown keys
preserved.'" But:

| | Unknown **key** | Unknown **type value** |
|---|---|---|
| Meaning | a field the grammar didn't name yet | a *value* outside a closed enum in a **required** field |
| Risk if kept | ~none — it's ignored data carried along | the entry's `type` drives decay (`DefaultTemporal(nt)`), `S_floor`, `lambda`, and specificity ranking. A bogus type silently gets **default/fallback** temporal params. |
| Today | soft (→ Extra) | **hard (reject)** |

Preserving an unknown *key* costs nothing because nothing downstream depends on
it. Preserving an unknown *type* is different: `type` is load-bearing for
Phase-3 ranking. `defaultSFloor`/`defaultLambda` both have a `default:` arm
(`types.go:109+`), so an unrecognized type wouldn't crash — it would silently
fall into the Fact/Task decay bucket. That's precisely the *silent wrong-answer*
failure this project targets.

So "consistent with unknown keys" is a false analogy. The honest framing is a
real tradeoff, below.

---

## 3. The two directions, with exact cost

### Direction A — KEEP hard error (status quo, zero code)

- **Change required:** none. The code already rejects. WP-2 adds one
  parser-level test (§5) to pin it end-to-end.
- **Behavior:** a typo'd or genuinely-new type fails the append *loudly, at the
  offending line*, and nothing is written until fixed.
- **Adding a 7th type later:** a 2-line change (add the const to the
  `TypeXxx` block + the `ValidNodeTypes` map). Not a format migration — no
  existing entry changes shape. The enum is extended *before* the entry using it
  is written. This is the normal path and it's cheap.

### Direction B — loosen to warning (accept + carry)

- **Change required (WP-2, not now):** `Validate()` can no longer hard-fail on
  type (or the parser must call a warn-not-fail variant); the parser emits a
  diagnostic on a side channel and still returns the entry with its raw/unknown
  type. Plus: a decision on what `DefaultTemporal` does for an unknown type
  (today it silently falls to the Fact/Task bucket — that behavior would now be
  *reachable in production*, so it needs to be made intentional and tested).
- **Behavior:** an entry with a typo'd type (`Decsion`) is **accepted**, ranked
  with fallback decay, and the only signal is a warning the single writer may not
  see (append is often non-interactive via `cmd/enso-append`).
- **New failure mode introduced:** silent second-class entries. This is the
  staleness/wrong-bucket class the corpus is supposed to *eliminate*, now
  reintroduced at the ingestion boundary.

---

## 4. Recommendation: KEEP THE HARD ERROR (Direction A)

The forward-compat instinct behind the "warning" lean is a good instinct **for
the wrong layer**. Forward-compat matters when *another system you don't control*
writes data you must tolerate. But the P1 corpus is:

- **closed-set by spec** (tech spec §6: exactly six node types),
- **self-authored** (single writer — Dross, via `cmd/enso-append`; RH-4),
- **INV-2 append-only** (a rejected append simply isn't written — no corruption,
  nothing half-done),
- and **load-bearing downstream** (`type` feeds decay + specificity ranking).

Under those four facts, the cost/benefit is lopsided:

- The thing a warning is supposed to buy — "don't lose data when you meet a type
  you didn't expect" — is nearly worthless here, because *I* author every entry
  and can extend the enum in the same edit (2 lines) before writing the entry.
- The thing a hard error buys — "a typo can never silently become a mis-ranked,
  wrong-decay-bucket memory" — is exactly the failure mode this project exists to
  kill (silent degradation, failure mode #2 in the design doc).

Loud-on-write is the *stronger* forward-compat story here: it forces the enum
extension to be a deliberate, reviewed 2-line change rather than an accident that
surfaces months later as a mysteriously-decaying memory.

**Keep unknown *keys* soft (they're inert). Keep unknown *type values* hard
(they're load-bearing). The asymmetry is correct, not an inconsistency.**

If Matt still prefers a warning, the fallback that preserves auditability is a
**third option**: accept-but-quarantine — carry the entry with a sentinel
`type: Unknown` *and* a persisted `_type_error` marker in `Extra`, so it's
findable and fixable but never silently ranked as a real type. That's more code
than Direction A and still needs a decision on decay for `Unknown`; only worth it
if a real "another tool writes my corpus" scenario ever appears (it doesn't
today — YAGNI).

---

## 5. Whichever way Q-A goes — one WP-2 test to add

There is currently **no parser-level test** for an unknown type (only the
domain-level `Validate` test). WP-2 should add, to `mdstore/parse_test.go`:

- **If Direction A:** `TestParse_LoudOnUnknownType` — a block with
  `type: Decsion` must return a `ParseError` naming the line (mirrors the
  existing `TestParse_LoudOnUnknownHeader` shape).
- **If Direction B:** `TestParse_WarnsButAcceptsUnknownType` — same input must
  return the entry + a surfaced diagnostic, and a companion test pinning what
  `DefaultTemporal` does for the unknown type (so the fallback bucket is
  *intentional*, not incidental).

This closes the end-to-end coverage gap from §1 either way.

---

## 6. What this memo changed

Nothing in `internal/`. This is analysis + a recommendation. It:

- corrects the brief's "consistent with unknown keys" reasoning (false analogy —
  two different code paths, §2),
- establishes that the **status quo is already hard-reject** (so "warning" is a
  *loosening*, not a codification, §1),
- recommends keeping the hard error with a concrete rationale grounded in the
  four corpus facts (§4),
- and names the single missing test to add in WP-2 regardless of the call (§5).

Q-A at the review is now: **"confirm keep-hard-error (recommended), or
deliberately loosen to warning."** ~2 minutes.

---

*Discipline note (RH-1): zero production code touched. This is a decision memo,
not a build. The parser test in §5 is authored during WP-2, after ratification.*
