# Ensō — Capture Policy

**Date:** 2026-07-09
**Status:** DRAFT — for Matt's ratification (closes design doc §10.5, the one
open decision never picked back up)
**Scope:** *behavioral* policy for what enters the substrate and how. The
*mechanics* of writing (grammar, append path) are WP-2; this governs judgment.
**Why it exists:** Goal #2 (reliable capture of Matt's world) is the ideal with
zero governing artifact, and the project's founding failure mode is *silent
non-capture* — including its own origin lineage (registry line
`miss:2026-07-07-enso-lineage`). A perfect substrate nobody feeds is the
Granola-shaped hole reopening.

---

## 1. The prime directive

> **When in doubt, capture.** INV-2 makes over-capture cheap (a low-value entry
> decays to its floor and bothers no one) and non-capture is the named,
> invisible, trust-breaking failure. The asymmetry is the policy.

Corollary: **capture at the turn, not in batch.** The durable commit happens in
the moment the information appears (write-ahead discipline, unified spec §5).
"I'll write it up tonight" is how Granola-class losses happen.

## 2. Always capture (triggers)

| Trigger | Type | Notes |
| --- | --- | --- |
| Matt states or accepts a commitment, deadline, or to-do | `Task` | Include owner + due signal in `content`; `OWNS` edge to the person/project. |
| A decision is made, or explicitly deferred | `Decision` | Rationale in `content`; deferral IS a decision — capture it with what unblocks it. |
| A correction of something already held | supersession | The full ceremony (tech spec §3.3): new entry + `SUPERSEDES` edge + close old `valid_until`. Never edit the old entry. |
| A durable fact about a person, project, tool, or plan | `Fact` | Billing tiers, role changes, name quirks ("Ollie = Ali"), paths, policies. |
| A realization / transferable pattern | `Insight` | Including cross-domain transfers and post-mortem lessons. |
| A thread left dangling ("let's come back to…", an unanswered ask) | `Task` | Dropped-thread surfacing is Goal #1; a thread not captured cannot be surfaced. |
| Anything Matt flags with "remember this" (or equivalent) | as fitting | Non-negotiable, immediate. |

## 3. Never capture (hard exclusions)

- **Secrets:** credentials, API keys, tokens, financial account details — even
  when Matt states them. Capture *that a credential exists and where it lives*,
  never its value.
- **Group/shared-context content:** the substrate serves direct-DM contexts only
  (privacy hard rule, tech spec §7). Content from group chats does not enter,
  period.
- **Third parties' sensitive personal information** stated in passing (health,
  private conflicts) unless Matt explicitly asks for it to be held.
- **Verbatim long quotations** of others' documents — capture the fact + a
  pointer, not the text.

## 4. Judgment rules (the ambiguous middle)

- **New entry vs supersession:** same subject AND the new information
  contradicts, narrows, or expands what's held → **supersede** (an
  incomplete-made-obsolete description is wrong-by-omission; the LeanCTX
  precedent, dross 2026-07-02). Genuinely new subject → new entry, with
  `RELATES_TO`/`ABOUT` edges to what it touches. Can't tell → new entry +
  `RELATES_TO` edge to the candidate + a note; resolve on next touch. **Never
  guess a supersession target** — a wrong SUPERSEDES silently buries a true
  memory, the costliest capture error (the 72%-false-positive lesson, dross
  2026-07-01).
- **Confidence assignment:** `high` = Matt stated it directly, or a verified
  artifact shows it. `medium` = inferred from strong context, or single
  secondhand source. `low` = speculation, partially-heard, or transcript-quality
  uncertainty. When transcription is involved and names/numbers matter, default
  one notch down.
- **Node-type tiebreak:** does it *oblige future action*? → `Task`. Was a
  *choice* made? → `Decision`. Is it a *reusable pattern*? → `Insight`.
  Otherwise `Fact`. `Person`/`Project` nodes exist to be pointed at — create
  them sparingly, on first real reference.
- **`event_time` vs `encoded_time`:** always record both, even when equal
  (Phase-3 hinges on never collapsing them). If the world-time is unknown,
  `event_time: null` — never fake it.
- **Edges are part of capture, not decoration.** An entry with no `about`/edge
  is a future NEIGHBOR miss: traversal can't reach an island. Minimum bar: one
  `about` ref per entry.

## 5. Self-audit (keeping the policy honest)

- The existing daily skim adds one line to its checklist: *"list today's
  capture-worthy moments (per §2) — were all written?"* Any miss is appended to
  the miss-corpus registry as `NON_CAPTURE` the same day.
- `NON_CAPTURE` registry lines are this policy's error signal. A recurring
  pattern (e.g., commitments made mid-meeting-transcript keep getting missed)
  is real evidence (n ≥ 1, RH-2) that justifies a policy amendment or, later, a
  capture-side aid — which is exactly the evidence path by which the deleted
  detection layer may earn its return (ADR-001, corollary b′).

## 6. Ratification

- [ ] Matt ratifies §2 triggers, §3 exclusions, §4 judgment rules
- [ ] Daily-skim checklist amended per §5
- [ ] This doc added to the drift table (unified spec §10) and hash-pinned
