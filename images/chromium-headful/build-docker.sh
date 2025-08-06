#!/usr/bin/env bash
set -e -o pipefail

# Move to the script's directory so relative paths work regardless of the caller CWD
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"
source ../../shared/ensure-common-build-run-vars.sh chromium-headful

source ../../shared/start-buildkit.sh

# Build the kernel-images API binary and place it into ./bin for Docker build context
source ../../shared/build-server.sh "$(pwd)/bin"

# Build (and optionally push) the Docker image.
docker build -t "$IMAGE" .
