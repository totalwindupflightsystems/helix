.PHONY: build test lint clean docker-build docker-up docker-up all

# Go tmpfs redirect — /tmp on this host is a 30G tmpfs at 80%+ util.
# Trust ledger integration tests use t.TempDir() and hit quota. Persist via go env -w
# AND export TMPDIR for the linker (which doesn't honour GOTMPDIR).
export TMPDIR ?= /home/kara/.cache/go-tmp
GOTMPCACHE := /home/kara/.cache/go-build

# Default target
all: lint test build

# Build all CLI binaries
build:
	go build ./cmd/...

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
