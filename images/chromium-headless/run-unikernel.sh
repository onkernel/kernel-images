#!/usr/bin/env bash
set -euo pipefail

# Move to the script's directory so relative paths work regardless of the caller CWD
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"
source ../../shared/ensure-common-build-run-vars.sh chromium-headless

kraft cloud inst rm "$NAME" || true

RUN_AS_ROOT="${RUN_AS_ROOT:-false}"

deploy_args=(
  --start
  --scale-to-zero idle
  --scale-to-zero-cooldown 3000ms
  --scale-to-zero-stateful
  --vcpus 1
  -M 1024
  -e RUN_AS_ROOT="$RUN_AS_ROOT"
  -e LOG_CDP_MESSAGES=true \
  -p 9222:9222/tls
  -p 444:10001/tls
  -n "$NAME"
)

kraft cloud inst create "${deploy_args[@]}" "$IMAGE"
