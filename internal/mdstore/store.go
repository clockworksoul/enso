package mdstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/clockworksoul/enso/internal/core"
)

// FSStore is the filesystem-backed Markdown adapter implementing core.Store.
// It persists structured entries/edges INLINE in daily files
// `memory/YYYY-MM-DD.md` (the ratified §3.5(a) layout): structured blocks are
// appended to the daily file, interleaved with whatever prose already lives
// there.
//
// Append-only (INV-2): Append never rewrites existing file content; it appends
// new blocks to the end of the relevant daily file. Supersession is modeled as
// additional appended blocks (a new entry, a SUPERSEDES edge, and a re-appended
// closed copy of the old entry), never an in-place edit.
type FSStore struct {
	root string // directory containing the memory/ subtree
}

// memorySubdir is the conventional location of daily files under root.
const memorySubdir = "memory"

// NewFSStore returns a store rooted at dir. The memory/ subdirectory is created
// lazily on first Append.
func NewFSStore(dir string) *FSStore {
	return &FSStore{root: dir}
}

func (s *FSStore) memoryDir() string { return filepath.Join(s.root, memorySubdir) }

// dailyFile returns the path of the daily file an entry belongs in, derived
// from its ID's date (the encoded date is the file bucket).
func (s *FSStore) dailyFileForID(id core.ID) (string, error) {
	d, err := id.Date()
	if err != nil {
		return "", err
	}
	return filepath.Join(s.memoryDir(), d.Format("2006-01-02")+".md"), nil
}

// Append writes entries and edges to their daily files. Entries bucket by their
// ID date; edges bucket by the date encoded in their From id (so a supersession
// edge lands alongside the new entry that owns it). The write is additive.
func (s *FSStore) Append(ctx context.Context, entries []core.Entry, edges []core.Edge) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Validate everything up front (loud, before any write) so a bad batch
	// doesn't partially land.
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("mdstore: refusing to append invalid entry %q: %w", e.ID, err)
		}
	}
	for _, ed := range edges {
		if err := ed.Validate(); err != nil {
			return fmt.Errorf("mdstore: refusing to append invalid edge from %q: %w", ed.From, err)
		}
	}

	// Group blocks by destination file, preserving input order within a file.
	byFile := map[string][]string{}
	order := []string{}
	add := func(file, block string) {
		if _, seen := byFile[file]; !seen {
			order = append(order, file)
		}
		byFile[file] = append(byFile[file], strings.TrimRight(block, "\n"))
	}
	for _, e := range entries {
		f, err := s.dailyFileForID(e.ID)
		if err != nil {
			return fmt.Errorf("mdstore: entry %q: %w", e.ID, err)
		}
		add(f, MarshalEntry(e))
	}
	for _, ed := range edges {
		f, err := s.dailyFileForID(ed.From)
		if err != nil {
			return fmt.Errorf("mdstore: edge from %q: %w", ed.From, err)
		}
		add(f, MarshalEdge(ed))
	}

	if err := os.MkdirAll(s.memoryDir(), 0o755); err != nil {
		return fmt.Errorf("mdstore: mkdir: %w", err)
	}
	for _, f := range order {
		if err := appendBlocks(f, byFile[f]); err != nil {
			return err
		}
	}
	return nil
}

// appendBlocks appends the given blocks to a file, separated by blank lines,
// creating the file if needed. It holds an exclusive advisory lock (flock) for
// the duration of the write so concurrent appenders do not interleave blocks.
func appendBlocks(path string, blocks []string) error {
	if len(blocks) == 0 {
		return nil
	}
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("mdstore: open %s: %w", path, err)
	}
	defer fh.Close()

	// Exclusive advisory lock: blocks other enso-append processes on the same
	// file. Released automatically when fh is closed.
	if err := syscall.Flock(int(fh.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("mdstore: lock %s: %w", path, err)
	}

	// Stat after locking to get accurate size (another writer may have just
	// appended while we were waiting for the lock).
	info, err := fh.Stat()
	if err != nil {
		return fmt.Errorf("mdstore: stat %s: %w", path, err)
	}
	pre := ""
	if info.Size() > 0 {
		pre = "\n\n"
	}
	payload := pre + strings.Join(blocks, "\n\n") + "\n"
	if _, err := fh.WriteString(payload); err != nil {
		return fmt.Errorf("mdstore: write %s: %w", path, err)
	}
	return nil
}

// Supersede performs the supersession-append ceremony (tech spec §3.3):
//
//  1. Stamps ValidUntil=now on a closed copy of old.
//  2. Appends: new entry + closed old entry + SUPERSEDES edge.
//
// The on-disk order (entries then edge, within the daily bucket) is an
// implementation detail; the parser is order-independent. The important
// invariant (INV-2) is that the old entry is never edited — a closed copy is
// appended so the full history is always recoverable.
//
// Both old and new must already be validated. new.EncodedTime determines which
// daily file receives all three blocks (they co-locate so the ceremony reads
// naturally in one file).
func (s *FSStore) Supersede(ctx context.Context, old, new core.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	closed := old
	closed.ValidUntil = &now

	edge := core.Edge{
		From:  new.ID,
		Type:  core.EdgeSupersedes,
		To:    string(old.ID),
		Extra: map[string]string{},
	}
	return s.Append(ctx, []core.Entry{new, closed}, []core.Edge{edge})
}

// Load reads every daily file under memory/ and parses all structured blocks.
// Files are read in sorted (chronological, given the YYYY-MM-DD naming) order
// so the returned slices have a stable, time-ordered shape. Parse errors are
// loud: the first malformed block aborts Load with a located error.
// LoadFileError names one daily file that failed to parse during Load, with
// the underlying parse error. Load isolates failures per file (2026-07-27,
// post-incident: a single malformed block in one daily file previously
// failed the ENTIRE corpus load — every other file, however clean, went
// unavailable along with it). A LoadFileError is still loud (returned to the
// caller, never swallowed) but it is scoped to the one bad file so the rest
// of the corpus remains usable.
type LoadFileError struct {
	Path string
	Err  error
}

func (e *LoadFileError) Error() string {
	return fmt.Sprintf("mdstore: in %s: %v", e.Path, e.Err)
}

func (e *LoadFileError) Unwrap() error { return e.Err }

// Load implements core.Store. Per-file isolation (2026-07-27): a malformed
// daily file is skipped, not fatal to the rest of the corpus, so Load
// returns every entry/edge from every file that DID parse cleanly and a nil
// error as long as at least the directory itself was readable. This is a
// deliberate best-effort default because every existing caller of Load
// (enso-recall, enso-load-check, the graph rebuild, the core recall path)
// treats a non-nil error as fatal and would otherwise still lose the WHOLE
// corpus to one bad file, exactly the failure this fix exists to prevent.
// Per-file failures are never silently dropped, though — they still exist,
// loudly, via LoadWithErrors (which callers that specifically want to
// detect/alert on corpus health, e.g. enso-load-check and the nightly audit
// cron, call directly instead of Load).
func (s *FSStore) Load(ctx context.Context) ([]core.Entry, []core.Edge, error) {
	entries, edges, _, err := s.LoadWithErrors(ctx)
	if err != nil {
		// LoadWithErrors' single error return doubles as "first per-file
		// failure" once the directory itself was readable. A *LoadFileError
		// is exactly that case — isolated to one file, not fatal to Load—
		// and must NOT propagate here, or Load regains the pre-2026-07-27
		// all-or-nothing behavior this fix exists to remove. Anything else
		// (context cancellation, directory unreadable) is a real fatal error.
		var lfe *LoadFileError
		if !errors.As(err, &lfe) {
			return entries, edges, err
		}
	}
	return entries, edges, nil
}

// LoadWithErrors is Load's per-file-isolated form: a malformed daily file is
// skipped (not fatal to the whole corpus) and reported back as a
// LoadFileError, alongside everything that DID load successfully from every
// other file. The plain error return is non-nil (first failure, for callers
// that just want a go/no-go signal and don't care which file); the []error
// return carries every failure, for callers that want to report or alert on
// all of them.
func (s *FSStore) LoadWithErrors(ctx context.Context) ([]core.Entry, []core.Edge, []error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	dir := s.memoryDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, nil // empty corpus is valid
		}
		return nil, nil, nil, fmt.Errorf("mdstore: readdir %s: %w", dir, err)
	}
	names := make([]string, 0, len(ents))
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		names = append(names, de.Name())
	}
	sort.Strings(names)

	var entries []core.Entry
	var edges []core.Edge
	var failures []error
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			// A read failure (permissions, race with deletion) is treated the
			// same as a parse failure: isolate to this file, keep going.
			failures = append(failures, &LoadFileError{Path: path, Err: rerr})
			continue
		}
		es, eds, perr := Parse(string(data))
		if perr != nil {
			failures = append(failures, &LoadFileError{Path: path, Err: perr})
			continue
		}
		entries = append(entries, es...)
		edges = append(edges, eds...)
	}
	var firstErr error
	if len(failures) > 0 {
		firstErr = failures[0]
	}
	return entries, edges, failures, firstErr
}

// compile-time assertion that FSStore satisfies the port.
var _ core.Store = (*FSStore)(nil)
