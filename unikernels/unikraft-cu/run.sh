#!/usr/bin/env bash

image="onkernel/kernel-cu-test:latest"
name="kernel-cu-test"

deploy_args=(
  -M 8192
  -p 443:6080/http+tls
  -p 9222:9222/tls
  -p 8080:8080/tls
  -e DISPLAY_NUM=1
  -e HEIGHT=768
  -e WIDTH=1024
  -e CHROMIUM_FLAGS="--no-sandbox --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --no-zygote"
  -e HOME=/
  -n "$name"
)

if [[ "${ENABLE_WEBRTC:-}" == "true" ]]; then
  echo "Deploying with WebRTC enabled"
  kraft cloud inst create --start \
    "${deploy_args[@]}" \
    -e ENABLE_WEBRTC=true \
    -e NEKO_ICESERVERS="${NEKO_ICESERVERS:-}" "$image"
else
  echo "Deploying without WebRTC"
  kraft cloud inst create --start "${deploy_args[@]}" "$image"
fi
