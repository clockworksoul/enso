// enso-load-check verifies that the live corpus round-trips cleanly.
//
// Exit codes (2026-07-27, post-incident: this is now the corpus-health
// primitive the enso-audit.sh cron and any other health check should call,
// rather than a grep-based presence check that can't detect a parse
// failure):
//
//	0  clean load, zero per-file failures
//	1  degraded  load succeeded (per-file isolation, WP-mdstore-isolation)
//	   but one or more daily files failed to parse; every failure is
//	   printed with its path and reason
//	2  fatal     the corpus directory itself could not be read (not a
//	   per-file problem)
//
// -root overrides the default corpus root for testing against a different
// tree; the default matches the live workspace corpus this tool exists to
// watch.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/clockworksoul/enso/internal/mdstore"
)

func main() {
	root := flag.String("root", "/Users/matt/.openclaw/workspace", "corpus root (directory containing memory/)")
	quiet := flag.Bool("q", false, "suppress the per-entry/per-edge listing; print only the summary and any failures")
	flag.Parse()

	s := mdstore.NewFSStore(*root)
	entries, edges, failures, err := s.LoadWithErrors(context.Background())
	if err != nil && len(failures) == 0 {
		// A non-nil error with zero per-file failures means the directory
		// itself was unreadable (or context cancellation) -- not a
		// per-file parse problem. Fatal, distinct exit code.
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("loaded %d entries, %d edges, %d file failure(s)\n", len(entries), len(edges), len(failures))
	for _, f := range failures {
		fmt.Printf("  FAILED %v\n", f)
	}

	if !*quiet {
		for _, e := range edges {
			fmt.Printf("  EDGE %s -[%s]-> %s\n", e.From, e.Type, e.To)
		}
		for _, e := range entries {
			valid := "open"
			if e.ValidUntil != nil {
				valid = "closed " + e.ValidUntil.Format("2006-01-02")
			}
			fmt.Printf("  ENTRY %-55s [%s]\n", e.ID, valid)
		}
	}

	if len(failures) > 0 {
		os.Exit(1)
	}
}
