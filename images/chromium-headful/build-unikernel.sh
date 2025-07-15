#!/usr/bin/env bash

source common.sh
set -euo pipefail  

source ../../shared/start-buildkit.sh

# Build the API binary
source ../../shared/build-server.sh "$(pwd)/bin"

kraft pkg \
  --name $UKC_INDEX/$IMAGE \
  --plat kraftcloud --arch x86_64 \
  --strategy overwrite \
  --push \
  .
