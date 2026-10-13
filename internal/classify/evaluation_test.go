package classify

import (
	"math/rand"
	"testing"
)

// TestHeldOutEvaluation uses stratified five-fold cross-validation. Exact text
// duplicates stay together; vocabulary, IDF and regression are refit on each
// training fold. No held-out labels influence the model or decision threshold.
func TestHeldOutEvaluation(t *testing.T) {
	examples := trainingExamples(t)
	groups := make([][]Example, 0)
	byText := make(map[string]int)
	for _, e := range examples {
		index, found := byText[e.Text]
		if !found {
			index = len(groups)
			byText[e.Text] = index
			groups = append(groups, nil)
		} else if groups[index][0].Label != e.Label {
			t.Fatal("conflicting labels for identical texts")
		}
		groups[index] = append(groups[index], e)
	}
	const folds = 5
	assignment := make([]int, len(groups))
	rng := rand.New(rand.NewSource(42))
	for _, label := range []string{Worthy, NotWorthy} {
		var indices []int
		for i, group := range groups {
			if group[0].Label == label {
				indices = append(indices, i)
			}
		}
		rng.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })
		for i, index := range indices {
			assignment[index] = i % folds
		}
	}
	var rows, unique confusion
	for fold := 0; fold < folds; fold++ {
		var train []Example
		for i, group := range groups {
			if assignment[i] != fold {
				train = append(train, group...)
			}
		}
		m, err := Train(train)
		if err != nil {
			t.Fatal(err)
		}
		var heldOut confusion
		for i, group := range groups {
			if assignment[i] != fold {
				continue
			}
			label, _ := m.Predict(group[0].Text)
			unique.add(group[0].Label, label)
			for _, e := range group {
				heldOut.add(e.Label, label)
				rows.add(e.Label, label)
			}
			if label != group[0].Label {
				t.Logf("fold %d miss: actual=%s predicted=%s text=%.100q", fold+1, group[0].Label, label, group[0].Text)
			}
		}
		t.Logf("fold %d: train=%d test=%d TP=%d TN=%d FP=%d FN=%d", fold+1, len(train), heldOut.total(), heldOut.tp, heldOut.tn, heldOut.fp, heldOut.fn)
	}
	if rows.total() != len(examples) || unique.total() != len(groups) {
		t.Fatal("cross-validation did not evaluate every example exactly once")
	}
	for _, result := range []struct {
		name string
		c    confusion
	}{{"all rows", rows}, {"unique texts", unique}} {
		c := result.c
		t.Logf("%s: n=%d TP=%d TN=%d FP=%d FN=%d accuracy=%.4f precision=%.4f recall=%.4f", result.name, c.total(), c.tp, c.tn, c.fp, c.fn,
			float64(c.tp+c.tn)/float64(c.total()), ratio(c.tp, c.tp+c.fp), ratio(c.tp, c.tp+c.fn))
	}
}

type confusion struct{ tp, tn, fp, fn int }

func (c *confusion) add(actual, predicted string) {
	switch {
	case actual == Worthy && predicted == Worthy:
		c.tp++
	case actual == NotWorthy && predicted == NotWorthy:
		c.tn++
	case predicted == Worthy:
		c.fp++
	default:
		c.fn++
	}
}

func (c confusion) total() int { return c.tp + c.tn + c.fp + c.fn }

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
