// enso-load-check verifies that the live corpus round-trips cleanly.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/clockworksoul/enso/internal/mdstore"
)

func main() {
	s := mdstore.NewFSStore("/Users/matt/.openclaw/workspace")
	entries, edges, err := s.Load(context.Background())
	if err != nil {
		log.Fatalf("load: %v", err)
	}
	fmt.Printf("loaded %d entries, %d edges\n", len(entries), len(edges))
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
