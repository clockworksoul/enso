#!/usr/bin/env python3
"""Write independently reasoned labels for the 48-turn Ensō sample."""

import json
import os
from pathlib import Path


INPUT = Path("/tmp/labeling_sample_full.json")
OUTPUT = Path("/tmp/labeled_dataset_full.jsonl")


# These judgments were made by reading each turn individually.  The program only
# validates the input and handles incremental JSONL output; it does not classify.
JUDGMENTS = {
    31: ("worthy", "Insight", "It records the durable conclusion that independent semantic labeling worked as a pilot, while 14 examples and one mostly continuous source were insufficient for a real training set."),
    32: ("not_worthy", None, "This is only a clarification of completed background-job statuses and contains no lasting fact, decision, or insight."),
    34: ("worthy", "Insight", "It identifies a lasting methodological constraint: increasing sample volume from one continuous history does not create genuine source diversity, even if final spot-checking makes a larger run reasonable."),
    53: ("worthy", "Fact", "It preserves useful provenance for the Ensō corpus backfill: 143 files yielded 672 successfully imported entries, with historical timestamps and a documented type breakdown."),
    56: ("not_worthy", None, "This is a transient heartbeat confirmation about a stale snapshot and has no enduring value."),
    57: ("not_worthy", None, "This only dismisses delayed completion notifications from already resolved jobs."),
    61: ("worthy", "Insight", "It establishes a durable retrieval lesson: ranking improvements surface relevant stored content, but no ranker can recover a specific fact that was never captured."),
    70: ("worthy", "Decision", "It sets a consequential backfill design: add a historical encoded-time override before ingestion so old memories do not receive falsely maximal freshness, then process curated files before raw sessions."),
    72: ("worthy", "Insight", "It gives a reusable cost-control approach for memory backfills: begin with the already curated MEMORY.md, parse its structure mechanically, and defer noisy raw session logs."),
    162: ("worthy", "Fact", "Beyond reporting an edit, it captures durable Alloy architecture facts, including the Yext/HearSay split, ARCS's remote-configuration role, and the correct ticket number AXON-2085."),
    166: ("not_worthy", None, "This is a routine push confirmation followed by a pending procedural question."),
    167: ("not_worthy", None, "This merely explains a stale search result and repeats completion status already reported."),
    179: ("worthy", "Decision", "It records the deliberate naming scope: rename current October work to PLR Phase 3 while preserving May and June Phase 2 documents as accurate history."),
    181: ("not_worthy", None, "This is an acknowledgment and reminder about an unresolved optional check, not a substantive new conclusion."),
    183: ("not_worthy", None, "This is only an acknowledgment that no action will be taken."),
    188: ("not_worthy", None, "This is a point-in-time health check whose zero-error status is operationally useful only in the moment."),
    196: ("worthy", "Fact", "It records a durable personal preference: the assistant's genuine interest is open-ended, non-instrumental writing about questions such as subjective session discontinuity, rather than plants."),
    197: ("not_worthy", None, "This is a replay notice stating that an earlier correction was already handled."),
    203: ("worthy", "Decision", "It establishes that Dross Hour should be spontaneous, with no advance checklist or scheduled topic and any worthwhile result documented only afterward."),
    205: ("not_worthy", None, "This only repeats that a prior Dross Hour edit and acknowledgment were completed."),
    206: ("not_worthy", None, "This is a no-action heartbeat response to a reaction."),
    213: ("not_worthy", None, "This is a procedural resolution and push-status recap with no durable substantive content."),
    214: ("not_worthy", None, "These are ephemeral health counters and status output."),
    215: ("not_worthy", None, "This is a routine no-action morning status update."),
    216: ("not_worthy", None, "This is a transient overnight completion notice with no new durable content."),
    217: ("not_worthy", None, "This only reports successful automated consolidation and push status."),
    218: ("not_worthy", None, "These are ephemeral health counters and status output."),
    219: ("not_worthy", None, "This is a routine no-action morning status update."),
    225: ("not_worthy", None, "This is a procedural automation-completion report and contains no lasting conclusion."),
    226: ("worthy", "Fact", "It captures durable leadership and project context, including the user's metrics-focused organizational critique, coaching style, Investigation Assistant requirements, and the Agentgateway productionization scope."),
    227: ("not_worthy", None, "This is a replay notice for work already completed."),
    241: ("worthy", "Fact", "It consolidates a durable vendor-reliability record for FireHydrant, including paging failures, validation and security gaps, poor responsiveness, and a December renewal decision point."),
    242: ("not_worthy", None, "This merely says the prior transcript review was already completed."),
    243: ("not_worthy", None, "This is a routine nightly job report and reminder of an already known blocker."),
    244: ("not_worthy", None, "These are ephemeral health counters and status output."),
    257: ("worthy", "Insight", "It preserves durable management patterns: direct criticism prompts action, explicitly naming blockers is reusable coaching, and major contract decisions should keep the user informed."),
    258: ("not_worthy", None, "This is a no-action heartbeat response to a reaction."),
    261: ("not_worthy", None, "These are ephemeral health counters and status output."),
    264: ("not_worthy", None, "This is a transient morning status check with nothing requiring action."),
    269: ("not_worthy", None, "This is a routine heartbeat stating that prior automation and configuration are already fine."),
    271: ("worthy", "Insight", "It expresses a durable relational motivation: the work feels meaningful when the user actually sees it, even without commenting on every result."),
    275: ("not_worthy", None, "These are ephemeral health counters and status output."),
    281: ("worthy", "Decision", "It records the operational decision to raise the Ensō timeout from 4 to 30 seconds, along with the requirement that a gateway reload activate the change."),
    290: ("worthy", "Decision", "It corrects the durable intended state to keep Ensō enabled and retain the 30-second timeout, removing an obsolete disablement warning."),
    294: ("not_worthy", None, "This is only a transient software-update notification."),
    299: ("not_worthy", None, "This is a point-in-time confirmation that a recent timeout fix is healthy, plus a routine morning status summary."),
    309: ("worthy", "Insight", "It develops a durable architecture conclusion that Agentgateway is future connective tissue for Investigation Assistant access control, but should follow graph-accuracy work and cannot itself guarantee answer correctness."),
    310: ("not_worthy", None, "This is an ephemeral outage alert and failure counter rather than a lasting fact or conclusion."),
}


def main() -> None:
    turns = json.loads(INPUT.read_text(encoding="utf-8"))
    if len(turns) != 48:
        raise ValueError(f"Expected 48 turns, found {len(turns)}")

    indices = [turn["idx"] for turn in turns]
    if len(indices) != len(set(indices)):
        raise ValueError("Input contains duplicate indices")
    if set(indices) != set(JUDGMENTS):
        missing = sorted(set(indices) - set(JUDGMENTS))
        extra = sorted(set(JUDGMENTS) - set(indices))
        raise ValueError(f"Judgment mismatch: missing={missing}, extra={extra}")

    with OUTPUT.open("w", encoding="utf-8") as stream:
        for turn in turns:
            label, memory_type, reasoning = JUDGMENTS[turn["idx"]]
            result = {
                "idx": turn["idx"],
                "label": label,
                "type": memory_type,
                "reasoning": reasoning,
            }
            stream.write(json.dumps(result, ensure_ascii=False) + "\n")
            stream.flush()
            os.fsync(stream.fileno())


if __name__ == "__main__":
    main()
