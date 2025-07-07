#!/usr/bin/env bash

# build-server.sh
# -------------------------
# Usage (source or execute):
#   build-recording-server.sh [dest-dir] [goos] [goarch]
#
#   dest-dir  (optional) Directory to place the resulting binary. Defaults to ./bin
#   goos      (optional) Target GOOS for cross-compilation. Defaults to linux
#   goarch    (optional) Target GOARCH for cross-compilation. Defaults to amd64
#
# Examples
#   source ../../shared/build-recording-server.sh              # → ./bin, linux/amd64
#   ../../shared/build-recording-server.sh ./bin arm64         # → linux/arm64
#   ../../shared/build-recording-server.sh ./out darwin arm64  # → darwin/arm64
set -euo pipefail

DEST_DIR="${1:-./bin}"
# Optional os/arch parameters
TARGET_OS="${2:-linux}"
TARGET_ARCH="${3:-amd64}"

# Resolve repo root as the parent directory of this script's directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# 1. Build the binary in the server module
GO_REQUIRED=$(grep -E "^go [0-9]+\.[0-9]+" "$REPO_ROOT/server/go.mod" | head -n1 | awk '{print $2}')
echo "🔨 Building kernel-images-api binary using go ${GO_REQUIRED}"
docker run --rm \
  -e GOOS="$TARGET_OS" \
  -e GOARCH="$TARGET_ARCH" \
  -e CGO_ENABLED=0 \
  -e GOFLAGS="-buildvcs=false" \
  -v "$REPO_ROOT":/workspace \
  -w /workspace/server \
  "golang:${GO_REQUIRED}" \
  make build

# 2. Copy to destination
mkdir -p "$DEST_DIR"
cp "$REPO_ROOT/server/bin/api" "$DEST_DIR/kernel-images-api"

echo "✅ kernel-images-api binary copied to $DEST_DIR/kernel-images-api" 
