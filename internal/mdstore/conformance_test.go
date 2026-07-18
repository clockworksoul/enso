package mdstore

import (
	"testing"

	"github.com/clockworksoul/enso/internal/core"
	"github.com/clockworksoul/enso/internal/storetest"
)

// TestStoreConformance runs the shared core.Store contract suite (WP-3 DoD:
// one suite, every adapter). The Markdown store is the CANONICAL adapter, so
// its conformance is what the graph adapter is measured against.
func TestStoreConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) core.Store {
		return NewFSStore(t.TempDir())
	})
}
