#!/usr/bin/env bash

source common.sh
name="chromium-headless-test"

kraft cloud inst rm "$name" || true

kraft cloud inst create \
  --start \
  -M 1G \
  -p 9222:9222/tls \
  --vcpus 1 \
  -n "$name" \
  $IMAGE
