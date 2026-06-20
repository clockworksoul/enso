// Package mdstore is the Markdown driven-adapter: it implements core.Store by
// serializing memory entries/edges to, and parsing them from, the canonical
// structured-Markdown format (tech spec §3.1).
//
// This package is the concrete expression of AMEND-1: the on-disk format is a
// public, documented contract, not an opaque detail. The serializer emits a
// fixed, deterministic grammar (pinned by golden-file tests) and the parser is
// mechanical and lossless — parse(serialize(x)) == x (the INV-1 round-trip
// law, tech spec §3.4).
package mdstore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// isoFormat is the ISO-8601 timestamp format used on disk. RFC3339 is the
// ISO-8601 profile Go provides; we use UTC-normalized RFC3339 for stable,
// round-trippable timestamps.
const isoFormat = time.RFC3339

// canonicalEntryKeys is the fixed field order the serializer emits (tech spec
// §3.1). Order is part of the public contract so golden files are stable.
// Unknown (Extra) keys are emitted after these, sorted, for determinism.
var canonicalEntryKeys = []string{
	"type", "content", "encoded_time", "event_time",
	"valid_from", "valid_until", "confidence", "tags", "about",
	// reserved, inactive until Phase 3:
	"last_ref_time", "S_last", "S_floor", "lambda", "S_cap",
}

// knownEntryKeys is the set of keys the parser maps to typed fields. Anything
// else goes into Entry.Extra (forward-compat, §3.4).
var knownEntryKeys = func() map[string]bool {
	m := make(map[string]bool, len(canonicalEntryKeys))
	for _, k := range canonicalEntryKeys {
		m[k] = true
	}
	return m
}()

// knownEdgeKeys are the keys the parser maps to typed Edge fields.
var knownEdgeKeys = map[string]bool{"from": true, "type": true, "to": true}

// MarshalEntry serializes a single Entry to its canonical Markdown block. The
// output ends with a trailing newline so blocks concatenate cleanly.
func MarshalEntry(e core.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n", e.ID)
	writeKV(&b, "type", string(e.Type))
	writeKV(&b, "content", e.Content)
	writeKV(&b, "encoded_time", formatTime(&e.EncodedTime))
	writeKV(&b, "event_time", formatTime(e.EventTime))
	writeKV(&b, "valid_from", formatTime(e.ValidFrom))
	writeKV(&b, "valid_until", formatTime(e.ValidUntil))
	writeKV(&b, "confidence", string(e.Confidence))
	writeKV(&b, "tags", formatList(e.Tags))
	writeKV(&b, "about", formatList(e.About))
	writeKV(&b, "last_ref_time", formatTime(&e.Temporal.LastRefTime))
	writeKV(&b, "S_last", formatFloat(e.Temporal.SLast))
	writeKV(&b, "S_floor", formatFloat(e.Temporal.SFloor))
	writeKV(&b, "lambda", formatFloat(e.Temporal.Lambda))
	writeKV(&b, "S_cap", formatFloat(e.Temporal.SCap))
	writeExtra(&b, e.Extra)
	return b.String()
}

// MarshalEdge serializes a single Edge to its canonical Markdown block.
func MarshalEdge(e core.Edge) string {
	var b strings.Builder
	b.WriteString("### edge\n")
	writeKV(&b, "from", string(e.From))
	writeKV(&b, "type", string(e.Type))
	writeKV(&b, "to", e.To)
	writeExtra(&b, e.Extra)
	return b.String()
}

// Marshal serializes a full corpus (entries then edges) into one document.
// Blocks are separated by a single blank line for readability.
func Marshal(entries []core.Entry, edges []core.Edge) string {
	blocks := make([]string, 0, len(entries)+len(edges))
	for _, e := range entries {
		blocks = append(blocks, strings.TrimRight(MarshalEntry(e), "\n"))
	}
	for _, e := range edges {
		blocks = append(blocks, strings.TrimRight(MarshalEdge(e), "\n"))
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

func writeKV(b *strings.Builder, key, val string) {
	fmt.Fprintf(b, "- %s: %s\n", key, val)
}

// writeExtra emits unknown keys after the known ones, sorted for determinism.
func writeExtra(b *strings.Builder, extra map[string]string) {
	if len(extra) == 0 {
		return
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		// Defensive: never let an Extra key shadow a known key on rewrite.
		if knownEntryKeys[k] || knownEdgeKeys[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeKV(b, k, extra[k])
	}
}

// formatTime renders a *time.Time as ISO-8601, or the literal "null" for nil.
// Times are normalized to UTC for stable round-tripping.
func formatTime(t *time.Time) string {
	if t == nil {
		return "null"
	}
	return t.UTC().Format(isoFormat)
}

// formatList renders a string slice as "[a, b, c]"; empty slice as "[]".
func formatList(items []string) string {
	return "[" + strings.Join(items, ", ") + "]"
}

// formatFloat renders a float in a compact, round-trippable form.
func formatFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}
