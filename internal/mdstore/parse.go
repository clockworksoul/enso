package mdstore

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// ParseError is a loud, located parse failure. Per the parser contract (tech
// spec §3.4) malformed blocks are surfaced, never silently skipped — silent
// skipping would reintroduce failure mode #2.
type ParseError struct {
	Line int    // 1-based line where the offending block started (0 if unknown)
	Msg  string // human-readable description
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("mdstore: parse error at line %d: %s", e.Line, e.Msg)
	}
	return "mdstore: parse error: " + e.Msg
}

// rawBlock is an accumulated `###` block plus the key/value lines under it,
// before typed interpretation.
type rawBlock struct {
	startLine int
	header    string            // text after "### "
	keys      []string          // preserves source order
	kv        map[string]string // last value wins on duplicate (loud-checked below)
	dupKey    string            // first duplicate key seen, if any
}

// Parse reads a structured-Markdown document and returns its entries and edges.
// It is the inverse of Marshal: parse(serialize(x)) == x.
//
// Prose and any line that isn't part of a `###` block is ignored (the inline
// requirement, §3.5a: structured blocks are interleaved with prose daily
// notes). Within a block, only `- key: value` lines are consumed; a malformed
// block is a loud error.
func Parse(doc string) (entries []core.Entry, edges []core.Edge, err error) {
	lines := strings.Split(doc, "\n")
	var cur *rawBlock

	flush := func() error {
		if cur == nil {
			return nil
		}
		e, ed, perr := cur.interpret()
		if perr != nil {
			return perr
		}
		if e != nil {
			entries = append(entries, *e)
		}
		if ed != nil {
			edges = append(edges, *ed)
		}
		cur = nil
		return nil
	}

	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimRight(line, "\r")

		if h, ok := blockHeader(trimmed); ok {
			if ferr := flush(); ferr != nil {
				return nil, nil, ferr
			}
			cur = &rawBlock{startLine: lineNo, header: h, kv: map[string]string{}}
			continue
		}

		if cur == nil {
			// Outside any block: prose. Ignore.
			continue
		}

		key, val, ok := kvLine(trimmed)
		if !ok {
			if strings.TrimSpace(trimmed) == "" {
				// Blank line ends the current block; subsequent prose is ignored
				// until the next header.
				if ferr := flush(); ferr != nil {
					return nil, nil, ferr
				}
				continue
			}
			// A non-blank, non-kv line inside a block is malformed: loud.
			return nil, nil, &ParseError{Line: lineNo, Msg: fmt.Sprintf("expected `- key: value` or blank line inside block, got %q", trimmed)}
		}
		if _, exists := cur.kv[key]; exists && cur.dupKey == "" {
			cur.dupKey = key
		}
		if _, exists := cur.kv[key]; !exists {
			cur.keys = append(cur.keys, key)
		}
		cur.kv[key] = val
	}
	if ferr := flush(); ferr != nil {
		return nil, nil, ferr
	}
	return entries, edges, nil
}

// blockHeader returns the header text if the line opens a `###` block.
func blockHeader(line string) (string, bool) {
	const p = "### "
	if strings.HasPrefix(line, p) {
		return strings.TrimSpace(line[len(p):]), true
	}
	if line == "###" {
		return "", true
	}
	return "", false
}

// kvLine parses a `- key: value` property line.
func kvLine(line string) (key, val string, ok bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "- ") {
		return "", "", false
	}
	rest := t[2:]
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(rest[:idx])
	val = strings.TrimSpace(rest[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

// interpret turns a rawBlock into a typed Entry or Edge.
func (b *rawBlock) interpret() (*core.Entry, *core.Edge, error) {
	if b.dupKey != "" {
		return nil, nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("duplicate key %q in block %q", b.dupKey, b.header)}
	}
	switch {
	case b.header == "edge":
		ed, err := b.toEdge()
		return nil, ed, err
	case strings.HasPrefix(b.header, "mem:"):
		e, err := b.toEntry()
		return e, nil, err
	default:
		return nil, nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("unknown block header %q (want `mem:<id>` or `edge`)", b.header)}
	}
}

func (b *rawBlock) toEntry() (*core.Entry, error) {
	// Required keys must be present (key-present contract, §3.2): absent is a
	// format error, distinct from an explicit `null`.
	required := []string{"type", "content", "encoded_time", "confidence", "tags"}
	for _, k := range required {
		if _, ok := b.kv[k]; !ok {
			return nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("entry %q missing required key %q", b.header, k)}
		}
	}

	e := core.Entry{
		ID:      core.ID(b.header),
		Type:    core.NodeType(b.kv["type"]),
		Content: b.kv["content"],
		Tags:    []string{},
		About:   []string{},
		Extra:   map[string]string{},
	}

	enc, err := parseTime(b.kv["encoded_time"])
	if err != nil || enc == nil {
		return nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("entry %q: encoded_time must be a valid timestamp, got %q", b.header, b.kv["encoded_time"])}
	}
	e.EncodedTime = *enc
	e.Confidence = core.Confidence(b.kv["confidence"])

	if e.EventTime, err = parseOptTime(b, "event_time"); err != nil {
		return nil, err
	}
	if e.ValidFrom, err = parseOptTime(b, "valid_from"); err != nil {
		return nil, err
	}
	if e.ValidUntil, err = parseOptTime(b, "valid_until"); err != nil {
		return nil, err
	}
	if v, ok := b.kv["tags"]; ok {
		e.Tags = parseList(v)
	}
	if v, ok := b.kv["about"]; ok {
		e.About = parseList(v)
	}

	// Reserved temporal fields. If present, parse them; if absent, leave zero
	// (only legacy/hand-written blocks would omit them — the serializer always
	// writes them).
	if t, err := parseReservedTime(b, "last_ref_time"); err != nil {
		return nil, err
	} else if t != nil {
		e.Temporal.LastRefTime = *t
	}
	if f, ok, err := parseReservedFloat(b, "S_last"); err != nil {
		return nil, err
	} else if ok {
		e.Temporal.SLast = f
	}
	if f, ok, err := parseReservedFloat(b, "S_floor"); err != nil {
		return nil, err
	} else if ok {
		e.Temporal.SFloor = f
	}
	if f, ok, err := parseReservedFloat(b, "lambda"); err != nil {
		return nil, err
	} else if ok {
		e.Temporal.Lambda = f
	}
	if f, ok, err := parseReservedFloat(b, "S_cap"); err != nil {
		return nil, err
	} else if ok {
		e.Temporal.SCap = f
	}

	// Unknown keys → Extra (forward-compat, §3.4).
	for _, k := range b.keys {
		if !knownEntryKeys[k] {
			e.Extra[k] = b.kv[k]
		}
	}

	if err := e.Validate(); err != nil {
		return nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("entry %q invalid: %v", b.header, err)}
	}
	return &e, nil
}

func (b *rawBlock) toEdge() (*core.Edge, error) {
	for _, k := range []string{"from", "type", "to"} {
		if _, ok := b.kv[k]; !ok {
			return nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("edge missing required key %q", k)}
		}
	}
	ed := core.Edge{
		From:  core.ID(b.kv["from"]),
		Type:  core.EdgeType(b.kv["type"]),
		To:    b.kv["to"],
		Extra: map[string]string{},
	}
	for _, k := range b.keys {
		if !knownEdgeKeys[k] {
			ed.Extra[k] = b.kv[k]
		}
	}
	if err := ed.Validate(); err != nil {
		return nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("edge invalid: %v", err)}
	}
	return &ed, nil
}

// parseTime parses a non-null ISO-8601 timestamp. Returns (nil, nil) for the
// literal "null".
func parseTime(s string) (*time.Time, error) {
	if s == "null" {
		return nil, nil
	}
	t, err := time.Parse(isoFormat, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// parseOptTime parses an optional timestamp field that must be PRESENT (key
// exists) but may be `null`. Absent is allowed here only because some fields
// were not required at §3.2; the serializer always writes them.
func parseOptTime(b *rawBlock, key string) (*time.Time, error) {
	v, ok := b.kv[key]
	if !ok {
		return nil, nil
	}
	t, err := parseTime(v)
	if err != nil {
		return nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("%s: invalid timestamp %q", key, v)}
	}
	return t, nil
}

func parseReservedTime(b *rawBlock, key string) (*time.Time, error) {
	v, ok := b.kv[key]
	if !ok {
		return nil, nil
	}
	t, err := parseTime(v)
	if err != nil {
		return nil, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("%s: invalid timestamp %q", key, v)}
	}
	return t, nil
}

func parseReservedFloat(b *rawBlock, key string) (float64, bool, error) {
	v, ok := b.kv[key]
	if !ok {
		return 0, false, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false, &ParseError{Line: b.startLine, Msg: fmt.Sprintf("%s: invalid float %q", key, v)}
	}
	return f, true, nil
}

// parseList parses "[a, b, c]" into []string{"a","b","c"}; "[]" → empty slice.
func parseList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		// Tolerate a bare value as a single-element list (defensive); the
		// serializer never emits this shape.
		if s == "" {
			return []string{}
		}
		return []string{s}
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return []string{}
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
