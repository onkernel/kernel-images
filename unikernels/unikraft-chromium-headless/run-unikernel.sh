#!/usr/bin/env bash

source common.sh
image="onkernel/$IMAGE:latest"
name="chromium-headless-test"

kraft cloud inst rm "$name" || true

kraft cloud inst create \
  --start \
  -M 1.5Gi \
  -p 9222:9222/tls \
  --vcpus 1 \
  -n "$name" \
  $image
