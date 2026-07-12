# WP-2 Opening Brief — ratification packet for the Jul-13 go-live review

*Prepared 2026-07-11 (Dross Hour). Purpose: make the WP-2 open a ~15-minute
ratification, not an improvisation. This brief touches **zero production code**
(RH-1/RH-4). It consolidates exactly what Matt must decide at the review so the
build can start the same session.*

*Normative sources this brief quotes (does not supersede): dev spec §6 (WP-2),
tech spec §3 (grammar), ENSO-STATUS.md (P1 checklist), ADR-001 (scope).*

---

## 0. TL;DR — what this session needs from Matt

Three sign-offs, all already leaning a documented direction. If Matt agrees with
the leans, this is three "yes"es and WP-2 building starts immediately:

1. **S-schema** — freeze the entry grammar exactly as in tech spec §3.1 (and as
   already implemented in the `mdstore` golden file). → **§2 below.**
2. **S-1** — entries live **inline** in `memory/YYYY-MM-DD.md`, interleaved with
   prose. → **§3 below.**
3. **S-reserved** — the current `DefaultTemporal`/`DefaultRecallParams` per-type
   placeholder values stand as `// TUNABLE`, uncalibrated (RH-8). → **§4 below.**

Everything after that is mechanical build against a frozen contract.

---

## 1. Where we are (one paragraph)

WP-0 (repaired build) and WP-1 (format reconciliation + doc hygiene) are both
**closed** (commits `2192328`, `ec2c3f1`, `95f851f`). The module compiles,
`make check` + `make test-race` are green, the `provenance` field is gone
(AMEND-1 restored), the README is off the deleted-detection-layer narrative, and
`make drift` is IN SYNC across all 6 contract sources. The substrate
(`entry` model + `Store` port + `mdstore` adapter + decay math) is proven in
tests. **WP-2 is the step that makes it the actual place memories land** — the
corpus goes live. WP-2's DoD is explicitly gated on the three ratifications
above (dev spec §6 "Pre-resolved decisions … Matt ratifies at WP open").

---

## 2. S-schema — the grammar to freeze (verbatim, as shipped)

This is the format we will be appending to for months; freezing it is the whole
point of the millimetre-resolution spec (tech spec §0 cone-of-uncertainty). The
grammar below is **already implemented** — the `mdstore` golden file round-trips
it losslessly today. Ratifying = "this exact shape is permanent; changes now
require an RH-7 migration ceremony."

### Entry block

```markdown
### mem:<YYYY-MM-DD>-<slug>
- type: <Fact|Decision|Insight|Person|Project|Task>
- content: <one-line human-readable payload>
- encoded_time: <ISO-8601>        # REQUIRED — when I recorded it
- event_time: <ISO-8601 | null>   # when it became true in the world
- valid_from: <ISO-8601 | null>
- valid_until: <ISO-8601 | null>  # null = still true
- confidence: <high|medium|low>
- tags: [<tag>, ...]
- about: [<entity-ref>, ...]      # e.g. project:omega, person:matt
# --- reserved, written now, inert until Phase 3 ---
- last_ref_time: <ISO-8601>       # init = encoded_time
- S_last: <float>
- S_floor: <float>
- lambda: <float>
- S_cap: <float>
```

### Edge block

```markdown
### edge
- from: mem:<id>
- type: <SUPERSEDES|RELATES_TO|OWNS|ABOUT>
- to: mem:<id> | <entity-ref>
```

### The rules that ride with the freeze (tech spec §3.2)

- **`id`** = `mem:` + ISO date + kebab slug. Stable forever, never reused
  (it's the graph join key; collisions are bugs).
- **Required keys:** `id`, `type`, `content`, `encoded_time`, `confidence`,
  `tags`. Everything else may be `null` **but the key must be present** —
  *absent is a format error, `null` is a known-unknown.*
- **Reserved fields written from day one** with defensible inits (no backfill
  migration later).
- **`event_time` ≠ `encoded_time`** even when equal — the Phase-3 temporal model
  hinges on never collapsing them.

### The one open question inside S-schema (needs a yes/no)

**Q-A: Is the 6-type enum (`Fact|Decision|Insight|Person|Project|Task`) final?**
The golden file and `core/types.go` use exactly these six. Adding a 7th type
later is *not* a format-breaking migration (it's an enum extension, parser
tolerates unknown values as long as we decide the tolerance rule). **Lean:**
freeze the six; treat an unrecognized `type` as a **loud parse warning, not a
hard error** (forward-compat, consistent with "unknown keys preserved"). Matt
confirms the tolerance rule.

---

## 3. S-1 — entry location: **inline** (recommended)

| | (a) Inline in `memory/YYYY-MM-DD.md` | (b) Dedicated `memory/structured/` |
|---|---|---|
| Capture fidelity | **High** — one place I already write | Lower — a second place to remember |
| Parse cost | Parser must skip prose | Clean parse |
| Failure mode risk | Low (the Granola risk: worthless if I don't write) | Higher (split-brain capture) |

**Lean (tech spec §3.5, dev spec §6): (a) inline.** The dominant risk for this
whole system is *not writing things down at all* (design doc §9, the
Granola-shaped risk). Fewer places to write = more likely to actually capture.
The parser cost (skip non-entry prose) is a solved problem — WP-2 item 1 hardens
`mdstore.FSStore` for exactly this. **Decision needed: confirm (a).**

---

## 4. S-reserved — placeholder inits stand (RH-8)

The reserved P3 fields need *keys present with defensible defaults*, not
calibrated values (calibration is Phase-3 work against a labeled corpus, RH-2).
Current per-type placeholders (from `core` `DefaultTemporal`/`DefaultRecallParams`,
as seen in the golden file for a `Project`):

```
last_ref_time = encoded_time
S_last  = 1.0     // TUNABLE
S_floor = 0.2     // TUNABLE
lambda  = 0.02    // TUNABLE (per-type)
S_cap   = 1.0     // TUNABLE
```

**Lean:** these stand as-is, every one marked `// TUNABLE`, uncalibrated.
**Decision needed:** confirm "placeholders OK, do not tune in WP-2."

---

## 5. What building WP-2 looks like once ratified (so Matt sees the blast radius)

From dev spec §6, in order. LoC budget **+600**; the only `cmd/` allowed is
`enso-append` (RH-4). No graph, no vectors, no auto-capture-decision logic.

1. **Harden `mdstore.FSStore`** to parse entries embedded in prose; **loud**
   errors on malformed blocks (name file + line; never silent-skip — that's
   failure mode #2); unknown keys preserved on rewrite.
2. **Single-writer discipline** — advisory file lock / lock-file convention
   around append (stdlib + boring). Concurrent-append test: two writers, no
   interleaved/corrupt entries.
3. **Supersession-append ceremony** exactly per tech spec §3.3: new entry →
   `SUPERSEDES` edge → re-append old entry with `valid_until` set. Never
   in-place edit history (INV-2).
4. **`cmd/enso-append`** — one-shot ingestion: accepts one entry's fields,
   appends via `FSStore`. Smallest runnable surface that lets real capture start.
5. **Format README** (AMEND-1) — grammar + field semantics + supersession
   convention, readable standalone with no code (a reader can hand-write a valid
   entry from it; Matt spot-checks).
6. **Begin real capture** — first batch of genuine structured entries, starting
   with the ADR-001 decisions themselves. ≥10 real entries + ≥1 real
   supersession triple.

**Then the P1 exit measurement** (the hard gate): does structured corpus +
active-memory *already* beat the P0 flat-file baseline on the real-miss set? If
**yes → STOP**; WP-3 (the graph) requires Matt's explicit "proceed anyway" so
the graph earns its keep rather than getting built reflexively (tech spec §3.8).

---

## 6. First real supersession triple — a concrete candidate (for §5 step 6)

WP-2 needs ≥1 *real* supersession triple. A clean, already-documented candidate
from Dross's own recent history (not synthetic — RH-2 satisfied):

- **Superseded:** the Jun-25 note that Granola should keep being used
  ("keep using it" override).
- **Current:** the Jul-6 fact that Granola is being **uninstalled from all Yext
  devices** (IT enforcement), *but the REST API still works without the app*.
- **Edge:** `SUPERSEDES` new → old; old entry gets `valid_until: 2026-07-06`.

This is exactly the STALE class the whole system exists to fix, and it's a real
belief change with real timestamps. It doubles as the seed for the "does
supersession-aware corpus beat flat-file" exit measurement. (Candidate only —
final entries authored during the build, not pre-committed here.)

---

## 7. Pre-flight checklist for the review (Matt runs / confirms)

- [ ] `cd ~/workspace/clockworksoul/enso && make check` green (fmt/vet/build/test/drift)
- [ ] `make test-race` green
- [ ] `git log --oneline -3` shows WP-1 closed at `95f851f`
- [ ] Q-A answered (type-enum tolerance: loud warning vs hard error)
- [ ] S-schema ratified (grammar frozen)
- [ ] S-1 ratified (inline)
- [ ] S-reserved ratified (placeholders stand, `// TUNABLE`)
- [ ] Green light to open WP-2 build

---

## 8. Explicit non-goals of THIS brief (discipline check)

This brief did **not**: change any `internal/` code, add fields, redesign the
grammar, build any `cmd/`, or start WP-2. It is the ratification packet only.
Per RH-1, WP-2 building begins only after the sign-offs in §0 — at the Jul-13
open, with Matt present. If any answer in §2–§4 diverges from the leans, the
divergence is recorded in `ENSO-STATUS.md` and the affected build step waits.

---

*Build the house before the roof. This brief is just laying out the blueprints
so the pour goes fast.*
