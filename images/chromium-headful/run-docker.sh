#!/usr/bin/env bash
set -ex -o pipefail

# Move to the script's directory so relative paths work regardless of the caller CWD
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"

IMAGE="${IMAGE:-onkernel/kernel-cu-test:latest}"
NAME="${NAME:-kernel-cu-test}"

# Directory on host where recordings will be saved
HOST_RECORDINGS_DIR="$SCRIPT_DIR/recordings"
mkdir -p "$HOST_RECORDINGS_DIR"

# Build docker run argument list
RUN_ARGS=(
  --name "$NAME"
  --privileged
  --tmpfs /dev/shm:size=2g
  -v "$HOST_RECORDINGS_DIR:/recordings"
  --memory 8192m
  -p 9222:9222 \
  -e DISPLAY_NUM=1 \
  -e HEIGHT=768 \
  -e WIDTH=1024 \
  -e CHROMIUM_FLAGS="--user-data-dir=/home/kernel/user-data --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=*"
)

if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  RUN_ARGS+=( -p 10001:10001 )
  RUN_ARGS+=( -e WITH_KERNEL_IMAGES_API=true )
fi

# noVNC vs WebRTC port mapping
if [[ "${ENABLE_WEBRTC:-}" == "true" ]]; then
  echo "Running container with WebRTC"
  RUN_ARGS+=( -p 8080:8080 )
  RUN_ARGS+=( -e ENABLE_WEBRTC=true )
  if [[ -n "${NEKO_ICESERVERS:-}" ]]; then
    RUN_ARGS+=( -e NEKO_ICESERVERS="$NEKO_ICESERVERS" )
  else
    RUN_ARGS+=( -e NEKO_WEBRTC_EPR=56000-56100 )
    RUN_ARGS+=( -e NEKO_WEBRTC_NAT1TO1=127.0.0.1 )
    RUN_ARGS+=( -p 56000-56100:56000-56100/udp )
  fi
else
  echo "Running container with noVNC"
  RUN_ARGS+=( -p 443:6080 )
fi

docker rm -f "$NAME" 2>/dev/null || true
docker run -it "${RUN_ARGS[@]}" "$IMAGE"
