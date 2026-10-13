// enso-mark-recalled persists reinforcement for a materially used memory.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/mdstore"
)

func main() {
	root := flag.String("root", "", "root directory of the memory store (contains memory/)")
	id := flag.String("id", "", "ID of the materially used entry (required)")
	flag.Parse()

	if *root == "" {
		log.Fatal("enso-mark-recalled: -root is required")
	}
	if *id == "" {
		log.Fatal("enso-mark-recalled: -id is required")
	}

	store := mdstore.NewFSStore(*root)
	if _, err := core.MarkRecalled(context.Background(), store, core.ID(*id), time.Now()); err != nil {
		log.Fatalf("enso-mark-recalled: mark recalled: %v", err)
	}
	fmt.Fprintf(os.Stderr, "marked recalled %s\n", *id)
}
