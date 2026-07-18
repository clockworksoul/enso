package graphstore

import (
	"path/filepath"
	"testing"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/storetest"
)

// TestStoreConformance runs the shared core.Store contract suite (WP-3 DoD:
// "graphstore passes the same Store-contract test suite as mdstore/memstore").
// On-disk (not in-memory) so the real persistence path is what conforms.
func TestStoreConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) core.Store {
		g, err := Open(filepath.Join(t.TempDir(), "index.kuzu"))
		if err != nil {
			t.Fatalf("open graphstore: %v", err)
		}
		t.Cleanup(g.Close)
		return g
	})
}
