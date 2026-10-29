#!/usr/bin/env bash
# ==============================================================================
# SEO-BOOST: POST-DEPLOY SUBMIT HOOK (Linux / macOS / Bash)
# ==============================================================================
# Pemicu pengiriman sitemap ke GSC dan ping IndexNow segera setelah deploy/publish.

set -e

SITE="${1:-all}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "================================================================================"
echo "  SEO-BOOST: POST-DEPLOY SUBMIT HOOK"
echo "================================================================================"
echo "Target Site : $SITE"
echo ""

if [ -f "$PROJECT_ROOT/seo-boost" ]; then
    "$PROJECT_ROOT/seo-boost" submit --config "$PROJECT_ROOT/config.json" --site "$SITE" --gsc --indexnow
else
    echo "[INFO] Menjalankan via 'go run' karena biner belum dikompilasi..."
    cd "$PROJECT_ROOT" && go run ./cmd/seo-boost submit --config "$PROJECT_ROOT/config.json" --site "$SITE" --gsc --indexnow
fi
