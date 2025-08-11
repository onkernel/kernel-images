#!/usr/bin/env bash
set -euo pipefail

# build-operator-api.sh
# -------------------------
# Usage:
#   build-operator-api.sh [dest-dir]
#
# Defaults:
#   dest-dir  → ./bin   (mirror the Go server script behavior)
#
# Behavior:
#   - Runs Bun in Docker from /workspace/operator-api
#   - Produces artifacts in operator-api/dist:
#       dist/kernel-operator-api
#       dist/kernel-operator-test
#       dist/.kernel-operator.env
#   - Copies those three into DEST_DIR

DEST_DIR="${1:-./bin}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MODULE_DIR="$REPO_ROOT/operator-api"
ART_DIR="$MODULE_DIR/dist"

BUN_IMAGE="${BUN_IMAGE:-oven/bun:1}"

echo "Building kernel-operator-api with ${BUN_IMAGE}"
docker run --rm \
  -u "$(id -u):$(id -g)" \
  -v "$REPO_ROOT":/workspace \
  -w /workspace/operator-api \
  "${BUN_IMAGE}" \
  bash -lc 'bun i && bun run build'

for f in kernel-operator-api kernel-operator-test .env; do
  [[ -e "$ART_DIR/$f" ]] || { echo "Missing artifact: $ART_DIR/$f" >&2; exit 1; }
done

mkdir -p "$DEST_DIR"
cp -f "$ART_DIR/kernel-operator-api" "$DEST_DIR/"
cp -f "$ART_DIR/kernel-operator-test" "$DEST_DIR/"
cp -f "$ART_DIR/.kernel-operator.env" "$DEST_DIR/"

chmod +x "$DEST_DIR/kernel-operator-api" "$DEST_DIR/kernel-operator-test" || true

echo "✅ kernel-operator-api (bin) , kernel-operator-test (bin) , .kernel-operator.env copied to $DEST_DIR/..." 
