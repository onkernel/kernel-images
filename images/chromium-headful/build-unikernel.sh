#!/usr/bin/env bash

source common.sh
source ../../shared/erofs-utils.sh

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
app_name=chromium-headful-build
docker build --platform linux/amd64 -t "$IMAGE" .
docker rm cnt-"$app_name" || true
docker create --platform linux/amd64 --name cnt-"$app_name" "$IMAGE" /bin/sh
docker cp cnt-"$app_name":/ ./.rootfs
rm -f initrd || true
sudo mkfs.erofs --all-root -d2 -E noinline_data -b 4096 initrd ./.rootfs

# Package the unikernel (and the new initrd) to KraftCloud
kraft pkg \
  --name $UKC_INDEX/$IMAGE \
  --plat kraftcloud \
  --arch x86_64 \
  --strategy overwrite \
  --push \
  .
