#!/usr/bin/env bash

source common.sh
name=chromium-headful-test

# Name for the Kraft Cloud volume that will carry Chromium flags
volume_name="${name}-flags"

# ------------------------------------------------------------------------------
# Prepare Kraft Cloud volume containing Chromium flags
# ------------------------------------------------------------------------------
# Build a temporary directory with a single file "flags" that holds all
# Chromium runtime flags. This directory will be imported into a Kraft Cloud
# volume which we then mount into the image at /chromium.
CHROMIUM_FLAGS_DEFAULT="--user-data-dir=/home/kernel/user-data --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --no-sandbox --no-zygote"
CHROMIUM_FLAGS="${CHROMIUM_FLAGS:-$CHROMIUM_FLAGS_DEFAULT}"
rm -rf .tmp/chromium
mkdir -p .tmp/chromium
FLAGS_DIR=".tmp/chromium"
echo "$CHROMIUM_FLAGS" > "$FLAGS_DIR/flags"

# Re-create the volume from scratch every run
kraft cloud volume rm "$volume_name" || true
kraft cloud volume create -n "$volume_name" -s 16M
# Import the flags directory into the freshly created volume
kraft cloud volume import -s "$FLAGS_DIR" -v "$volume_name"

# Ensure the temp directory is cleaned up on exit
trap 'rm -rf "$FLAGS_DIR"' EXIT

#   -e CHROMIUM_FLAGS="--user-data-dir=/home/kernel/user-data --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --disable-breakpad --crash-dumps-dir=/tmp --no-sandbox --no-zygote"


deploy_args=(
  -M 8192
  -p 9222:9222/tls
  -p 8080:8080/tls
  -e DISPLAY_NUM=1
  -e HEIGHT=768
  -e WIDTH=1024
  -e HOME=/
  -v "$volume_name":/chromium
  -n "$name"
)

if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  deploy_args+=( -p 444:10001/tls )
  deploy_args+=( -e WITH_KERNEL_IMAGES_API=true )
fi

kraft cloud inst rm $name || true

if [[ "${ENABLE_WEBRTC:-}" == "true" ]]; then
  echo "Deploying with WebRTC enabled"
  kraft cloud inst create --start \
    "${deploy_args[@]}" \
    -p 443:8080/http+tls \
    -e ENABLE_WEBRTC=true \
    -e NEKO_ICESERVERS="${NEKO_ICESERVERS:-}" "$IMAGE"
else
  echo "Deploying without WebRTC"
  kraft cloud inst create --start \
    "${deploy_args[@]}" \
    -p 443:6080/http+tls \
    "$IMAGE"
fi
