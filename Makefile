.PHONY: build test lint clean docker-build docker-up docker-up all install

# Go tmpfs redirect — /tmp on this host is a 30G tmpfs at 80%+ util.
# Trust ledger integration tests use t.TempDir() and hit quota. Persist via go env -w
# AND export TMPDIR for the linker (which doesn't honour GOTMPDIR).
export TMPDIR ?= /home/kara/.cache/go-tmp
GOTMPCACHE := /home/kara/.cache/go-build

# Default target
all: lint test build

# Build all CLI binaries into the repository root.
# Note: plain `go build ./cmd/...` (no -o) only compiles multi-package
# builds and DISCARDS the executables (go1.26); `-o .` is required so the
# binaries actually land in the repo root, where the unified `helix` CLI
# and `make install` expect them.
build:
	go build -o . ./cmd/...

# Install prefix for `make install` (override, e.g. `make install PREFIX=$(HOME)/.local`)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

# Install all CLI binaries into $(BINDIR). Requires sudo for the default
# /usr/local/bin; use `make install PREFIX=$(HOME)/.local` for a no-sudo,
# per-user install (then add $(BINDIR) to your PATH).
install: build
	install -d "$(BINDIR)"
	install -m 0755 helix helix-identity helix-estimate helix-marketplace helix-negotiate helix-prompt helix-release helix-verify sandbox "$(BINDIR)/"

# Run unit tests (short mode, no integration)
test:
	go test -short -count=1 ./...

# Run integration tests
test-integration:
	go test -count=1 ./pkg/integration/...

# Run linter
lint:
	golangci-lint run ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Build Docker image
docker-build:
	docker build -t helix .

# Bring up Docker Compose stack
docker-up:
	docker compose up -d

# Tear down Docker Compose stack
docker-down:
	docker compose down
