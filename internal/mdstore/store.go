package mdstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
// creating the file if needed. It guarantees a blank-line separation between
// any pre-existing content and the new blocks.
func appendBlocks(path string, blocks []string) error {
	if len(blocks) == 0 {
		return nil
	}
	var pre string
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		pre = "\n\n"
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("mdstore: stat %s: %w", path, err)
	}
	payload := pre + strings.Join(blocks, "\n\n") + "\n"

	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("mdstore: open %s: %w", path, err)
	}
	defer fh.Close()
	if _, err := fh.WriteString(payload); err != nil {
		return fmt.Errorf("mdstore: write %s: %w", path, err)
	}
	return nil
}

// Load reads every daily file under memory/ and parses all structured blocks.
// Files are read in sorted (chronological, given the YYYY-MM-DD naming) order
// so the returned slices have a stable, time-ordered shape. Parse errors are
// loud: the first malformed block aborts Load with a located error.
func (s *FSStore) Load(ctx context.Context) ([]core.Entry, []core.Edge, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	dir := s.memoryDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil // empty corpus is valid
		}
		return nil, nil, fmt.Errorf("mdstore: readdir %s: %w", dir, err)
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
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, nil, fmt.Errorf("mdstore: read %s: %w", path, rerr)
		}
		es, eds, perr := Parse(string(data))
		if perr != nil {
			return nil, nil, fmt.Errorf("mdstore: in %s: %w", path, perr)
		}
		entries = append(entries, es...)
		edges = append(edges, eds...)
	}
	return entries, edges, nil
}

// compile-time assertion that FSStore satisfies the port.
var _ core.Store = (*FSStore)(nil)
