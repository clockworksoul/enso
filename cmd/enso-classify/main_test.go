package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	training := filepath.Join(dir, "training.jsonl")
	input := filepath.Join(dir, "turn.txt")
	data := `{"text":"durable decision","label":"worthy","type":"Decision"}` + "\n" + `{"text":"nothing pending","label":"not_worthy","type":null}`
	if err := os.WriteFile(training, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("durable decision"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-training", training}, {"-training", training, "-"}, {"-training", training, input}} {
		var out, stderr bytes.Buffer
		if err := run(args, strings.NewReader("durable decision"), &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var prediction struct {
			Label      string  `json:"label"`
			Confidence float64 `json:"confidence"`
		}
		if err := json.Unmarshal(out.Bytes(), &prediction); err != nil {
			t.Fatal(err)
		}
		if prediction.Label != "worthy" || prediction.Confidence < 0.5 || prediction.Confidence > 1 {
			t.Fatalf("unexpected prediction: %+v", prediction)
		}
	}
	// Reusing the same path must still load and train from its current contents.
	reversed := strings.NewReplacer(`"worthy"`, `"not_worthy"`, `"not_worthy"`, `"worthy"`).Replace(data)
	if err := os.WriteFile(training, []byte(reversed), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := run([]string{"-training", training}, strings.NewReader("durable decision"), &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"label":"not_worthy"`) {
		t.Fatalf("invocation ignored changed training file: %s", &out)
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("{broken}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-unknown"},
		{"one", "two"},
		{"-training", filepath.Join(dir, "missing.jsonl")},
		{"-training", bad},
		{"-training", "../../internal/classify/testdata/training_examples.jsonl", filepath.Join(dir, "missing.txt")},
	} {
		var out, stderr bytes.Buffer
		if err := run(args, strings.NewReader("turn"), &out, &stderr); err == nil {
			t.Fatalf("run(%v) should fail", args)
		}
		if out.Len() != 0 {
			t.Fatalf("failed invocation emitted prediction: %s", &out)
		}
	}
}
