// embed-corpus pre-computes Gemini embeddings for every query, stale_text, and
// current_text in the git history corpus, and writes them to a supplementary
// JSONL file that the semantic bench model loads at test time.
//
// Usage:
//
//	GEMINI_API_KEY=<key> embed-corpus \
//	  -corpus internal/bench/testdata/git_history_cases.jsonl \
//	  -out    internal/bench/testdata/git_history_embeddings.jsonl
//
// The output file has one JSON record per unique text:
//
//	{"text": "<original text>", "embedding": [3072 floats]}
//
// Running this overwrites any existing embeddings file. The bench test will
// skip gracefully if the file is absent, so regeneration is optional.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	geminiModel    = "models/gemini-embedding-001"
	batchEndpoint  = "https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:batchEmbedContents"
	maxBatchSize   = 100
	retryMax       = 4
	retryBaseDelay = 2 * time.Second
)

// corpusRecord mirrors the git_history_cases.jsonl schema (only the fields we
// need for embedding).
type corpusRecord struct {
	ID          string `json:"id"`
	Query       string `json:"query"`
	StaleText   string `json:"stale_text"`
	CurrentText string `json:"current_text"`
}

// embeddingRecord is one line of the output JSONL.
type embeddingRecord struct {
	Text      string    `json:"text"`
	Embedding []float64 `json:"embedding"`
}

// batchRequest / batchResponse mirror the Gemini batchEmbedContents API shape.
type batchRequest struct {
	Requests []embedRequest `json:"requests"`
}
type embedRequest struct {
	Model   string       `json:"model"`
	Content embedContent `json:"content"`
}
type embedContent struct {
	Parts []embedPart `json:"parts"`
}
type embedPart struct {
	Text string `json:"text"`
}
type batchResponse struct {
	Embeddings []struct {
		Values []float64 `json:"values"`
	} `json:"embeddings"`
}

func main() {
	corpusPath := flag.String("corpus", "internal/bench/testdata/git_history_cases.jsonl", "input corpus JSONL")
	outPath := flag.String("out", "internal/bench/testdata/git_history_embeddings.jsonl", "output embeddings JSONL")
	flag.Parse()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY env var required")
	}

	records, err := loadCorpus(*corpusPath)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}
	fmt.Fprintf(os.Stderr, "loaded %d corpus records\n", len(records))

	// Collect all unique texts in a stable order: query, stale, current for
	// each record. Deduplicate so we don't re-embed identical texts.
	type textKey struct{ id, role string }
	seen := map[string]bool{}
	var texts []string
	for _, r := range records {
		for _, t := range []string{r.Query, r.StaleText, r.CurrentText} {
			if !seen[t] {
				seen[t] = true
				texts = append(texts, t)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "unique texts to embed: %d\n", len(texts))

	// Embed in batches.
	embedMap := map[string][]float64{}
	for start := 0; start < len(texts); start += maxBatchSize {
		end := start + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]
		fmt.Fprintf(os.Stderr, "embedding batch %d–%d/%d…\n", start+1, end, len(texts))
		embs, err := embedBatch(apiKey, batch)
		if err != nil {
			log.Fatalf("embed batch %d–%d: %v", start, end, err)
		}
		for i, text := range batch {
			embedMap[text] = embs[i]
		}
	}

	// Write output JSONL: one record per unique text.
	f, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create %s: %v", *outPath, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, text := range texts {
		if err := enc.Encode(embeddingRecord{Text: text, Embedding: embedMap[text]}); err != nil {
			log.Fatalf("encode: %v", err)
		}
	}
	fmt.Fprintf(os.Stderr, "wrote %d embedding records to %s\n", len(texts), *outPath)
}

func loadCorpus(path string) ([]corpusRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []corpusRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r corpusRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, sc.Err()
}

func embedBatch(apiKey string, texts []string) ([][]float64, error) {
	reqs := make([]embedRequest, len(texts))
	for i, t := range texts {
		reqs[i] = embedRequest{
			Model:   geminiModel,
			Content: embedContent{Parts: []embedPart{{Text: t}}},
		}
	}
	body, err := json.Marshal(batchRequest{Requests: reqs})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s?key=%s", batchEndpoint, apiKey)
	var lastErr error
	for attempt := range retryMax {
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = err
			time.Sleep(retryBaseDelay * (1 << attempt))
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			delay := retryBaseDelay * (1 << attempt)
			fmt.Fprintf(os.Stderr, "rate limited, sleeping %v…\n", delay)
			time.Sleep(delay)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
		}

		var br batchResponse
		if err := json.Unmarshal(raw, &br); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(br.Embeddings) != len(texts) {
			return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(br.Embeddings))
		}
		out := make([][]float64, len(texts))
		for i, e := range br.Embeddings {
			out[i] = e.Values
		}
		return out, nil
	}
	return nil, fmt.Errorf("all retries exhausted: %v", lastErr)
}
