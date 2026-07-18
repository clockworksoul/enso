// Package graphstore is the KùzuDB-backed driven adapter implementing
// core.Store (WP-3 / Phase 2 part 1). It is the SECOND implementation of the
// port — the Markdown store remains canonical (INV-1); this graph is a derived,
// rebuildable index. Nothing may ever exist only here: kill the database file,
// rebuild from Markdown (OpenRebuilt), and no information is lost.
//
// # Schema (tech spec §6, as Cypher DDL)
//
// One node table per memory node type — Fact, Decision, Insight, Person,
// Project, Task — all with identical properties, plus an Entity table for
// entity-ref edge targets (e.g. "person:matt", "project:enso"), and one rel
// table per edge type — SUPERSEDES, RELATES_TO, OWNS, ABOUT — declared over
// every (node, node) table pair so any edge the grammar permits is
// representable. The DDL is generated mechanically from the core type enums so
// it cannot drift from them.
//
// # Append-only fidelity (INV-2)
//
// The Markdown corpus may legally contain MULTIPLE physical records for one
// mem: id — the supersession ceremony re-appends a closed copy of the old
// entry. The graph preserves that record-level history exactly: the primary
// key is a store-assigned global sequence number (seq), not the mem: id, so
// every appended record is a distinct node and Load returns all of them in
// append order, byte-equivalent in information to what mdstore returns.
// Append never deletes or rewrites a prior record.
//
// # Property encoding
//
// Scalar times are stored as UTC RFC3339Nano strings ("" = explicit null);
// tags/about/extra are stored as JSON. The graph's job is structure (typed
// nodes and edges for traversal); property payloads stay boring and lossless.
// Tokenizing, ranking, and staleness logic live in core and are NOT
// re-implemented in Cypher — the graph provides reach, core provides judgment.
package graphstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	kuzu "github.com/kuzudb/go-kuzu"

	"github.com/clockworksoul/enso/internal/core"
)

// memTables is the closed set of memory node tables, one per core.NodeType.
// Order is fixed (it feeds DDL generation and per-table Load queries).
var memTables = []core.NodeType{
	core.TypeFact, core.TypeDecision, core.TypeInsight,
	core.TypePerson, core.TypeProject, core.TypeTask,
}

// relTables is the closed set of edge tables, one per core.EdgeType.
var relTables = []core.EdgeType{
	core.EdgeSupersedes, core.EdgeRelatesTo, core.EdgeOwns, core.EdgeAbout,
}

// entityTable holds entity-ref edge targets (and placeholder endpoints for
// edges whose mem: endpoint has no node record yet — the corpus permits an
// edge to reference an entry that was never appended; see resolveEndpoint).
const entityTable = "Entity"

// timeLayout is the on-graph timestamp encoding: UTC RFC3339Nano, a strict
// superset of the Markdown store's RFC3339, so the derived index never loses
// precision the canonical layer could carry.
const timeLayout = time.RFC3339Nano

// GraphStore is an embedded-KùzuDB implementation of core.Store.
// Use Open / OpenInMemory / OpenRebuilt to construct; Close when done.
type GraphStore struct {
	mu      sync.Mutex
	db      *kuzu.Database
	conn    *kuzu.Connection
	nextSeq int64
	closed  bool

	// embedder, when set, embeds entry content at append time (WP-4 vector
	// doorfinder; see vectors.go). Nil = pure WP-3 lexical+traversal store.
	embedder Embedder
}

// errClosed guards every operation after Close: the cgo handles are destroyed,
// so a use-after-close must be a loud Go error, never a C-level crash.
var errClosed = fmt.Errorf("graphstore: store is closed")

// Open opens (creating if absent) a graph database at path and ensures the
// schema exists. The database is a derived artifact: deleting path and calling
// OpenRebuilt with the Markdown corpus reconstructs it completely (INV-1).
func Open(path string) (*GraphStore, error) {
	var (
		db  *kuzu.Database
		err error
	)
	if path == "" {
		db, err = kuzu.OpenInMemoryDatabase(kuzu.DefaultSystemConfig())
	} else {
		db, err = kuzu.OpenDatabase(path, kuzu.DefaultSystemConfig())
	}
	if err != nil {
		return nil, fmt.Errorf("graphstore: open database %q: %w", path, err)
	}
	conn, err := kuzu.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("graphstore: open connection: %w", err)
	}
	g := &GraphStore{db: db, conn: conn}
	if err := g.ensureSchema(); err != nil {
		g.Close()
		return nil, err
	}
	if err := g.loadNextSeq(); err != nil {
		g.Close()
		return nil, err
	}
	return g, nil
}

// OpenInMemory opens an ephemeral in-memory graph (tests, throwaway sessions).
func OpenInMemory() (*GraphStore, error) { return Open("") }

// Close releases the connection and database. Safe to call more than once;
// all subsequent operations fail loudly with errClosed.
func (g *GraphStore) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	if g.conn != nil {
		g.conn.Close()
	}
	if g.db != nil {
		g.db.Close()
	}
}

// ensureSchema creates the node and rel tables if absent. DDL is generated
// from the core enums so the schema cannot drift from the domain types.
func (g *GraphStore) ensureSchema() error {
	const nodeCols = `seq INT64 PRIMARY KEY, id STRING, content STRING,
		encoded_time STRING, event_time STRING, valid_from STRING, valid_until STRING,
		confidence STRING, tags STRING, about STRING,
		last_ref_time STRING, s_last DOUBLE, s_floor DOUBLE, lambda DOUBLE, s_cap DOUBLE,
		extra STRING, embedding STRING`
	for _, nt := range memTables {
		ddl := fmt.Sprintf("CREATE NODE TABLE IF NOT EXISTS %s(%s)", nt, nodeCols)
		if err := g.exec(ddl); err != nil {
			return fmt.Errorf("graphstore: create node table %s: %w", nt, err)
		}
	}
	if err := g.exec(fmt.Sprintf("CREATE NODE TABLE IF NOT EXISTS %s(ref STRING PRIMARY KEY)", entityTable)); err != nil {
		return fmt.Errorf("graphstore: create node table %s: %w", entityTable, err)
	}

	// Every rel table spans all (from, to) node-table pairs: any memory node
	// (or a placeholder Entity endpoint) may point at any memory node or
	// entity-ref, matching the grammar's permissiveness (Edge.To is a free
	// string). 49 pairs per rel type, generated, not hand-maintained.
	allTables := make([]string, 0, len(memTables)+1)
	for _, nt := range memTables {
		allTables = append(allTables, string(nt))
	}
	allTables = append(allTables, entityTable)
	var pairs []string
	for _, from := range allTables {
		for _, to := range allTables {
			pairs = append(pairs, fmt.Sprintf("FROM %s TO %s", from, to))
		}
	}
	relCols := "seq INT64, from_id STRING, to_raw STRING, extra STRING"
	for _, et := range relTables {
		ddl := fmt.Sprintf("CREATE REL TABLE IF NOT EXISTS %s(%s, %s)",
			et, strings.Join(pairs, ", "), relCols)
		if err := g.exec(ddl); err != nil {
			return fmt.Errorf("graphstore: create rel table %s: %w", et, err)
		}
	}
	return nil
}

// loadNextSeq initializes the global sequence counter from the stored maximum,
// so a reopened database continues numbering where it left off.
func (g *GraphStore) loadNextSeq() error {
	max := int64(0)
	for _, nt := range memTables {
		v, err := g.queryScalar(fmt.Sprintf("MATCH (n:%s) RETURN max(n.seq)", nt))
		if err != nil {
			return fmt.Errorf("graphstore: max seq for %s: %w", nt, err)
		}
		if s, ok := v.(int64); ok && s > max {
			max = s
		}
	}
	for _, et := range relTables {
		v, err := g.queryScalar(fmt.Sprintf("MATCH ()-[r:%s]->() RETURN max(r.seq)", et))
		if err != nil {
			return fmt.Errorf("graphstore: max seq for %s: %w", et, err)
		}
		if s, ok := v.(int64); ok && s > max {
			max = s
		}
	}
	g.nextSeq = max + 1
	return nil
}

// Append durably records entries and edges (core.Store). All inputs are
// validated before any write (loud, no partial batch on invalid input — the
// same discipline as mdstore/memstore). A mid-batch storage failure can leave
// a partial graph; that is acceptable ONLY because the graph is derived — the
// Markdown corpus stays authoritative and the next OpenRebuilt repairs the
// index (unified spec §5 write path).
func (g *GraphStore) Append(ctx context.Context, entries []core.Entry, edges []core.Edge) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("graphstore: refusing to append invalid entry %q: %w", e.ID, err)
		}
	}
	for _, ed := range edges {
		if err := ed.Validate(); err != nil {
			return fmt.Errorf("graphstore: refusing to append invalid edge from %q: %w", ed.From, err)
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return errClosed
	}
	for _, e := range entries {
		if err := g.insertEntry(e); err != nil {
			return err
		}
	}
	for _, ed := range edges {
		if err := g.insertEdge(ed); err != nil {
			return err
		}
	}
	return nil
}

// insertEntry creates one node record in the entry's type table. Caller holds mu.
func (g *GraphStore) insertEntry(e core.Entry) error {
	tags, err := json.Marshal(e.Tags)
	if err != nil {
		return fmt.Errorf("graphstore: entry %q: marshal tags: %w", e.ID, err)
	}
	about, err := json.Marshal(e.About)
	if err != nil {
		return fmt.Errorf("graphstore: entry %q: marshal about: %w", e.ID, err)
	}
	extra, err := json.Marshal(e.Extra)
	if err != nil {
		return fmt.Errorf("graphstore: entry %q: marshal extra: %w", e.ID, err)
	}
	// Derived vector (WP-4). An embed failure stores the record WITHOUT a
	// vector — durability outranks index quality; the entry is still fully
	// lexically recallable and a later rebuild re-embeds it. "" = no vector.
	embedding := ""
	if g.embedder != nil {
		if vec, embErr := g.embedder.Embed(context.Background(), e.Content); embErr == nil {
			b, jerr := json.Marshal(vec)
			if jerr != nil {
				return fmt.Errorf("graphstore: entry %q: marshal embedding: %w", e.ID, jerr)
			}
			embedding = string(b)
		}
	}
	q := fmt.Sprintf(`CREATE (:%s {seq: $seq, id: $id, content: $content,
		encoded_time: $encoded_time, event_time: $event_time,
		valid_from: $valid_from, valid_until: $valid_until,
		confidence: $confidence, tags: $tags, about: $about,
		last_ref_time: $last_ref_time, s_last: $s_last, s_floor: $s_floor,
		lambda: $lambda, s_cap: $s_cap, extra: $extra, embedding: $embedding})`, e.Type)
	err = g.execParams(q, map[string]any{
		"seq":           g.nextSeq,
		"id":            string(e.ID),
		"content":       e.Content,
		"encoded_time":  fmtTime(e.EncodedTime),
		"event_time":    fmtTimePtr(e.EventTime),
		"valid_from":    fmtTimePtr(e.ValidFrom),
		"valid_until":   fmtTimePtr(e.ValidUntil),
		"confidence":    string(e.Confidence),
		"tags":          string(tags),
		"about":         string(about),
		"last_ref_time": fmtTime(e.Temporal.LastRefTime),
		"s_last":        e.Temporal.SLast,
		"s_floor":       e.Temporal.SFloor,
		"lambda":        e.Temporal.Lambda,
		"s_cap":         e.Temporal.SCap,
		"extra":         string(extra),
		"embedding":     embedding,
	})
	if err != nil {
		return fmt.Errorf("graphstore: insert entry %q: %w", e.ID, err)
	}
	g.nextSeq++
	return nil
}

// endpoint describes one side of a rel: either a memory node record (matched
// by table + seq — KùzuDB requires rel endpoints bound to a single node
// label) or an Entity node (matched by ref, MERGE-created on demand).
type endpoint struct {
	isEntity bool
	table    string // node table holding the record, when !isEntity
	seq      int64  // memory node primary key, when !isEntity
	ref      string // entity ref, when isEntity
}

// resolveEndpoint maps an edge endpoint string to a graph node. A string that
// names an existing memory record binds to the EARLIEST record with that id
// (the record identity; later closed copies of the same id are the same
// logical node). Anything else — an entity-ref like "person:matt", or a mem:
// id with no record yet — binds to an Entity node created on demand, so the
// edge is never dropped (loud fidelity beats referential strictness; the
// corpus grammar does not force edge targets to exist). Caller holds mu.
func (g *GraphStore) resolveEndpoint(raw string) (endpoint, error) {
	if core.ID(raw).Validate() == nil {
		// Per-table lookup (a rel endpoint must bind one label); the earliest
		// record across all tables wins.
		best := endpoint{seq: -1}
		for _, nt := range memTables {
			q := fmt.Sprintf("MATCH (n:%s) WHERE n.id = $id RETURN min(n.seq)", nt)
			v, err := g.queryScalarParams(q, map[string]any{"id": raw})
			if err != nil {
				return endpoint{}, fmt.Errorf("graphstore: resolve endpoint %q in %s: %w", raw, nt, err)
			}
			if s, ok := v.(int64); ok && (best.seq < 0 || s < best.seq) {
				best = endpoint{table: string(nt), seq: s}
			}
		}
		if best.seq >= 0 {
			return best, nil
		}
	}
	if err := g.execParams(fmt.Sprintf("MERGE (:%s {ref: $ref})", entityTable),
		map[string]any{"ref": raw}); err != nil {
		return endpoint{}, fmt.Errorf("graphstore: merge entity %q: %w", raw, err)
	}
	return endpoint{isEntity: true, ref: raw}, nil
}

// insertEdge creates one rel record. Caller holds mu.
func (g *GraphStore) insertEdge(ed core.Edge) error {
	extra, err := json.Marshal(ed.Extra)
	if err != nil {
		return fmt.Errorf("graphstore: edge from %q: marshal extra: %w", ed.From, err)
	}
	from, err := g.resolveEndpoint(string(ed.From))
	if err != nil {
		return err
	}
	to, err := g.resolveEndpoint(ed.To)
	if err != nil {
		return err
	}

	match := func(alias string, ep endpoint, seqParam, refParam string) string {
		if ep.isEntity {
			return fmt.Sprintf("(%s:%s {ref: $%s})", alias, entityTable, refParam)
		}
		return fmt.Sprintf("(%s:%s {seq: $%s})", alias, ep.table, seqParam)
	}
	params := map[string]any{
		"seq":     g.nextSeq,
		"from_id": string(ed.From),
		"to_raw":  ed.To,
		"extra":   string(extra),
	}
	if from.isEntity {
		params["aref"] = from.ref
	} else {
		params["aseq"] = from.seq
	}
	if to.isEntity {
		params["bref"] = to.ref
	} else {
		params["bseq"] = to.seq
	}
	q := fmt.Sprintf(`MATCH %s, %s CREATE (a)-[:%s {seq: $seq, from_id: $from_id, to_raw: $to_raw, extra: $extra}]->(b)`,
		match("a", from, "aseq", "aref"), match("b", to, "bseq", "bref"), ed.Type)
	if err := g.execParams(q, params); err != nil {
		return fmt.Errorf("graphstore: insert %s edge from %q: %w", ed.Type, ed.From, err)
	}
	g.nextSeq++
	return nil
}

// seqRecord pairs a loaded record with its sequence number for global ordering.
type seqRecord[T any] struct {
	seq int64
	val T
}

// Load reads back the full corpus in append (seq) order — every record,
// including superseded copies; nothing is hidden at the storage layer
// (core.Store contract). Decode failures are loud with table + seq context.
func (g *GraphStore) Load(ctx context.Context) ([]core.Entry, []core.Edge, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, nil, errClosed
	}

	var ents []seqRecord[core.Entry]
	for _, nt := range memTables {
		q := fmt.Sprintf(`MATCH (n:%s) RETURN n.seq, n.id, n.content,
			n.encoded_time, n.event_time, n.valid_from, n.valid_until,
			n.confidence, n.tags, n.about,
			n.last_ref_time, n.s_last, n.s_floor, n.lambda, n.s_cap, n.extra`, nt)
		rows, err := g.queryRows(q)
		if err != nil {
			return nil, nil, fmt.Errorf("graphstore: load %s: %w", nt, err)
		}
		for _, row := range rows {
			rec, err := decodeEntry(nt, row)
			if err != nil {
				return nil, nil, err
			}
			ents = append(ents, rec)
		}
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].seq < ents[j].seq })

	var eds []seqRecord[core.Edge]
	for _, et := range relTables {
		q := fmt.Sprintf("MATCH ()-[r:%s]->() RETURN r.seq, r.from_id, r.to_raw, r.extra", et)
		rows, err := g.queryRows(q)
		if err != nil {
			return nil, nil, fmt.Errorf("graphstore: load %s edges: %w", et, err)
		}
		for _, row := range rows {
			rec, err := decodeEdge(et, row)
			if err != nil {
				return nil, nil, err
			}
			eds = append(eds, rec)
		}
	}
	sort.Slice(eds, func(i, j int) bool { return eds[i].seq < eds[j].seq })

	if len(ents) == 0 && len(eds) == 0 {
		return nil, nil, nil // empty corpus is valid (matches mdstore/memstore)
	}
	entries := make([]core.Entry, len(ents))
	for i, r := range ents {
		entries[i] = r.val
	}
	edges := make([]core.Edge, len(eds))
	for i, r := range eds {
		edges[i] = r.val
	}
	return entries, edges, nil
}

// decodeEntry converts one node row (column order fixed by Load's RETURN)
// back into a core.Entry.
func decodeEntry(nt core.NodeType, row []any) (seqRecord[core.Entry], error) {
	var zero seqRecord[core.Entry]
	fail := func(field string, err error) (seqRecord[core.Entry], error) {
		return zero, fmt.Errorf("graphstore: decode %s node (seq %v): %s: %w", nt, row[0], field, err)
	}
	seq, ok := row[0].(int64)
	if !ok {
		return fail("seq", fmt.Errorf("unexpected type %T", row[0]))
	}
	encoded, err := parseTime(str(row[3]))
	if err != nil {
		return fail("encoded_time", err)
	}
	eventT, err := parseTimePtr(str(row[4]))
	if err != nil {
		return fail("event_time", err)
	}
	validFrom, err := parseTimePtr(str(row[5]))
	if err != nil {
		return fail("valid_from", err)
	}
	validUntil, err := parseTimePtr(str(row[6]))
	if err != nil {
		return fail("valid_until", err)
	}
	lastRef, err := parseTime(str(row[10]))
	if err != nil {
		return fail("last_ref_time", err)
	}
	var tags, about []string
	if err := json.Unmarshal([]byte(str(row[8])), &tags); err != nil {
		return fail("tags", err)
	}
	if err := json.Unmarshal([]byte(str(row[9])), &about); err != nil {
		return fail("about", err)
	}
	var extra map[string]string
	if err := json.Unmarshal([]byte(str(row[15])), &extra); err != nil {
		return fail("extra", err)
	}
	e := core.Entry{
		ID:          core.ID(str(row[1])),
		Type:        nt,
		Content:     str(row[2]),
		EncodedTime: encoded,
		EventTime:   eventT,
		ValidFrom:   validFrom,
		ValidUntil:  validUntil,
		Confidence:  core.Confidence(str(row[7])),
		Tags:        tags,
		About:       about,
		Temporal: core.Temporal{
			LastRefTime: lastRef,
			SLast:       row[11].(float64),
			SFloor:      row[12].(float64),
			Lambda:      row[13].(float64),
			SCap:        row[14].(float64),
		},
		Extra: extra,
	}
	return seqRecord[core.Entry]{seq: seq, val: e}, nil
}

// decodeEdge converts one rel row (r.seq, r.from_id, r.to_raw, r.extra).
func decodeEdge(et core.EdgeType, row []any) (seqRecord[core.Edge], error) {
	var zero seqRecord[core.Edge]
	seq, ok := row[0].(int64)
	if !ok {
		return zero, fmt.Errorf("graphstore: decode %s edge: seq: unexpected type %T", et, row[0])
	}
	var extra map[string]string
	if err := json.Unmarshal([]byte(str(row[3])), &extra); err != nil {
		return zero, fmt.Errorf("graphstore: decode %s edge (seq %d): extra: %w", et, seq, err)
	}
	ed := core.Edge{
		From:  core.ID(str(row[1])),
		Type:  et,
		To:    str(row[2]),
		Extra: extra,
	}
	return seqRecord[core.Edge]{seq: seq, val: ed}, nil
}

// --- small query/encoding helpers ---

func (g *GraphStore) exec(q string) error {
	res, err := g.conn.Query(q)
	if err != nil {
		return err
	}
	res.Close()
	return nil
}

func (g *GraphStore) execParams(q string, params map[string]any) error {
	prep, err := g.conn.Prepare(q)
	if err != nil {
		return err
	}
	defer prep.Close()
	res, err := g.conn.Execute(prep, params)
	if err != nil {
		return err
	}
	res.Close()
	return nil
}

// queryRows runs q and returns all result tuples as value slices.
func (g *GraphStore) queryRows(q string) ([][]any, error) {
	res, err := g.conn.Query(q)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	var rows [][]any
	for res.HasNext() {
		t, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := t.GetAsSlice()
		t.Close()
		if err != nil {
			return nil, err
		}
		rows = append(rows, vals)
	}
	return rows, nil
}

// queryScalar runs q and returns the single value of the single row (nil when
// the aggregate is over an empty set).
func (g *GraphStore) queryScalar(q string) (any, error) {
	rows, err := g.queryRows(q)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return nil, nil
	}
	return rows[0][0], nil
}

func (g *GraphStore) queryScalarParams(q string, params map[string]any) (any, error) {
	prep, err := g.conn.Prepare(q)
	if err != nil {
		return nil, err
	}
	defer prep.Close()
	res, err := g.conn.Execute(prep, params)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	if !res.HasNext() {
		return nil, nil
	}
	t, err := res.Next()
	if err != nil {
		return nil, err
	}
	defer t.Close()
	vals, err := t.GetAsSlice()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, nil
	}
	return vals[0], nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func fmtTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func fmtTimePtr(t *time.Time) string {
	if t == nil {
		return "" // explicit null (known-unknown), distinct from a zero time
	}
	return fmtTime(*t)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(timeLayout, s)
}

func parseTimePtr(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := parseTime(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// compile-time assertion: GraphStore must satisfy the port.
var _ core.Store = (*GraphStore)(nil)
