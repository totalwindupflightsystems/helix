#!/usr/bin/env bash
#
# helix-pre-receive.sh — Forgejo/Git pre-receive hook for merge gate enforcement
#
# This script is the git-level enforcement point for Helix merge gates.
# It reads pushed refs from stdin (standard pre-receive protocol) and pipes
# them to `helix mergegate hook` for evaluation.
#
# Installation:
#   cp scripts/helix-pre-receive.sh /path/to/forgejo/data/gitea/repositories/<owner>/<repo>.git/hooks/pre-receive.d/helix
#   chmod +x /path/to/.../pre-receive.d/helix
#
# Or install as the main pre-receive hook:
#   cp scripts/helix-pre-receive.sh /path/to/repo.git/hooks/pre-receive
#   chmod +x /path/to/repo.git/hooks/pre-receive
#
# Configuration via environment variables (set in Forgejo repo settings):
#   HELIX_TRUST_TIER       Agent trust tier (provisional|observed|trusted|veteran)
#   HELIX_EVIDENCE_PATH    Path to evidence directory (relative to repo root)
#   HELIX_PROTECTED        Comma-separated protected branch patterns (default: main,master,release/*)
#   HELIX_SKIP_GATE        Set to "1" to bypass all gate checks (emergency only)
#
# Exit codes:
#   0  Push ALLOWED — all gate checks passed
#   1  Push BLOCKED — one or more gate checks failed
#
# Spec: specs/plans/phase-7-8-negotiate-merge.md §Gap 2-4, §8.1-8.3

set -euo pipefail

# --- Configuration ---

TRUST_TIER="${HELIX_TRUST_TIER:-}"
EVIDENCE_PATH="${HELIX_EVIDENCE_PATH:-}"
PROTECTED="${HELIX_PROTECTED:-main,master,release/*}"
SKIP_GATE="${HELIX_SKIP_GATE:-0}"

# --- Locate helix binary ---

# Try HELIX_BIN, then PATH, then common locations.
HELIX_BIN="${HELIX_BIN:-}"
if [ -z "$HELIX_BIN" ]; then
    HELIX_BIN="$(command -v helix 2>/dev/null || true)"
fi
if [ -z "$HELIX_BIN" ]; then
    HELIX_BIN="/usr/local/bin/helix"
fi
if [ ! -x "$HELIX_BIN" ]; then
    # Helix not installed — allow push with warning.
    echo "helix-pre-receive: WARNING — helix binary not found at $HELIX_BIN, skipping gate enforcement" >&2
    exit 0
fi

# --- Bypass check ---

if [ "$SKIP_GATE" = "1" ]; then
    echo "helix-pre-receive: HELIX_SKIP_GATE=1, bypassing all gate checks" >&2
    exit 0
fi

# --- Build CLI args ---

ARGS=("mergegate" "hook")
[ -n "$TRUST_TIER" ] && ARGS+=("--trust" "$TRUST_TIER")
[ -n "$EVIDENCE_PATH" ] && ARGS+=("--evidence-path" "$EVIDENCE_PATH")
[ -n "$PROTECTED" ] && ARGS+=("--protected" "$PROTECTED")

# --- Run the hook ---

# stdin contains the pre-receive refs (one per line: old-sha new-sha ref-name)
# Pass through to helix mergegate hook.
exec "$HELIX_BIN" "${ARGS[@]}"
