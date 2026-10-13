package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

func TestMarkRecalledRoundTrip(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "enso-mark-recalled")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	root := t.TempDir()
	ctx := context.Background()
	store := mdstore.NewFSStore(root)
	entry, err := core.NewEntry(core.NewEntryParams{
		ID: "mem:2026-01-01-recall", Type: core.TypeFact,
		Content: "A materially used memory", EncodedTime: time.Now().UTC().Add(-72 * time.Hour),
		Confidence: core.ConfHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, []core.Entry{entry}, nil); err != nil {
		t.Fatal(err)
	}

	originals, _, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Truncate(time.Second)
	if output, err := exec.Command(binary, "-root", root, "-id", string(entry.ID)).CombinedOutput(); err != nil {
		t.Fatalf("mark recalled: %v\n%s", err, output)
	}
	after := time.Now().UTC()
	entries, edges, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || len(edges) != 0 {
		t.Fatalf("expected original and appended update, got %d entries and %d edges", len(entries), len(edges))
	}
	if !reflect.DeepEqual(entries[0], originals[0]) {
		t.Fatal("reinforcement rewrote the original entry")
	}
	original, bumped := entries[0], entries[1]
	if bumped.Temporal.LastRefTime.Before(before) || bumped.Temporal.LastRefTime.After(after) {
		t.Fatalf("recall timestamp is not now: %v", bumped.Temporal.LastRefTime)
	}
	if bumped.Temporal.SFloor <= original.Temporal.SFloor || bumped.Temporal.SLast != original.Temporal.SCap {
		t.Fatalf("reinforcement not persisted: original %+v, bumped %+v", original.Temporal, bumped.Temporal)
	}
	bumped.Temporal = original.Temporal
	if !reflect.DeepEqual(bumped, original) {
		t.Fatal("reinforcement changed non-temporal fields")
	}

	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing root", []string{"-id", string(entry.ID)}, "-root is required"},
		{"missing id", []string{"-root", root}, "-id is required"},
		{"unknown id", []string{"-root", root, "-id", "mem:missing"}, "no record with that id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := exec.Command(binary, tt.args...).CombinedOutput()
			if err == nil || !strings.Contains(string(output), "enso-mark-recalled:") || !strings.Contains(string(output), tt.want) {
				t.Fatalf("expected CLI failure containing %q, got %v\n%s", tt.want, err, output)
			}
		})
	}
}
