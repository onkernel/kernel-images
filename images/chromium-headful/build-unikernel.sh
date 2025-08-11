#!/usr/bin/env bash

# Flag to control whether to use EROFS or not
EROFS_DISABLE=${EROFS_DISABLE:-false}

# Move to the script's directory so relative paths work regardless of the caller CWD
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/../../shared/ensure-common-build-run-vars.sh" chromium-headful

# Copy the appropriate Kraftfile based on EROFS_DISABLE flag
if [ "$EROFS_DISABLE" = "false" ]; then
  echo "Using EROFS configuration (default)..."
  cp Kraftfile.erofs Kraftfile
  source "$SCRIPT_DIR/../../shared/erofs-utils.sh"

  # Ensure the mkfs.erofs tool is available
  if ! check_mkfs_erofs; then
      echo "mkfs.erofs is not installed. Installing erofs-utils..."
      install_erofs_utils
  fi

  set -euo pipefail  

  # Build the root file system
  source ../../shared/start-buildkit.sh

  rm -rf ./.rootfs || true

  # Build the API binary
  source ../../shared/build-server.sh "$(pwd)/bin"

  # Build operator api + test + .env → ./bin
  source ../../shared/build-operator-api.sh "$(pwd)/bin"

  app_name=chromium-headful-build
  docker build --platform linux/amd64 -t "$IMAGE" .
  docker rm cnt-"$app_name" || true
  docker create --platform linux/amd64 --name cnt-"$app_name" "$IMAGE" /bin/sh
  docker cp cnt-"$app_name":/ ./.rootfs
  rm -f initrd || true
  # sudo mkfs.erofs --all-root -d2 -E noinline_data -b 4096 initrd ./.rootfs
  # default block size is 4096 and -b fails for some reason, removed it
  sudo mkfs.erofs --all-root -d2 -E noinline_data initrd ./.rootfs
  
  # Package the unikernel (and the new initrd) to KraftCloud
  kraft pkg \
    --name $UKC_INDEX/$IMAGE \
    --plat kraftcloud \
    --arch x86_64 \
    --strategy overwrite \
    --push \
    .
else
  echo "Using non-EROFS configuration..."
  cp Kraftfile.no-erofs Kraftfile
  
  set -euo pipefail  

  source ../../shared/start-buildkit.sh

  # Build the API binary
  source ../../shared/build-server.sh "$(pwd)/bin"

  # Build operator api + test + .env → ./bin
  source ../../shared/build-operator-api.sh "$(pwd)/bin"

  # Package the unikernel to KraftCloud
  kraft pkg \
    --name $UKC_INDEX/$IMAGE \
    --plat kraftcloud --arch x86_64 \
    --strategy overwrite \
    --push \
    .
fi
