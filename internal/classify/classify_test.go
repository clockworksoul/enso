package classify

import (
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	examples, err := Load(strings.NewReader("\n" + `{"text":"keep this","label":"worthy","type":"Fact"}` + "\n" + `{"text":"skip this","label":"not_worthy","type":null}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Example{{"keep this", Worthy, "Fact"}, {"skip this", NotWorthy, ""}}
	if !reflect.DeepEqual(examples, want) {
		t.Fatalf("Load = %#v, want %#v", examples, want)
	}
	for _, tc := range []struct{ name, input, errorText string }{
		{"empty", "\n", "no training examples"},
		{"invalid json", "\n{", "line 2:"},
		{"invalid label", `{"text":"hi","label":"other"}`, "invalid label"},
		{"missing label", `{"text":"hi"}`, "invalid label"},
		{"empty text", `{"text":"  ","label":"worthy"}`, "empty text"},
		{"multiple objects", `{"text":"hi","label":"worthy"} {}`, "line 1:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(tc.input)); err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("Load error = %v, want %q", err, tc.errorText)
			}
		})
	}
	if _, err := Load(brokenReader{}); !errors.Is(err, errRead) {
		t.Fatalf("Load read error = %v", err)
	}
}

func TestLoadLongTurn(t *testing.T) {
	// Raw turns can exceed Scanner's default 64 KiB limit.
	text := strings.Repeat("conversation ", 10000)
	input := `{"text":"` + text + `","label":"worthy","type":"Fact"}`
	examples, err := Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) != 1 || examples[0].Text != text {
		t.Fatal("long turn was truncated")
	}
	// The explicit record limit must return an error rather than silently drop data.
	input = `{"text":"` + strings.Repeat("x", 4*1024*1024) + `","label":"worthy"}`
	if _, err := Load(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "read line 1:") {
		t.Fatalf("oversize record error = %v", err)
	}
}

var errRead = errors.New("read failed")

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errRead }

func TestTokenize(t *testing.T) {
	want := []string{"ensō", "café", "42", "workspace", "enso", "can", "t"}
	if got := tokenize("ENSŌ café 42 ~/workspace/enso can't!"); !reflect.DeepEqual(got, want) {
		t.Fatalf("tokenize = %v, want %v", got, want)
	}
}

func TestTFIDF(t *testing.T) {
	m, err := Train([]Example{{Text: "common rare rare", Label: Worthy}, {Text: "common other", Label: NotWorthy}})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.idf[m.vocabulary["common"]]; got != 1 {
		t.Fatalf("common IDF = %v, want 1", got)
	}
	rareIDF := math.Log(3.0/2) + 1
	if got := m.idf[m.vocabulary["rare"]]; math.Abs(got-rareIDF) > 1e-12 {
		t.Fatalf("rare IDF = %v, want %v", got, rareIDF)
	}
	x := m.vector("COMMON rare rare unseen")
	rareTFIDF := (1 + math.Log(2)) * rareIDF
	norm := math.Sqrt(1 + rareTFIDF*rareTFIDF)
	if len(x) != 2 {
		t.Fatalf("vector = %v, want 2 known terms", x)
	}
	for _, f := range x {
		want := 1 / norm
		if f.index == m.vocabulary["rare"] {
			want = rareTFIDF / norm
		}
		if math.Abs(f.value-want) > 1e-12 {
			t.Errorf("feature %d = %v, want %v", f.index, f.value, want)
		}
	}
	if _, ok := m.vocabulary["unseen"]; ok {
		t.Fatal("prediction expanded the training vocabulary")
	}
}

func TestTrainPredict(t *testing.T) {
	examples := []Example{
		{Text: "durable architectural decision", Label: Worthy},
		{Text: "durable architectural insight", Label: Worthy},
		{Text: "nothing pending heartbeat", Label: NotWorthy},
		{Text: "nothing pending status", Label: NotWorthy},
	}
	m, err := Train(examples)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ text, want string }{
		{"DURABLE architectural", Worthy},
		{"nothing pending", NotWorthy},
	} {
		label, confidence := m.Predict(tc.text)
		if label != tc.want || confidence < 0.5 || confidence > 1 || math.IsNaN(confidence) {
			t.Errorf("Predict(%q) = %s, %v; want %s and finite confidence in [0.5,1]", tc.text, label, confidence, tc.want)
		}
	}
	for _, text := range []string{"", "!?", "unseenword"} {
		_, confidence := m.Predict(text)
		p := sigmoid(m.bias)
		if want := math.Max(p, 1-p); confidence != want {
			t.Errorf("unknown text confidence = %v, want intercept %v", confidence, want)
		}
	}
	again, err := Train(examples)
	if err != nil || !reflect.DeepEqual(m, again) {
		t.Fatalf("training is not deterministic: %v", err)
	}
	// A new training call must use the new labels and leave the old model intact.
	for i := range examples {
		if examples[i].Label == Worthy {
			examples[i].Label = NotWorthy
		} else {
			examples[i].Label = Worthy
		}
	}
	reversed, err := Train(examples)
	if err != nil {
		t.Fatal(err)
	}
	if label, _ := reversed.Predict("durable architectural"); label != NotWorthy {
		t.Fatal("fresh training ignored changed labels")
	}
	if label, _ := m.Predict("durable architectural"); label != Worthy {
		t.Fatal("fresh training mutated earlier model")
	}
}

func TestTrainInvalid(t *testing.T) {
	for _, tc := range []struct {
		name     string
		examples []Example
	}{
		{"empty", nil},
		{"one class", []Example{{Text: "hello", Label: Worthy}}},
		{"bad label", []Example{{Text: "hello", Label: "bad"}}},
		{"empty text", []Example{{Label: Worthy}}},
		{"no vocabulary", []Example{{Text: "!!!", Label: Worthy}, {Text: "???", Label: NotWorthy}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Train(tc.examples); err == nil {
				t.Fatal("Train should reject invalid examples")
			}
		})
	}
}

func TestSigmoid(t *testing.T) {
	for _, z := range []float64{-1000, -1, 0, 1, 1000} {
		p := sigmoid(z)
		if math.IsNaN(p) || p < 0 || p > 1 || math.Abs(p+sigmoid(-z)-1) > 1e-12 {
			t.Errorf("unstable sigmoid(%v) = %v", z, p)
		}
	}
}

func trainingExamples(t testing.TB) []Example {
	t.Helper()
	f, err := os.Open("testdata/training_examples.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	examples, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	return examples
}

func BenchmarkTrain(b *testing.B) {
	examples := trainingExamples(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Train(examples); err != nil {
			b.Fatal(err)
		}
	}
}
