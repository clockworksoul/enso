// enso-classify trains from JSONL on each invocation and classifies one raw turn.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/clockworksoul/enso/internal/classify"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "enso-classify: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("enso-classify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	training := flags.String("training", "internal/classify/testdata/training_examples.jsonl", "training JSONL file (read afresh every invocation)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: enso-classify [-training examples.jsonl] [text-file|-]\nWithout a text file, reads one complete turn from stdin.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("expected at most one text file")
	}
	f, err := os.Open(*training)
	if err != nil {
		return fmt.Errorf("open training data: %w", err)
	}
	examples, err := classify.Load(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("load training data: %w", err)
	}
	model, err := classify.Train(examples)
	if err != nil {
		return err
	}
	input := stdin
	if flags.NArg() == 1 && flags.Arg(0) != "-" {
		f, err := os.Open(flags.Arg(0))
		if err != nil {
			return fmt.Errorf("open input: %w", err)
		}
		defer f.Close()
		input = f
	}
	text, err := io.ReadAll(input)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	label, confidence := model.Predict(string(text))
	return json.NewEncoder(stdout).Encode(struct {
		Label      string  `json:"label"`
		Confidence float64 `json:"confidence"`
	}{label, confidence})
}
