// enso-lint is the write-time guard for hand-authored structured `mem:` blocks.
//
// The Phase-1 corpus is authoritative (INV-1: Markdown is canonical). Structured
// blocks are hand-written inline in memory/YYYY-MM-DD.md during consolidation
// passes, and the parser is strict by design — a block missing `type`/
// `encoded_time` or carrying a malformed ID fails loudly, because those keys are
// load-bearing for ranking. The problem is *when* that strictness fires: nothing
// on the write path calls mdstore.Load, so a malformed block sits committed and
// silent until a later benchmark run fails, and the whole daily file's entries
// silently drop from the corpus in the meantime.
//
// enso-lint moves that exact check earlier. It runs the SAME mdstore.Parse that
// Load runs, on specific files (or a whole memory dir), so a malformed block is
// caught seconds after authoring instead of days later by accident.
//
//	enso-lint [-q] <path>...            # lint specific files
//	enso-lint [-q] -dir <memory-dir>    # lint every *.md in a dir (sorted)
//
// Exit 0 = all clean, 1 = a file failed to parse, 2 = usage/IO error.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/clockworksoul/enso/internal/mdstore"
)

func main() {
	quiet := flag.Bool("q", false, "suppress per-file ok lines (failures + summary only)")
	dir := flag.String("dir", "", "lint every *.md file in this directory (non-recursive)")
	flag.Parse()

	paths, err := targets(*dir, flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "enso-lint: %v\n", err)
		os.Exit(2)
	}

	var totalEntries, totalEdges, failed int
	for _, p := range paths {
		e, d, lerr := lintFile(p)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, lerr)
			failed++
			continue
		}
		totalEntries += e
		totalEdges += d
		if !*quiet {
			fmt.Printf("ok  %s (%d entries, %d edges)\n", p, e, d)
		}
	}

	fmt.Printf("enso-lint: %d files, %d entries, %d edges, %d failed\n",
		len(paths), totalEntries, totalEdges, failed)

	if failed > 0 {
		os.Exit(1)
	}
}

// targets resolves the set of files to lint from either -dir or positional args.
// Supplying both, or neither, is a usage error.
func targets(dir string, args []string) ([]string, error) {
	switch {
	case dir != "" && len(args) > 0:
		return nil, fmt.Errorf("use either -dir or file paths, not both")
	case dir != "":
		ents, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read dir %s: %w", dir, err)
		}
		var out []string
		for _, de := range ents {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
				continue
			}
			out = append(out, filepath.Join(dir, de.Name()))
		}
		sort.Strings(out)
		return out, nil
	case len(args) > 0:
		return args, nil
	default:
		return nil, fmt.Errorf("no targets: give file paths or -dir <memory-dir>")
	}
}

// lintFile reads path and runs it through the canonical parser. A file with zero
// structured blocks is valid (prose-only daily files are normal). Returns the
// parsed entry/edge counts, or a parse/IO error.
func lintFile(path string) (entries, edges int, err error) {
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return 0, 0, rerr
	}
	es, eds, perr := mdstore.Parse(string(data))
	if perr != nil {
		return 0, 0, perr
	}
	return len(es), len(eds), nil
}
