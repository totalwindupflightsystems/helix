#!/usr/bin/env bash
#
# check-trust-tier.sh — GitReins pre-commit hook for trust tier enforcement
#
# Blocks merges from agents whose trust tier is below the minimum required
# for the changed file categories (spec specs/trust-model.md §Integration Points).
#
# Usage:
#   ./scripts/check-trust-tier.sh --agent-id <uuid> [--tier <tier>] [--files <paths...>]
#
# If --tier is not provided, the script simulates querying the Helix marketplace
# and defaults to "provisional" (safest default for autonomous agents).
#
# File category → minimum trust tier mapping:
#   IaC (infra-as-code)        → Observed (Tier 1+)
#   CI/CD configs              → Veteran (Tier 3+)
#   Auth/security policies     → Trusted (Tier 2+)
#   API/data schemas           → Observed (Tier 1+)
#   Documentation              → Provisional (no restriction)
#   Tests/test fixtures        → Provisional (no restriction)
#   General source code        → Provisional (no restriction)

set -euo pipefail

# ---- Configuration ----

# Tier ordering (higher index = higher privilege)
TIERS=(provisional observed trusted veteran)

# File category → minimum tier index mapping (0-based index into TIERS)
declare -A CATEGORY_MIN_TIER
CATEGORY_MIN_TIER["iac"]=1          # observed
CATEGORY_MIN_TIER["cicd"]=3         # veteran
CATEGORY_MIN_TIER["auth"]=2         # trusted
CATEGORY_MIN_TIER["schema"]=1       # observed
CATEGORY_MIN_TIER["docs"]=0         # provisional
CATEGORY_MIN_TIER["tests"]=0        # provisional
CATEGORY_MIN_TIER["code"]=0         # provisional

# File extension → category mapping
classify_file() {
    local path="$1"
    case "$path" in
        *.tf|*.tfvars|terraform/*|*.tfstate*)
            echo "iac"
            ;;
        *.yml|*.yaml)
            # Check if it's a CI/CD config
            if echo "$path" | grep -qiE '(ci|cd|pipeline|deploy|action|workflow)'; then
                echo "cicd"
            else
                echo "code"
            fi
            ;;
        Dockerfile|*.Dockerfile|docker-compose*|dockerfile*)
            echo "cicd"
            ;;
        *auth*|*security*|*secret*|*credential*|*session*|*token*|*oauth*)
            echo "auth"
            ;;
        *.go|*.rs|*.py|*.ts|*.js|*.java|*.rb|*.swift)
            echo "code"
            ;;
        *.md|*.rst|*.txt|docs/*)
            echo "docs"
            ;;
        *_test.go|*_test.py|*_test.rs|*_test.ts|*_test.js|*spec*|test*)
            echo "tests"
            ;;
        *.sql|*.proto|openapi*|swagger*)
            echo "schema"
            ;;
        *)
            echo "code"
            ;;
    esac
}

# ---- Parse arguments ----

AGENT_ID=""
AGENT_TIER="provisional"  # safest default
FILES=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --agent-id)
            AGENT_ID="$2"
            shift 2
            ;;
        --tier)
            AGENT_TIER="$2"
            shift 2
            ;;
        --files)
            shift
            while [[ $# -gt 0 && ! "$1" =~ ^-- ]]; do
                FILES+=("$1")
                shift
            done
            ;;
        *)
            echo "Usage: $0 --agent-id <uuid> [--tier <tier>] [--files <paths...>]"
            exit 1
            ;;
    esac
done

if [[ -z "$AGENT_ID" ]]; then
    echo "ERROR: --agent-id is required"
    exit 1
fi

# ---- Resolve agent tier index ----

AGENT_TIER_LOWER=$(echo "$AGENT_TIER" | tr '[:upper:]' '[:lower:]')
AGENT_TIER_IDX=-1
for i in "${!TIERS[@]}"; do
    if [[ "${TIERS[$i]}" == "$AGENT_TIER_LOWER" ]]; then
        AGENT_TIER_IDX=$i
        break
    fi
done

if [[ $AGENT_TIER_IDX -eq -1 ]]; then
    echo "ERROR: unknown tier '$AGENT_TIER' (valid: ${TIERS[*]})"
    exit 1
fi

# ---- Check files against categories ----

BLOCKED=false
declare -A SEEN_CATEGORIES

if [[ ${#FILES[@]} -eq 0 ]]; then
    # No files to check — pass (cover all case)
    echo "✓ Trust tier: no changed files to check — PASS"
    exit 0
fi

for file in "${FILES[@]}"; do
    cat=$(classify_file "$file")
    SEEN_CATEGORIES["$cat"]=1
    
    min_idx=${CATEGORY_MIN_TIER[$cat]:-0}
    
    if [[ $AGENT_TIER_IDX -lt $min_idx ]]; then
        echo "✗ BLOCKED: $file ($cat requires ${TIERS[$min_idx]}+, agent is $AGENT_TIER_LOWER)"
        BLOCKED=true
    fi
done

# Report summary
for cat in "${!SEEN_CATEGORIES[@]}"; do
    min_idx=${CATEGORY_MIN_TIER[$cat]:-0}
    if [[ $AGENT_TIER_IDX -ge $min_idx ]]; then
        status="OK"
    else
        status="BLOCKED"
    fi
    echo "  File category '$cat': requires ${TIERS[$min_idx]}+, agent is $AGENT_TIER_LOWER — $status"
done

if $BLOCKED; then
    echo ""
    echo "COMMIT BLOCKED: agent trust tier ($AGENT_TIER_LOWER) insufficient for changed files"
    echo "See specs/trust-model.md §Integration Points for tier requirements"
    exit 1
fi

echo "✓ Trust tier: PASS"
