#!/usr/bin/env bash

source common.sh
name=chromium-headful-test

deploy_args=(
  -M 8192
  -p 9222:9222/tls
  -p 8080:8080/tls
  -e DISPLAY_NUM=1
  -e HEIGHT=768
  -e WIDTH=1024
  -e CHROMIUM_FLAGS="--user-data-dir=/home/kernel/user-data --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --disable-breakpad"
  -e HOME=/
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
