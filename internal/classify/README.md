# Memory-worthiness classifier

`classify.Load(reader)` reads JSONL training examples;
`classify.Train(examples)` returns `(*Model, error)`;
`model.Predict(text)` returns `(label, confidence)`.
Labels are `worthy` and `not_worthy`. Confidence is the model's estimated
probability of the returned label, not a calibrated reliability guarantee.
The `type` annotation is retained in `Example` but is not predicted.

The model uses lowercase Unicode letter/digit tokens, sublinear term frequency
(`1 + log(count)`), smoothed IDF (`log((N+1)/(df+1)) + 1`), and L2-normalized
TF-IDF vectors. Full-batch gradient descent trains binary logistic regression
with an intercept and L2 weight regularization. Training uses fixed defaults
(1,000 steps, learning rate 1, regularization 0.001), and classification uses a
0.5 threshold. Unknown tokens are ignored; empty or entirely unknown input
uses the learned intercept. There are no external ML dependencies.

Each CLI invocation reads the current training file and fits a fresh in-memory
model. It never writes or loads model artifacts. Callers using the Go API
should likewise load and train at the start of each invocation.

From the repository root, classify a whole text file or one complete stdin turn:

```sh
go run ./cmd/enso-classify turn.txt
cat turn.txt | go run ./cmd/enso-classify
go run ./cmd/enso-classify -training /path/to/training_examples.jsonl turn.txt
```

Output is one JSON object with `label` and `confidence`. Errors go to stderr
and exit nonzero. The default training path is relative to the working
directory; provide `-training` when running elsewhere.

## Held-out evaluation

Reproduce with:

```sh
go test ./internal/classify -run TestHeldOutEvaluation -v
go test ./internal/classify -run '^$' -bench BenchmarkTrain -benchmem
```

The supplied dataset has 62 rows (26 worthy, 36 not worthy), representing 56
unique texts. Evaluation uses deterministic, class-stratified five-fold
cross-validation (shuffle seed 42); identical texts stay in the same fold.
Every row is predicted once while held out. Each fold rebuilds the vocabulary,
IDF and regression exclusively from its 48–51 training rows. No hyperparameter
or threshold search was performed against these held-out results.

| Evaluation unit | Accuracy | Worthy precision | Worthy recall | TP | TN | FP | FN |
| --- | --- | --- | --- | --- | --- | --- | --- |
| All 62 rows | 96.77% | 92.86% | 100% | 26 | 34 | 2 | 0 |
| 56 unique texts | 96.43% | 92.31% | 100% | 24 | 30 | 2 | 0 |

The false positives were a brief acknowledgment mentioning an open project
question and a raw shadow-health outage notification. These numbers are
preliminary: the dataset is small, repetitive, and drawn from a narrow
conversation context. Related turns and similar status templates can still
cross folds even though exact duplicates cannot. This is not a session-grouped
or future-time holdout; 100% observed recall does not establish perfect recall
on new conversations. Test on independently labeled new turns before using
the classifier as an unattended ingestion gate.
