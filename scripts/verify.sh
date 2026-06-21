#!/usr/bin/env bash
# Helix platform verification script
# Run: ./scripts/verify.sh
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; exit 1; }
warn() { echo -e "${YELLOW}WARN${NC} $1"; }

echo "=== Helix Platform Verification ==="
echo ""

# 1. Go build
echo -n "Building all CLIs... "
if go build ./cmd/... 2>/dev/null; then
    pass "All CLIs build"
else
    fail "Build failed"
fi

# 2. Go vet
echo -n "Running go vet... "
if go vet ./... 2>/dev/null; then
    pass "go vet clean"
else
    fail "go vet found issues"
fi

# 3. Tests
echo -n "Running tests... "
if go test -short -count=1 ./... 2>/dev/null; then
    pass "All tests pass"
else
    fail "Tests failed"
fi

# 4. Forgejo
echo -n "Checking Forgejo... "
FORGEJO_URL="${FORGEJO_URL:-http://localhost:3000}"
if curl -sf "${FORGEJO_URL}/api/v1/version" >/dev/null 2>&1; then
    pass "Forgejo reachable"
else
    warn "Forgejo not reachable at ${FORGEJO_URL}"
fi

# 5. Chimera
echo -n "Checking Chimera... "
CHIMERA_URL="${CHIMERA_URL:-http://localhost:8765}"
if curl -sf "${CHIMERA_URL}/v1/health" >/dev/null 2>&1; then
    pass "Chimera reachable"
else
    warn "Chimera not reachable at ${CHIMERA_URL}"
fi

# 6. Binary check
echo ""
echo "=== Binary Availability ==="
for bin in helix-identity helix-estimate helix-negotiate helix-prompt helix-marketplace; do
    echo -n "  ${bin}: "
    if command -v "${bin}" >/dev/null 2>&1 || [ -x "${bin}" ]; then
        pass "available"
    else
        warn "not found (run 'make build' or 'go build ./cmd/${bin}/')"
    fi
done

echo ""
echo "=== Verification Complete ==="
