#!/usr/bin/env bash
set -euo pipefail

# Move to the script's directory so relative paths work regardless of the caller CWD
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"
source ../../shared/ensure-common-build-run-vars.sh chromium-headless

kraft cloud inst rm "$NAME" || true

deploy_args=(
  --start
  -M 1G
  -p 9222:9222/tls
  --vcpus 1
  -n "$NAME"
)

if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  deploy_args+=( -p 444:10001/tls )
  deploy_args+=( -e WITH_KERNEL_IMAGES_API=true )
fi

kraft cloud inst create "${deploy_args[@]}" "$IMAGE"
