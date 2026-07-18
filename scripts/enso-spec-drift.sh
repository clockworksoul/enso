#!/usr/bin/env bash
# enso-spec-drift.sh — detect when the Ensō unified-spec snapshot has gone stale
# relative to its source docs.
#
# The unified spec (research/2026-06-20-enso-unified-spec.md) is a SNAPSHOT that
# points back to four living source docs. This script recomputes each source's
# sha256 and compares it to the hash pinned in §10 of the snapshot. A mismatch
# means a source changed and the snapshot must be reconciled.
#
# NOTE: the Phase 0 benchmark doc has a running log that drifts BY DESIGN. Its
# mismatches are reported as INFO (benign), not STALE. The other three sources
# are contract; their mismatches are STALE.
#
# Usage:   bash scripts/enso-spec-drift.sh
# Exit:    0 = in sync (or only benign benchmark drift); 1 = STALE; 2 = setup error

set -euo pipefail

RESEARCH="$(cd "$(dirname "${BASH_SOURCE[0]}")/../docs" && pwd)"
SNAPSHOT="$RESEARCH/2026-06-20-enso-unified-spec.md"

# source doc -> "benign" flag (1 = log drift expected/benign)
declare -a SOURCES=(
  "2026-06-16-memory-improvement-design.md|0"
  "2026-06-17-memory-system-technical-spec.md|0"
  "2026-06-17-phase0-benchmark.md|1"
  "2026-06-20-enso-hexagonal-portability-architecture.md|0"
  "2026-07-07-mnemosyne-prior-art-comparison.md|0"
  "adr/ADR-001-scope-ratification.md|0"
  "adr/ADR-002-vector-engine.md|0"
)

[ -f "$SNAPSHOT" ] || { echo "ERROR: snapshot not found: $SNAPSHOT" >&2; exit 2; }

stale=0
benign=0

echo "Ensō unified-spec drift check"
echo "Snapshot: $SNAPSHOT"
echo "----------------------------------------"

for entry in "${SOURCES[@]}"; do
  file="${entry%%|*}"
  is_benign="${entry##*|}"
  path="$RESEARCH/$file"

  if [ ! -f "$path" ]; then
    echo "MISSING  $file  (source doc not found)"
    stale=1
    continue
  fi

  actual="$(shasum -a 256 "$path" | awk '{print $1}')"
  # pinned hash = the sha256-looking token on the snapshot table row for this file
  pinned="$(grep -F "$file" "$SNAPSHOT" | grep -oE '[0-9a-f]{64}' | head -1 || true)"

  if [ -z "$pinned" ]; then
    echo "NO-PIN   $file  (no pinned hash found in snapshot §10)"
    stale=1
    continue
  fi

  if [ "$actual" = "$pinned" ]; then
    echo "OK       $file"
  else
    if [ "$is_benign" = "1" ]; then
      echo "INFO     $file  (changed — benign running-log drift; re-pin on next reconcile)"
      echo "           pinned=$pinned"
      echo "           actual=$actual"
      benign=1
    else
      echo "STALE    $file  (CONTRACT changed — snapshot must be reconciled)"
      echo "           pinned=$pinned"
      echo "           actual=$actual"
      stale=1
    fi
  fi
done

echo "----------------------------------------"
if [ "$stale" -eq 1 ]; then
  echo "RESULT: STALE — reconcile the unified spec against changed source(s), then update §10 hashes."
  exit 1
elif [ "$benign" -eq 1 ]; then
  echo "RESULT: IN SYNC (only benign benchmark-log drift). Re-pin benchmark hash at next reconcile."
  exit 0
else
  echo "RESULT: IN SYNC."
  exit 0
fi
