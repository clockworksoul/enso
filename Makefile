# Ensō — dev harness.
# Inside-out build: the core (internal/core) has no outward dependencies and is
# fully unit-testable in isolation. Adapters are added in later stages.

GO ?= go

.PHONY: all build test vet fmt fmt-check lint drift check tidy

all: check

build:
	$(GO) build ./...

test:
	$(GO) test ./...

# -race + -count=1 for the trustworthy run.
test-race:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

tidy:
	$(GO) mod tidy

# Verify the unified-spec snapshot is in sync with its source docs (see
# scripts/enso-spec-drift.sh and docs/2026-06-20-enso-unified-spec.md §10).
drift:
	bash scripts/enso-spec-drift.sh

# The full local gate: format, vet, build, test, and spec-drift.
check: fmt-check vet build test drift
