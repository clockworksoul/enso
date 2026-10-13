package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/clockworksoul/enso/internal/graphstore"
)

func TestFreshnessMatchesLoader(t *testing.T) {
	root := seedCorpus(t)
	index := filepath.Join(root, defaultIndexRelPath)
	g, _, _, err := coldRebuild(context.Background(), index, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	g.Close()
	future := time.Now().Add(time.Hour)
	for _, name := range []string{"backup-last-run.txt", "nested/ignored.md", "ignored.MD"} {
		path := filepath.Join(root, "memory", name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, future, future); err != nil {
			t.Fatal(err)
		}
	}
	if fresh, err := indexIsFresh(index, root); err != nil || !fresh {
		t.Fatalf("non-corpus touch: fresh=%v err=%v", fresh, err)
	}
	path := filepath.Join(root, "memory", "2026-07-03.md")
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if fresh, err := indexIsFresh(index, root); err != nil || fresh {
		t.Fatalf("corpus touch: fresh=%v err=%v", fresh, err)
	}
}

type blockingEmbedder struct {
	marker string
	calls  int
}

func (e *blockingEmbedder) Name() string { return "blocked-test" }
func (e *blockingEmbedder) Embed(context.Context, string) ([]float64, error) {
	e.calls++
	if e.calls == 2 {
		if err := os.WriteFile(e.marker, nil, 0644); err != nil {
			panic(err)
		}
		select {}
	}
	return []float64{1, 0}, nil
}

// Run the same cold build used by the CLI in a killable process, with the
// provider blocked after the first entry has been appended.
func TestKilledRebuildHelper(t *testing.T) {
	if os.Getenv("ENSO_TEST_KILLED_BUILD") != "1" {
		return
	}
	_, _, _, err := coldRebuild(context.Background(), os.Getenv("ENSO_TEST_INDEX"), &blockingEmbedder{marker: os.Getenv("ENSO_TEST_MARKER")}, os.Getenv("ENSO_TEST_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestKilledRebuildPreservesLiveIndex(t *testing.T) {
	root := seedCorpus(t)
	index := filepath.Join(root, defaultIndexRelPath)
	g, _, _, err := coldRebuild(context.Background(), index, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	g.Close()
	before := hashTree(t, filepath.Dir(index))
	marker := filepath.Join(root, "blocked")
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledRebuildHelper$")
	lock, err := tryRebuildLock(index)
	if err != nil || lock == nil {
		t.Fatalf("parent lock: %v", err)
	}
	defer lock.Close()
	cmd.ExtraFiles = []*os.File{lock}
	cmd.Env = append(os.Environ(), rebuildLockEnv+"=3", "ENSO_TEST_KILLED_BUILD=1", "ENSO_TEST_ROOT="+root, "ENSO_TEST_INDEX="+index, "ENSO_TEST_MARKER="+marker)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lock.Close() // child alone now owns the inherited lock
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("build never reached blocked provider")
		}
		time.Sleep(10 * time.Millisecond)
	}
	duplicate, err := tryRebuildLock(index)
	if err != nil || duplicate != nil {
		t.Fatalf("worker failed to retain exclusive lock: %v", err)
	}
	// Existing readers remain usable while the worker has a partially built
	// replacement and owns the lock. Keep several snapshots open at once.
	var readers []*graphstore.GraphStore
	for range 3 {
		reader, n, built, err := openIndex(context.Background(), index, nil, root, false)
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, reader)
		if n != 4 || built {
			t.Fatalf("reader during build: entries=%d built=%v", n, built)
		}
	}
	for _, reader := range readers {
		reader.Close()
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	after := hashTree(t, filepath.Dir(index))
	// Compare only live files; temporary build artifacts may remain after SIGKILL.
	for path, hash := range before {
		if after[path] != hash {
			t.Fatalf("killed rebuild changed live file %s", path)
		}
	}
	g, n, built, err := openIndex(context.Background(), index, nil, root, false)
	if err != nil {
		t.Fatal(err)
	}
	g.Close()
	if n != 4 || built {
		t.Fatalf("old index unusable: entries=%d built=%v", n, built)
	}
	if remaining := hashTree(t, filepath.Dir(index)); !reflect.DeepEqual(before, remaining) {
		t.Fatal("next recall did not clean aborted build artifacts")
	}
}

type cancellingEmbedder struct{ cancel context.CancelFunc }

func (e cancellingEmbedder) Name() string { return "cancel-test" }
func (e cancellingEmbedder) Embed(context.Context, string) ([]float64, error) {
	e.cancel()
	return []float64{1, 0}, nil
}

func TestFailedRebuildPreservesLiveIndex(t *testing.T) {
	root := seedCorpus(t)
	index := filepath.Join(root, defaultIndexRelPath)
	g, _, _, err := coldRebuild(context.Background(), index, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	g.Close()
	before := hashTree(t, filepath.Dir(index))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if g, _, _, err := coldRebuild(ctx, index, cancellingEmbedder{cancel}, root); err == nil {
		g.Close()
		t.Fatal("cancelled rebuild succeeded")
	}
	if !reflect.DeepEqual(before, hashTree(t, filepath.Dir(index))) {
		t.Fatal("failed rebuild modified live index or left artifacts")
	}
}

func TestRebuildLockExclusion(t *testing.T) {
	index := filepath.Join(t.TempDir(), "index.kuzu")
	first, err := tryRebuildLock(index)
	if err != nil || first == nil {
		t.Fatalf("first lock: %v", err)
	}
	defer first.Close()
	second, err := tryRebuildLock(index)
	if err != nil || second != nil {
		t.Fatalf("duplicate lock: %v, %v", second, err)
	}
	first.Close()
	recovered, err := tryRebuildLock(index)
	if err != nil || recovered == nil {
		t.Fatalf("recover existing lock file: %v", err)
	}
	recovered.Close()
}

func TestRecallCLIBackgroundRebuild(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	binary := filepath.Join(t.TempDir(), "enso-recall")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	root := seedCorpus(t)
	index := filepath.Join(root, defaultIndexRelPath)
	recall := func() outputJSON {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, binary, "-root", root, "-query", "granola").Output()
		if err != nil {
			t.Fatalf("CLI: %v", err)
		}
		var o outputJSON
		if err := json.Unmarshal(out, &o); err != nil {
			t.Fatalf("JSON: %v: %s", err, out)
		}
		return o
	}
	cold := recall()
	if !cold.IndexBuilt || cold.IndexStale || cold.RebuildStarted {
		t.Fatalf("cold: %+v", cold)
	}
	backup := filepath.Join(root, "memory", "backup-last-run.txt")
	if err := os.WriteFile(backup, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if warm := recall(); warm.IndexBuilt || warm.IndexStale || warm.RebuildStarted {
		t.Fatalf("backup touch: %+v", warm)
	}
	// A real corpus edit, after the original snapshot's timestamp.
	path := filepath.Join(root, "memory", "2026-07-03.md")
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	stale := recall()
	if stale.IndexBuilt || !stale.IndexStale || !stale.RebuildStarted || stale.CorpusEntries != cold.CorpusEntries {
		t.Fatalf("stale CLI: %+v", stale)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		fresh, err := indexIsFresh(index, root)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := tryRebuildLock(index)
		if err != nil {
			t.Fatal(err)
		}
		if lock != nil {
			lock.Close()
			if fresh {
				break
			}
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(index + ".rebuild.log")
			t.Fatalf("background worker did not finish: %s", log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if warm := recall(); warm.IndexBuilt || warm.IndexStale || warm.RebuildStarted || warm.CorpusEntries != cold.CorpusEntries {
		t.Fatalf("after background publication: %+v", warm)
	}
}

func TestAtomicPublicationKeepsOpenReaderUsable(t *testing.T) {
	root := seedCorpus(t)
	index := filepath.Join(root, defaultIndexRelPath)
	old, _, _, err := coldRebuild(context.Background(), index, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	data, err := os.ReadFile(filepath.Join(root, "memory", "2026-07-03.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "memory", "copy.md"), data, 0644); err != nil {
		t.Fatal(err)
	}
	replacement, n, _, err := coldRebuild(context.Background(), index, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if n <= 4 {
		t.Fatalf("replacement did not include added corpus file: %d", n)
	}
	entries, _, err := old.Load(context.Background())
	if err != nil || len(entries) != 4 {
		t.Fatalf("old open snapshot after rename: entries=%d err=%v", len(entries), err)
	}
	entries, _, err = replacement.Load(context.Background())
	if err != nil || len(entries) != n {
		t.Fatalf("replacement snapshot: entries=%d err=%v", len(entries), err)
	}
}

type touchingEmbedder struct{ path string }

func (e touchingEmbedder) Name() string { return "touch-test" }
func (e touchingEmbedder) Embed(context.Context, string) ([]float64, error) {
	now := time.Now()
	if err := os.Chtimes(e.path, now, now); err != nil {
		return nil, err
	}
	return []float64{1, 0}, nil
}

func TestCorpusEditDuringBuildRemainsStale(t *testing.T) {
	root := seedCorpus(t)
	index := filepath.Join(root, defaultIndexRelPath)
	g, _, _, err := coldRebuild(context.Background(), index, touchingEmbedder{filepath.Join(root, "memory", "2026-07-03.md")}, root)
	if err != nil {
		t.Fatal(err)
	}
	g.Close()
	if fresh, err := indexIsFresh(index, root); err != nil || fresh {
		t.Fatalf("edit during build lost: fresh=%v err=%v", fresh, err)
	}
}
