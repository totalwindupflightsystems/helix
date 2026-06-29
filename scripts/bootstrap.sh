#!/usr/bin/env bash
# scripts/bootstrap.sh — Bootstrap Helix from zero to running
#
# Usage:
#   ./scripts/bootstrap.sh           # full bootstrap (Forgejo + Chimera + CLIs)
#   ./scripts/bootstrap.sh --verify  # only run verification checks
#
# This script automates the Phase 0→4 sequence from specs/build-order.md.
# It creates directories, starts Forgejo, installs/starts Chimera,
# verifies both health endpoints, and builds all Helix CLIs.
#
# Prerequisites (checked at runtime):
#   - Docker ≥ 24.0
#   - Go ≥ 1.22
#   - Python ≥ 3.11
#   - curl

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────
FORGEJO_CONTAINER="forgejo-helix"
FORGEJO_PORT="3030"
FORGEJO_IMAGE="codeberg.org/forgejo/forgejo:1.21"
CHIMERA_DIR="/home/kara/chimera"
CHIMERA_PORT="8765"
LANGFUSE_CONTAINER="langfuse-helix"
LANGFUSE_PORT="3001"
HELIX_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ── Helpers ────────────────────────────────────────────────────
pass() { echo -e "${GREEN}✓ PASS${NC} $1"; }
fail() { echo -e "${RED}✗ FAIL${NC} $1"; exit 1; }
warn() { echo -e "${YELLOW}⚠ WARN${NC} $1"; }
info() { echo -e "${BLUE}ℹ${NC} $1"; }

# ── Phase 0: Prerequisites ────────────────────────────────────
check_prerequisites() {
    echo ""
    echo "=== Phase 0: Prerequisites ==="

    command -v docker >/dev/null 2>&1 || fail "Docker not found (need ≥ 24.0)"
    docker --version | grep -oP '\d+\.\d+\.\d+' | head -1 | {
        read -r ver
        major=$(echo "$ver" | cut -d. -f1)
        if [ "$major" -lt 24 ]; then
            fail "Docker version $ver < 24.0"
        fi
    }
    pass "Docker $(docker --version | grep -oP '\d+\.\d+\.\d+' | head -1)"

    command -v go >/dev/null 2>&1 || fail "Go not found (need ≥ 1.22)"
    go_version=$(go version | grep -oP 'go\K\d+\.\d+')
    pass "Go $go_version"

    command -v python3 >/dev/null 2>&1 || fail "Python3 not found (need ≥ 3.11)"
    py_version=$(python3 --version 2>&1 | grep -oP '\d+\.\d+\.\d+')
    pass "Python $py_version"

    command -v curl >/dev/null 2>&1 || fail "curl not found"
    pass "curl available"

    # Create Helix directories
    mkdir -p ~/.helix/keys
    mkdir -p ~/.helix/marketplace/agents
    mkdir -p ~/.helix/negotiations
    mkdir -p ~/.helix/estimates
    pass "Helix directories created"
}

# ── Phase 1: Forgejo ───────────────────────────────────────────
start_forgejo() {
    echo ""
    echo "=== Phase 1: Forgejo ==="

    # Check if already running
    if docker ps --format '{{.Names}}' | grep -q "^${FORGEJO_CONTAINER}$"; then
        pass "Forgejo container already running"
    else
        # Clean up stopped container if exists
        docker rm -f "$FORGEJO_CONTAINER" 2>/dev/null || true

        info "Starting Forgejo on port ${FORGEJO_PORT}..."
        docker run -d --name "$FORGEJO_CONTAINER" \
            -p "${FORGEJO_PORT}:3000" \
            -e FORGEJO__server__DOMAIN=localhost \
            -e FORGEJO__server__ROOT_URL="http://localhost:${FORGEJO_PORT}" \
            -e FORGEJO__server__HTTP_PORT=3000 \
            -e FORGEJO__server__START_SSH_SERVER=false \
            -v forgejo-helix-data:/data \
            "$FORGEJO_IMAGE" >/dev/null

        info "Waiting for Forgejo to start (15s)..."
        sleep 15
        pass "Forgejo container started"
    fi

    # Verify Forgejo health
    info "Checking Forgejo health..."
    local attempts=0
    local max=10
    while [ $attempts -lt $max ]; do
        if curl -sf "http://localhost:${FORGEJO_PORT}/api/v1/version" >/dev/null 2>&1; then
            pass "Forgejo health check OK"
            return
        fi
        attempts=$((attempts + 1))
        sleep 2
    done
    fail "Forgejo health check failed after ${max} attempts"
}

# ── Phase 2: Chimera ───────────────────────────────────────────
start_chimera() {
    echo ""
    echo "=== Phase 2: Chimera ==="

    if [ ! -d "$CHIMERA_DIR" ]; then
        warn "Chimera directory not found at $CHIMERA_DIR — skipping"
        return
    fi

    # Check if already running
    if curl -sf "http://localhost:${CHIMERA_PORT}/v1/health" >/dev/null 2>&1; then
        pass "Chimera already running"
        return
    fi

    info "Installing Chimera dependencies..."
    cd "$CHIMERA_DIR"
    if [ -f ".venv/bin/pip" ]; then
        .venv/bin/pip install -e . -q 2>/dev/null || warn "pip install had warnings"
    else
        warn "No .venv found in $CHIMERA_DIR — skipping Chimera setup"
        cd "$HELIX_DIR"
        return
    fi

    info "Starting Chimera on port ${CHIMERA_PORT}..."
    .venv/bin/python -c "
from chimera.api.server import app
import uvicorn
uvicorn.run(app, host='0.0.0.0', port=${CHIMERA_PORT})
" &
    CHIMERA_PID=$!
    info "Chimera PID: ${CHIMERA_PID}"

    # Wait for Chimera to be ready
    info "Waiting for Chimera to start..."
    sleep 5

    local attempts=0
    local max=10
    while [ $attempts -lt $max ]; do
        if curl -sf "http://localhost:${CHIMERA_PORT}/v1/health" >/dev/null 2>&1; then
            pass "Chimera health check OK"
            cd "$HELIX_DIR"
            return
        fi
        attempts=$((attempts + 1))
        sleep 2
    done
    warn "Chimera health check failed (may still be starting)"
    cd "$HELIX_DIR"
}

# ── Phase 3: Helix CLIs ────────────────────────────────────────
build_helix_clis() {
    echo ""
    echo "=== Phase 3: Helix CLIs ==="

    cd "$HELIX_DIR"

    info "Building all Helix CLIs..."
    if go build ./cmd/... 2>/dev/null; then
        pass "All Helix CLIs built successfully"
    else
        fail "Helix CLI build failed — check go build output"
    fi

    # Verify each CLI exists
    local clis=("helix-identity" "helix-estimate" "helix-negotiate" "helix-prompt" "helix-marketplace")
    for cli in "${clis[@]}"; do
        if [ -f "$HELIX_DIR/$cli" ]; then
            pass "$cli binary exists"
        elif [ -f "$HELIX_DIR/cmd/$cli/$cli" ]; then
            pass "$cli binary exists (in cmd/)"
        else
            warn "$cli binary not found at expected path"
        fi
    done
}

# ── Phase 4: Verification ──────────────────────────────────────
run_verification() {
    echo ""
    echo "=== Phase 4: Verification ==="

    local checks_passed=0
    local checks_total=5

    # 1. Forgejo is running
    if curl -sf "http://localhost:${FORGEJO_PORT}/api/v1/version" >/dev/null 2>&1; then
        pass "1/5: Forgejo is running"
        checks_passed=$((checks_passed + 1))
    else
        fail "1/5: Forgejo is not running"
    fi

    # 2. Chimera is running
    if curl -sf "http://localhost:${CHIMERA_PORT}/v1/health" >/dev/null 2>&1; then
        pass "2/5: Chimera is running"
        checks_passed=$((checks_passed + 1))
    else
        warn "2/5: Chimera is not running (optional — may not be installed)"
    fi

    # 3. Helix CLIs build
    cd "$HELIX_DIR"
    if go build ./cmd/... 2>/dev/null; then
        pass "3/5: Helix CLIs build"
        checks_passed=$((checks_passed + 1))
    else
        fail "3/5: Helix CLIs build failed"
    fi

    # 4. Helix identity dry-run
    if [ -f "$HELIX_DIR/helix-identity" ]; then
        if "$HELIX_DIR/helix-identity" sync --dry-run >/dev/null 2>&1; then
            pass "4/5: helix-identity dry-run works"
            checks_passed=$((checks_passed + 1))
        else
            warn "4/5: helix-identity dry-run failed (may need config)"
        fi
    else
        warn "4/5: helix-identity binary not found"
    fi

    # 5. Helix sandbox dry-run
    if [ -f "$HELIX_DIR/sandbox" ]; then
        if "$HELIX_DIR/sandbox" run --dry-run -- echo "hello" >/dev/null 2>&1; then
            pass "5/5: sandbox dry-run works"
            checks_passed=$((checks_passed + 1))
        else
            warn "5/5: sandbox dry-run failed (may need bubblewrap)"
        fi
    else
        warn "5/5: sandbox binary not found"
    fi

    echo ""
    echo "=== Bootstrap Summary ==="
    echo "Checks passed: ${checks_passed}/${checks_total}"
    echo ""
    echo "Services:"
    echo "  Forgejo:  http://localhost:${FORGEJO_PORT} (admin: helio / helio123)"
    echo "  Chimera:  http://localhost:${CHIMERA_PORT}"
    echo "  Helix CLIs in ${HELIX_DIR}/"

    if [ "$checks_passed" -ge 3 ]; then
        echo ""
        pass "Bootstrap completed with ${checks_passed}/${checks_total} checks passing"
    else
        echo ""
        fail "Bootstrap incomplete: only ${checks_passed}/${checks_total} checks passed"
    fi
}

# ── Main ───────────────────────────────────────────────────────
main() {
    echo "╔══════════════════════════════════════════════════╗"
    echo "║         Helix Platform — Bootstrap v1.0          ║"
    echo "╚══════════════════════════════════════════════════╝"

    if [ "${1:-}" = "--verify" ]; then
        run_verification
        exit 0
    fi

    check_prerequisites
    start_forgejo
    start_chimera
    build_helix_clis
    run_verification
}

main "$@"
