package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

func TestAppendWhitespaceRoundTrip(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "enso-append")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	const encoded = "2026-10-13T12:00:00Z"
	const event = "2026-10-12T09:00:00Z"
	encodedAt, _ := time.Parse(time.RFC3339, encoded)
	eventAt, _ := time.Parse(time.RFC3339, event)
	tests := []struct {
		name, content, tags, about string
	}{
		{"single line", "First paragraph. Second paragraph.", "memory capture, regression", "project:enso memory, person:matt"},
		{"multi-paragraph content", " \tFirst  paragraph.\n\nSecond\tparagraph.\r\n ", "memory capture, regression", "project:enso memory, person:matt"},
		{"multiline tags", "First paragraph. Second paragraph.", " memory\n\n capture,\tregression , , ", "project:enso memory, person:matt"},
		{"multiline about", "First paragraph. Second paragraph.", "memory capture, regression", " project:enso\n\n memory,\tperson:matt , , "},
		{"unicode whitespace", "\u2003First\u00a0paragraph.\u2028\u2029Second\tparagraph.\u2003", "memory\u00a0capture, regression", "project:enso\u2003memory, person:matt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			args := []string{
				"-root", root, "-type", "Fact", "-slug", "whitespace-regression",
				"-content", tt.content, "-tags", tt.tags, "-about", tt.about,
				"-confidence", "medium", "-encoded-time", encoded, "-event-time", event,
			}
			if output, err := exec.Command(binary, args...).CombinedOutput(); err != nil {
				t.Fatalf("append: %v\n%s", err, output)
			}
			files, err := filepath.Glob(filepath.Join(root, "memory", "*.md"))
			if err != nil || len(files) != 1 {
				t.Fatalf("expected one daily file, got %v (error: %v)", files, err)
			}
			doc, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			entries, edges, err := mdstore.Parse(string(doc))
			if err != nil {
				t.Fatalf("parse written block: %v\n%s", err, doc)
			}
			if len(entries) != 1 || len(edges) != 0 {
				t.Fatalf("expected one entry and no edges, got %d entries and %d edges", len(entries), len(edges))
			}
			if !strings.HasSuffix(string(entries[0].ID), "-whitespace-regression") {
				t.Fatalf("unexpected ID: %s", entries[0].ID)
			}
			want, err := core.NewEntry(core.NewEntryParams{
				ID: entries[0].ID, Type: core.TypeFact,
				Content:     "First paragraph. Second paragraph.",
				EncodedTime: encodedAt, EventTime: &eventAt, Confidence: core.ConfMedium,
				Tags:  []string{"memory capture", "regression"},
				About: []string{"project:enso memory", "person:matt"},
			})
			if err != nil {
				t.Fatal(err)
			}
			// Compare every field, including nullable and reserved temporal fields.
			if !reflect.DeepEqual(entries[0], want) {
				t.Fatalf("round-trip changed fields:\ngot  %+v\nwant %+v", entries[0], want)
			}
		})
	}
}
