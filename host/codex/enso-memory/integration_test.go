package ensomemory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

func TestUserPromptSubmitEndToEnd(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is required by the Codex adapter")
	}
	_, file, _, _ := runtime.Caller(0)
	pluginRoot := filepath.Dir(file)
	repoRoot := filepath.Clean(filepath.Join(pluginRoot, "..", "..", ".."))
	binary := filepath.Join(t.TempDir(), "enso-recall")

	build := exec.Command("go", "build", "-o", binary, "./cmd/enso-recall")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build enso-recall: %v\n%s", err, output)
	}

	corpus := seedCorpus(t)
	event, _ := json.Marshal(map[string]any{
		"session_id":      "thr_e2e",
		"turn_id":         "turn_e2e",
		"cwd":             repoRoot,
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "what happened with granola?",
		"permission_mode": "default",
		"transcript_path": nil,
	})
	hook := exec.Command(python, filepath.Join(pluginRoot, "scripts", "user_prompt_submit.py"))
	hook.Stdin = bytes.NewReader(event)
	hook.Env = append(os.Environ(),
		"ENSO_CODEX_MODE=live",
		"ENSO_CORPUS_ROOT="+corpus,
		"ENSO_RECALL_BIN="+binary,
		"ENSO_CODEX_SHADOW_DIR="+t.TempDir(),
		"GEMINI_API_KEY=",
	)
	output, err := hook.Output()
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	var response struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("hook output is not JSON: %v\n%s", err, output)
	}
	context := response.HookSpecificOutput.AdditionalContext
	if !strings.Contains(context, "mem:2026-07-04-granola-uninstalled") {
		t.Fatalf("current entry missing from context:\n%s", context)
	}
	if strings.Contains(context, "mem:2026-07-03-granola-installed") {
		t.Fatalf("superseded entry surfaced:\n%s", context)
	}
}

func seedCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ctx := context.Background()
	at := func(day int) time.Time {
		return time.Date(2026, 7, day, 9, 0, 0, 0, time.UTC)
	}
	entry := func(day int, label, content string) core.Entry {
		id, err := core.NewID(at(day), label)
		if err != nil {
			t.Fatal(err)
		}
		value, err := core.NewEntry(core.NewEntryParams{
			ID: id, Type: core.TypeFact, Content: content,
			EncodedTime: at(day), Confidence: core.ConfHigh,
			Tags: []string{"granola"}, About: []string{},
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	stale := entry(3, "granola-installed", "granola stays installed")
	current := entry(4, "granola-uninstalled", "granola was uninstalled")
	store := mdstore.NewFSStore(root)
	if err := store.Append(ctx, []core.Entry{stale}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Supersede(ctx, stale, current); err != nil {
		t.Fatal(err)
	}
	return root
}
