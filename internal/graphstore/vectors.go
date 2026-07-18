package graphstore

// vectors.go — WP-4: the internal vector supplement (ADR-002).
//
// The vector index is the DOORFINDER: it finds entry nodes the query's exact
// vocabulary misses; the graph then walks the house (traversal), and the
// existing filter/rank pipeline judges. Vectors live INSIDE Ensō — embedded
// alongside the graph — so an embedding-provider outage can never again sit in
// the critical path: recall degrades to WP-3 lexical+traversal, never to zero
// (fail-safe invariant, dev spec §8).
//
// Engine (ADR-002, ratified 2026-07-18): KùzuDB is the single storage engine —
// entry embeddings are stored as node properties in the graph database, no
// sqlite-vec sidecar. Similarity is exact cosine over the candidate set;
// KùzuDB's native ANN index (the statically-linked VECTOR extension, verified
// available offline in this binding) is DEFERRED until a real latency case is
// logged (RH-2) — at the current corpus scale exact scan is microseconds and
// has no index lifecycle to maintain.
//
// Embeddings are DERIVED data (INV-1): they exist only in the graph, never in
// the Markdown substrate, and are recomputed at append/rebuild. Killing the
// graph loses nothing.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// Embedder turns text into a vector. Implementations must be safe for
// concurrent use. An Embedder is OPTIONAL on a GraphStore; without one the
// store is exactly the WP-3 lexical+traversal adapter.
type Embedder interface {
	Name() string
	Embed(ctx context.Context, text string) ([]float64, error)
}

// Vector-seeding knobs. Phase-1 priors, untouched until a labeled corpus
// exists to calibrate against (RH-8).
const (
	// vectorSeedK is the maximum number of entries vector similarity may add
	// to the seed set per query.
	vectorSeedK = 5 // TUNABLE — Phase-3 calibration target

	// vectorMinSim is the cosine floor below which a match is noise, not a
	// door. Conservative prior: related prose under gemini-embedding-001
	// typically scores well above this; unrelated text below it.
	vectorMinSim = 0.60 // TUNABLE — Phase-3 calibration target
)

// SetEmbedder attaches an embedder. Entries appended AFTER this call have
// their content embedded and stored; call it before loading a corpus (the
// OpenRebuiltWith constructor does this in the right order).
//
// An append-time embed failure does NOT fail the append: the record is stored
// without a vector (it simply cannot be vector-found until re-embedded by a
// later rebuild). Memory durability always outranks index quality — the same
// principle as the log-first write path. The failure is not swallowed: it is
// returned to recall-time callers via RecallResult.Degraded whenever the
// vector path is unavailable.
func (g *GraphStore) SetEmbedder(e Embedder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.embedder = e
}

// Cosine returns the cosine similarity of two vectors, 0 when either is empty
// or lengths differ (defensive: mismatched embedding models must never rank).
func Cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// loadEmbeddings returns id → stored embedding for every record that has one
// (first non-empty record per id wins; copies of a re-appended id share
// content). Caller holds mu.
func (g *GraphStore) loadEmbeddings() (map[core.ID][]float64, error) {
	out := map[core.ID][]float64{}
	for _, nt := range memTables {
		q := fmt.Sprintf("MATCH (n:%s) WHERE n.embedding <> '' RETURN n.id, n.embedding ORDER BY n.seq", nt)
		rows, err := g.queryRows(q)
		if err != nil {
			return nil, fmt.Errorf("graphstore: load embeddings from %s: %w", nt, err)
		}
		for _, row := range rows {
			id := core.ID(str(row[0]))
			if _, seen := out[id]; seen {
				continue
			}
			var vec []float64
			if err := json.Unmarshal([]byte(str(row[1])), &vec); err != nil {
				return nil, fmt.Errorf("graphstore: decode embedding for %s: %w", id, err)
			}
			out[id] = vec
		}
	}
	return out, nil
}

// MapEmbedder is a deterministic, in-process Embedder backed by a fixed
// text → vector table (e.g. the pre-computed corpus embeddings in
// internal/bench/testdata). Unknown text is an error — a map has no provider
// to fall back to, and pretending otherwise would hide exactly the outage the
// degradation path exists for.
type MapEmbedder struct {
	Vectors map[string][]float64
}

func (m MapEmbedder) Name() string { return "map-embedder" }

func (m MapEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	v, ok := m.Vectors[text]
	if !ok {
		return nil, fmt.Errorf("map-embedder: no vector for text (%d known)", len(m.Vectors))
	}
	return v, nil
}

// GeminiEmbedder embeds via the Gemini embedContents API — the ADR-002
// embedding source (gemini-embedding-001, the same model the benchmark
// baseline uses). stdlib HTTP only; any failure is returned as-is and the
// caller degrades to lexical recall.
type GeminiEmbedder struct {
	APIKey string
	// Model defaults to gemini-embedding-001 when empty.
	Model string
	// Client defaults to a 30s-timeout http.Client when nil.
	Client *http.Client
}

func (g GeminiEmbedder) Name() string { return "gemini/" + g.model() }

func (g GeminiEmbedder) model() string {
	if g.Model == "" {
		return "gemini-embedding-001"
	}
	return g.Model
}

func (g GeminiEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(map[string]any{
		"model":   "models/" + g.model(),
		"content": map[string]any{"parts": []map[string]string{{"text": text}}},
	})
	if err != nil {
		return nil, err
	}
	url := "https://generativelanguage.googleapis.com/v1beta/models/" + g.model() + ":embedContents"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)

	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("gemini embed: HTTP %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Embedding struct {
			Values []float64 `json:"values"`
		} `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("gemini embed: decode: %w", err)
	}
	if len(out.Embedding.Values) == 0 {
		return nil, fmt.Errorf("gemini embed: empty embedding in response")
	}
	return out.Embedding.Values, nil
}
