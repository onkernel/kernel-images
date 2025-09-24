#!/usr/bin/env bash
set -ex -o pipefail

# Move to the script's directory so relative paths work regardless of the caller CWD
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"
source ../../shared/ensure-common-build-run-vars.sh chromium-headful

# Directory on host where recordings will be saved
HOST_RECORDINGS_DIR="$SCRIPT_DIR/recordings"
mkdir -p "$HOST_RECORDINGS_DIR"

# RUN_AS_ROOT defaults to false in docker
RUN_AS_ROOT="${RUN_AS_ROOT:-false}"

# Build Chromium flags file and mount
CHROMIUM_FLAGS_DEFAULT="--user-data-dir=/home/kernel/user-data --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=*"
if [[ "$RUN_AS_ROOT" == "true" ]]; then
  CHROMIUM_FLAGS_DEFAULT="$CHROMIUM_FLAGS_DEFAULT --no-sandbox --no-zygote"
fi
if [[ -n "${XDS_JWT:-}" ]]; then
  CHROMIUM_FLAGS_DEFAULT="$CHROMIUM_FLAGS_DEFAULT --proxy-server=https://127.0.0.1:3128"
fi
CHROMIUM_FLAGS="${CHROMIUM_FLAGS:-$CHROMIUM_FLAGS_DEFAULT}"
rm -rf .tmp/chromium
mkdir -p .tmp/chromium
FLAGS_FILE="$(pwd)/.tmp/chromium/flags"
echo "$CHROMIUM_FLAGS" > "$FLAGS_FILE"

echo "flags file: $FLAGS_FILE"
cat "$FLAGS_FILE"

# Build docker run argument list
RUN_ARGS=(
  --name "$NAME"
  --privileged
  --tmpfs /dev/shm:size=2g
  -v "$HOST_RECORDINGS_DIR:/recordings"
  --memory 8192m
  -p 9222:9222
  -p 444:10001
  -e DISPLAY_NUM=1
  -e HEIGHT=768
  -e WIDTH=1024
  -e RUN_AS_ROOT="$RUN_AS_ROOT"
  --mount type=bind,src="$FLAGS_FILE",dst=/chromium/flags,ro
)

# Add XDS environment variables if provided
if [[ -n "${INST_NAME:-}" ]]; then
  RUN_ARGS+=( -e "INST_NAME=$INST_NAME" )
fi
if [[ -n "${METRO_NAME:-}" ]]; then
  RUN_ARGS+=( -e "METRO_NAME=$METRO_NAME" )
fi
if [[ -n "${XDS_SERVER:-}" ]]; then
  RUN_ARGS+=( -e "XDS_SERVER=$XDS_SERVER" )
fi
if [[ -n "${XDS_JWT:-}" ]]; then
  RUN_ARGS+=( -e "XDS_JWT=$XDS_JWT" )
  RUN_ARGS+=( -p 9901:9901 )
  RUN_ARGS+=( -p 3128:3128 )
  # TODO: import bright data and self-signed client proxy cert to chrome instead of ignore
  CHROMIUM_FLAGS_DEFAULT="$CHROMIUM_FLAGS_DEFAULT --proxy-server=https://127.0.0.1:3128 --ignore-certificate-errors"
fi

# WebRTC port mapping
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
fi

docker rm -f "$NAME" 2>/dev/null || true
docker run -it "${RUN_ARGS[@]}" "$IMAGE"
