#!/usr/bin/env bash
# scripts/up.sh — Start the full Helix platform stack
#
# Usage:
#   ./scripts/up.sh
#
# This script:
#   1. Starts all services via docker compose
#   2. Waits for health checks to pass
#   3. Prints dashboard URLs

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "=== Helix Platform — Starting Stack ==="
echo ""

# Ensure .env exists
if [ ! -f .env ]; then
    echo "[WARN] .env not found, copying .env.example"
    cp .env.example .env
fi

echo "[1/3] Starting services..."
docker compose up -d --build

echo "[2/3] Waiting for Forgejo to be healthy..."
for i in $(seq 1 60); do
    if curl -sf http://localhost:3000/api/v1/version >/dev/null 2>&1; then
        echo "  [OK] Forgejo is ready"
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "  [FAIL] Forgejo did not become ready in 60s"
        echo "  Check: docker compose logs forgejo"
        exit 1
    fi
    sleep 2
done

echo "[3/3] Waiting for Chimera to be healthy..."
for i in $(seq 1 60); do
    if curl -sf http://localhost:8765/v1/health >/dev/null 2>&1; then
        echo "  [OK] Chimera is ready"
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "  [WARN] Chimera did not become ready in 60s (non-fatal)"
        echo "  Check: docker compose logs chimera"
        break
    fi
    sleep 2
done

echo ""
echo "=== Helix Platform Running ==="
echo ""
echo "  Forgejo UI:    http://localhost:3000"
echo "  Chimera API:   http://localhost:8765"
echo "  Admin user:    ${FORGEJO_ADMIN_USER:-helio}"
echo ""
echo "  Run commands:  docker exec -it helix-cli helix status"
echo "  Stop:          ./scripts/down.sh"
echo ""
