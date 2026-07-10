# ADR-NNN — <short, declarative decision title>

**Date drafted:** YYYY-MM-DD
**Status:** PROPOSED | RATIFIED (YYYY-MM-DD) | SUPERSEDED by ADR-MMM
**Deciders:** Matt Titmus (ratifier), <author>
**Supersedes:** <ADR or implicit prior state, or "—">
**Related:** <docs, commits, registry lines — cite `miss:` ids where evidence
is a real case; RH-2 applies to decisions too>

---

## Context

<What question forced a decision, and why now. Two or three paragraphs max.
Name the alternatives that were live — a decision with no rejected alternative
is a description, not a decision. If a prior decision is being reversed, say so
plainly and link it; drift decided by silence is the failure this file format
exists to prevent (see the Jul-7 lineage miss, `miss:2026-07-07-enso-lineage`).>

## Decision

<One bold sentence stating the decision, then the minimum detail needed to act
on it. Present tense: "Ensō does X." If the decision has a corollary or a
deliberate exception, name it here with its own label so it can be cited.>

## Rationale (evidence)

<Numbered. Each item points at something checkable: a registry line, a
benchmark number, a doc section, an outage, a test. "It felt cleaner" is not
evidence. If the honest rationale includes a judgment call, say "judgment call"
explicitly rather than dressing it as data.>

## Consequences

<What becomes true, required, or forbidden as a result — including the costs.
Which open decisions (design doc §10 / tech spec §8) this resolves or
re-scopes. Which invariants or dev-spec guards it touches. An ADR that lists
only upsides hasn't been thought through.>

## Action items (ratified alongside the decision)

| # | Item | Owner |
| --- | --- | --- |
| 1 | <concrete, checkable> | |

## Ratification

- [ ] Matt ratifies the decision (and corollaries, if any)
- [ ] Action items accepted into the work queue (`ENSO-STATUS.md`)
- [ ] This ADR added to the unified spec §10 provenance table and hash-pinned
      (`bash scripts/enso-spec-drift.sh`) in the same commit

---

*Conventions: ADRs live in `docs/adr/`, numbered sequentially, never renumbered,
never deleted (INV-2 applies to decisions). A reversed decision gets a new ADR
whose `Supersedes` field points back; the old one's Status is updated to
SUPERSEDED — the only in-place edit permitted. Keep the whole thing under two
pages; if it needs more, the depth belongs in a design doc the ADR cites.*
