#!/usr/bin/env bash

# namespace here (onkernel) should match UKC_TOKEN's username
image="onkernel/kernel-cu-test:latest"

# fail if UKC_TOKEN and UKC_METRO are not set
if [ -z "$UKC_TOKEN" ] || [ -z "$UKC_METRO" ]; then
    echo "UKC_TOKEN and UKC_METRO must be set"
    exit 1
fi
source ../../shared/start-buildkit.sh

# Build the API binary
source ../../shared/build-server.sh "$(pwd)/bin"

kraft pkg \
  --name index.unikraft.io/$image \
  --plat kraftcloud --arch x86_64 \
  --strategy overwrite \
  --push \
  .
