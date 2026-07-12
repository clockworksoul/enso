package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// SemanticModel implements QueryModel using pre-computed Gemini embeddings and
// cosine similarity. It is the "session-memory-equivalent" baseline: it ranks
// candidates by semantic similarity to the query without any structural
// knowledge (no supersession edges, no temporal filtering).
//
// This tests whether embedding-based retrieval — the core of OpenClaw's
// memory_search — can naturally prefer the current belief over a stale one
// purely on semantic grounds. If it can, session memory alone may be sufficient
// for supersession. If it cannot (score ≈ 0.50), structural knowledge (Ensō's
// SUPERSEDES edges + ValidUntil) is load-bearing.
type SemanticModel struct {
	// embeddings maps text content → embedding vector, loaded from the
	// pre-computed embeddings JSONL produced by cmd/embed-corpus.
	embeddings map[string][]float64
}

// Name implements QueryModel.
func (s SemanticModel) Name() string { return "semantic-embedding" }

// LoadSemanticModel reads the embeddings JSONL file and returns a ready model.
// Returns (nil, nil) if the file does not exist — callers should skip
// gracefully.
func LoadSemanticModel(path string) (*SemanticModel, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	type record struct {
		Text      string    `json:"text"`
		Embedding []float64 `json:"embedding"`
	}

	m := &SemanticModel{embeddings: map[string][]float64{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20) // 4MB — embeddings are large
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		m.embeddings[r.Text] = r.Embedding
	}
	return m, sc.Err()
}

// RankQuery implements QueryModel. It ranks candidates by cosine similarity to
// the query, highest first. Candidates with no embedding (not found in the
// pre-computed map) are sorted last.
func (s SemanticModel) RankQuery(query string, candidates []core.Entry, _ []core.Edge, _ time.Time) []core.Entry {
	queryEmb, ok := s.embeddings[query]
	if !ok {
		// No query embedding: return candidates in original order.
		return candidates
	}

	type scored struct {
		entry core.Entry
		score float64
	}
	ss := make([]scored, len(candidates))
	for i, c := range candidates {
		emb, ok := s.embeddings[c.Content]
		if !ok {
			ss[i] = scored{c, -2} // sort last
			continue
		}
		ss[i] = scored{c, cosine(queryEmb, emb)}
	}

	// Sort descending by score.
	for i := range ss {
		for j := i + 1; j < len(ss); j++ {
			if ss[j].score > ss[i].score {
				ss[i], ss[j] = ss[j], ss[i]
			}
		}
	}

	out := make([]core.Entry, len(ss))
	for i, s := range ss {
		out[i] = s.entry
	}
	return out
}

// cosine returns the cosine similarity between two vectors. Returns 0 if
// either vector has zero magnitude.
func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
