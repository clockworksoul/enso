// enso-append is the single-entry ingestion CLI for Ensō Phase 1.
//
// It appends one structured memory entry (and optionally a supersession triple)
// to the appropriate daily file under the memory store. This is the minimal
// surface that makes real capture possible without a GUI or editor macro.
//
// # Normal append
//
//	enso-append \
//	  -root ~/.openclaw/workspace \
//	  -type Fact \
//	  -content "Granola REST API works without the desktop app" \
//	  -tags "granola,tooling" \
//	  -confidence high
//
// # Supersession (closes an existing entry and creates a new one)
//
//	enso-append \
//	  -root ~/.openclaw/workspace \
//	  -supersede mem:2026-06-25-granola-keep-using \
//	  -type Fact \
//	  -content "Granola is being uninstalled from all Yext devices; REST API still works" \
//	  -tags "granola,yext,tooling" \
//	  -confidence high
//
// The entry ID is auto-generated from today's date and a slug of the content.
// EncodedTime is set to UTC now. All reserved Phase-3 fields are written with
// sane defaults (marked TUNABLE; inert until Phase 3).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

func main() {
	root := flag.String("root", "", "root directory of the memory store (contains memory/)")
	entryType := flag.String("type", "", "node type: Fact|Decision|Insight|Person|Project|Task")
	content := flag.String("content", "", "one-line human-readable payload (required)")
	slug := flag.String("slug", "", "override the auto-generated ID slug (kebab-case, no mem: prefix)")
	tags := flag.String("tags", "", "comma-separated tags (optional)")
	about := flag.String("about", "", "comma-separated entity refs, e.g. project:omega (optional)")
	confidence := flag.String("confidence", "high", "confidence: high|medium|low")
	eventTime := flag.String("event-time", "", "ISO-8601 event time (when it became true; optional)")
	supersede := flag.String("supersede", "", "ID of the entry this new one supersedes (triggers supersession ceremony)")
	dryRun := flag.Bool("dry-run", false, "print the formatted block(s) without writing")
	flag.Parse()

	if *root == "" {
		log.Fatal("enso-append: -root is required")
	}
	if *content == "" {
		log.Fatal("enso-append: -content is required")
	}
	if *entryType == "" {
		log.Fatal("enso-append: -type is required")
	}

	now := time.Now().UTC()

	slugSrc := *content
	if *slug != "" {
		slugSrc = *slug
	}
	id, err := core.NewID(now, slugSrc)
	if err != nil {
		log.Fatalf("enso-append: generate id: %v", err)
	}

	params := core.NewEntryParams{
		ID:          id,
		Type:        core.NodeType(*entryType),
		Content:     *content,
		EncodedTime: now,
		Confidence:  core.Confidence(*confidence),
		Tags:        splitList(*tags),
		About:       splitList(*about),
	}
	if *eventTime != "" {
		t, err := time.Parse(time.RFC3339, *eventTime)
		if err != nil {
			log.Fatalf("enso-append: -event-time: %v", err)
		}
		params.EventTime = &t
	}

	entry, err := core.NewEntry(params)
	if err != nil {
		log.Fatalf("enso-append: build entry: %v", err)
	}

	if *dryRun {
		fmt.Println(mdstore.MarshalEntry(entry))
		if *supersede != "" {
			edge := core.Edge{
				From:  entry.ID,
				Type:  core.EdgeSupersedes,
				To:    *supersede,
				Extra: map[string]string{},
			}
			fmt.Println()
			fmt.Println(mdstore.MarshalEdge(edge))
			fmt.Println()
			fmt.Printf("(closed copy of %s would also be appended with valid_until=%s)\n", *supersede, now.Format(time.RFC3339))
		}
		return
	}

	store := mdstore.NewFSStore(*root)
	ctx := context.Background()

	if *supersede != "" {
		// Load the store to find the old entry.
		entries, _, err := store.Load(ctx)
		if err != nil {
			log.Fatalf("enso-append: load store: %v", err)
		}
		oldID := core.ID(*supersede)
		var old *core.Entry
		for i := range entries {
			if entries[i].ID == oldID {
				old = &entries[i]
				break
			}
		}
		if old == nil {
			log.Fatalf("enso-append: entry %q not found in store (supersede requires the entry to exist)", *supersede)
		}
		if err := store.Supersede(ctx, *old, entry); err != nil {
			log.Fatalf("enso-append: supersede: %v", err)
		}
		fmt.Fprintf(os.Stderr, "superseded %s → %s\n", oldID, entry.ID)
	} else {
		if err := store.Append(ctx, []core.Entry{entry}, nil); err != nil {
			log.Fatalf("enso-append: append: %v", err)
		}
		fmt.Fprintf(os.Stderr, "appended %s\n", entry.ID)
	}
}

// splitList splits a comma-separated string into a trimmed slice. Returns nil
// (not empty slice) when the input is blank so callers can distinguish
// "not provided" from "explicitly empty". NewEntry normalises nil → [].
func splitList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
