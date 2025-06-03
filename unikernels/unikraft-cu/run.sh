#!/usr/bin/env bash

image="onkernel/kernel-cu-test:latest"
name="kernel-cu-test"

kraft cloud inst create \
  --start 
	-M 8192 \
	-p 443:6080/http+tls \
    -p 9222:9222/tls \
	-e DISPLAY_NUM=1 \
	-e HEIGHT=768 \
	-e WIDTH=1024 \
	-e HOME=/ \
	-e CHROMIUM_FLAGS="--no-sandbox --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --no-zygote" \
	-n "$name" \
    $image
