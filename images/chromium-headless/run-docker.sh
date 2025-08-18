#!/usr/bin/env bash
set -ex -o pipefail

# Move to the script's directory so relative paths work regardless of the caller CWD
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"
source ../../shared/ensure-common-build-run-vars.sh chromium-headless

# Directory on host where recordings will be saved when the API is enabled
HOST_RECORDINGS_DIR="$SCRIPT_DIR/recordings"
mkdir -p "$HOST_RECORDINGS_DIR"

RUN_ARGS=(
  --name "$NAME"
  --privileged
  --tmpfs /dev/shm:size=2g
  -p 9222:9222
  -e WITH_DOCKER=true
)

if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  RUN_ARGS+=( -p 444:10001 )
  RUN_ARGS+=( -e WITH_KERNEL_IMAGES_API=true )
  RUN_ARGS+=( -v "$HOST_RECORDINGS_DIR:/recordings" )
fi

# If a positional argument is given, use it as the entrypoint
ENTRYPOINT_ARG=()
if [[ $# -ge 1 && -n "$1" ]]; then
  ENTRYPOINT_ARG+=(--entrypoint "$1")
fi

docker rm -f "$NAME" 2>/dev/null || true
docker run -it --rm "${ENTRYPOINT_ARG[@]}" "${RUN_ARGS[@]}" "$IMAGE"
