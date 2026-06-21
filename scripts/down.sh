#!/usr/bin/env bash
# scripts/down.sh — Stop the full Helix platform stack
#
# Usage:
#   ./scripts/down.sh          # stop services, keep data
#   ./scripts/down.sh --clean  # stop services AND remove all data

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "=== Helix Platform — Stopping Stack ==="
echo ""

docker compose down

if [[ "${1:-}" == "--clean" ]]; then
    echo ""
    echo "[CLEAN] Removing volumes..."
    docker compose down --volumes --remove-orphans
    echo "[CLEAN] All data removed."
fi

echo ""
echo "=== Stack Stopped ==="
echo ""
echo "  Start again: ./scripts/up.sh"
echo ""
