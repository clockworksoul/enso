// Package classify learns binary memory-worthiness from labeled conversation turns.
// Models live only in memory; callers load the JSONL source and train each invocation.
package classify

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	Worthy    = "worthy"
	NotWorthy = "not_worthy"
)

// Example is one training turn. Type preserves the source annotation but is not
// used by the binary classifier (JSON null decodes as an empty string).
type Example struct {
	Text  string `json:"text"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

func validate(e Example) error {
	if strings.TrimSpace(e.Text) == "" {
		return fmt.Errorf("empty text")
	}
	if e.Label != Worthy && e.Label != NotWorthy {
		return fmt.Errorf("invalid label %q", e.Label)
	}
	return nil
}

// Load reads JSONL examples, reporting malformed records with their line number.
// Blank lines are ignored. Train additionally requires both classes to be present.
func Load(r io.Reader) ([]Example, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	var examples []Example
	line := 0
	for scanner.Scan() {
		line++
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var e Example
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if err := validate(e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		examples = append(examples, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read line %d: %w", line+1, err)
	}
	if len(examples) == 0 {
		return nil, fmt.Errorf("no training examples")
	}
	return examples, nil
}

// Model is an immutable TF-IDF vocabulary and logistic regression fit. Predict
// may be called concurrently. There is deliberately no persistence API.
type Model struct {
	vocabulary map[string]int
	idf        []float64
	weights    []float64
	bias       float64
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

type feature struct {
	index int
	value float64
}

// vector uses sublinear TF (1 + log count), smoothed IDF, and L2 normalization.
// Unknown terms are ignored; sorting makes floating-point arithmetic reproducible.
func (m *Model) vector(text string) []feature {
	counts := make(map[int]int)
	for _, token := range tokenize(text) {
		if index, ok := m.vocabulary[token]; ok {
			counts[index]++
		}
	}
	indices := make([]int, 0, len(counts))
	for index := range counts {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	x := make([]feature, 0, len(indices))
	var norm float64
	for _, index := range indices {
		value := (1 + math.Log(float64(counts[index]))) * m.idf[index]
		x = append(x, feature{index, value})
		norm += value * value
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range x {
			x[i].value /= norm
		}
	}
	return x
}

func sigmoid(z float64) float64 {
	if z >= 0 {
		return 1 / (1 + math.Exp(-z))
	}
	e := math.Exp(z)
	return e / (1 + e)
}

func (m *Model) probability(x []feature) float64 {
	z := m.bias
	for _, f := range x {
		z += m.weights[f.index] * f.value
	}
	return sigmoid(z)
}

// Train fits a fresh model using only examples, including vocabulary and IDF.
// It minimizes mean binary cross-entropy plus L2 weight regularization using
// full-batch gradient descent. Fixed conservative defaults keep the API small.
func Train(examples []Example) (*Model, error) {
	df := make(map[string]int)
	positives := 0
	for i, e := range examples {
		if err := validate(e); err != nil {
			return nil, fmt.Errorf("example %d: %w", i+1, err)
		}
		if e.Label == Worthy {
			positives++
		}
		seen := make(map[string]bool)
		for _, token := range tokenize(e.Text) {
			if !seen[token] {
				df[token]++
				seen[token] = true
			}
		}
	}
	if positives == 0 || positives == len(examples) {
		return nil, fmt.Errorf("training requires both worthy and not_worthy examples")
	}
	if len(df) == 0 {
		return nil, fmt.Errorf("training vocabulary is empty")
	}
	terms := make([]string, 0, len(df))
	for term := range df {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	m := &Model{
		vocabulary: make(map[string]int, len(terms)),
		idf:        make([]float64, len(terms)),
		weights:    make([]float64, len(terms)),
		bias:       math.Log(float64(positives) / float64(len(examples)-positives)),
	}
	for i, term := range terms {
		m.vocabulary[term] = i
		m.idf[i] = math.Log(float64(len(examples)+1)/float64(df[term]+1)) + 1
	}
	xs := make([][]feature, len(examples))
	for i, e := range examples {
		xs[i] = m.vector(e.Text)
	}
	const epochs, learningRate, regularization = 1000, 1.0, 0.001
	gradient := make([]float64, len(terms))
	n := float64(len(examples))
	for epoch := 0; epoch < epochs; epoch++ {
		clear(gradient)
		var biasGradient float64
		for i, x := range xs {
			y := 0.0
			if examples[i].Label == Worthy {
				y = 1
			}
			residual := m.probability(x) - y
			biasGradient += residual
			for _, f := range x {
				gradient[f.index] += residual * f.value
			}
		}
		for i := range m.weights {
			m.weights[i] -= learningRate * (gradient[i]/n + regularization*m.weights[i])
		}
		m.bias -= learningRate * biasGradient / n
	}
	return m, nil
}

// Predict returns the label at a fixed 0.5 threshold and the estimated probability
// of that label. Confidence is an uncalibrated model score, not a guarantee of
// correctness. Empty or entirely unknown text uses the learned intercept alone.
func (m *Model) Predict(text string) (label string, confidence float64) {
	p := m.probability(m.vector(text))
	if p >= 0.5 {
		return Worthy, p
	}
	return NotWorthy, 1 - p
}
