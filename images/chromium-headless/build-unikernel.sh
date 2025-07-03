#!/usr/bin/env bash

source common.sh

# Function to check if mkfs.erofs is available
check_mkfs_erofs() {
    if command -v mkfs.erofs &>/dev/null; then
        echo "mkfs.erofs is already installed."
        return 0
    else
        echo "mkfs.erofs is not installed."
        return 1
    fi
}

# Function to install erofs-utils package
install_erofs_utils() {
    if command -v apt-get &>/dev/null; then
        echo "Detected Ubuntu/Debian-based system. Installing erofs-utils..."
        sudo apt update
        sudo apt install -y erofs-utils
    elif command -v dnf &>/dev/null; then
        echo "Detected Fedora-based system. Installing erofs-utils..."
        sudo dnf install -y erofs-utils
    elif command -v yum &>/dev/null; then
        echo "Detected CentOS/RHEL-based system. Installing erofs-utils..."
        sudo yum install -y erofs-utils
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        if command -v brew &>/dev/null; then
            echo "Detected macOS. Installing erofs-utils..."
            brew install erofs-utils
        else
            echo "Homebrew (brew) not found. Please install Homebrew first."
            exit 1
        fi
    else
        echo "Unsupported operating system or package manager. Please install erofs-utils manually."
        exit 1
    fi
}

check_mkfs_erofs
if [ $? -ne 0 ]; then
    echo "mkfs.erofs is not installed. Installing erofs-utils..."
    install_erofs_utils
fi

set -euo pipefail  

cd image/

# Build the root file system
rm -rf ./.rootfs || true

# Load configuration
app_name=chromium-headless-test

docker build --platform linux/amd64 -t "$IMAGE" .
docker rm cnt-"$app_name" || true
docker create --platform linux/amd64 --name cnt-"$app_name" "$IMAGE" /bin/sh
docker cp cnt-"$app_name":/ ./.rootfs
rm -f initrd || true
mkfs.erofs --all-root -d2 -E noinline_data -b 4096 initrd ./.rootfs

kraft pkg \
  --name  $UKC_INDEX/$IMAGE \
  --plat kraftcloud \
  --arch x86_64 \
  --strategy overwrite \
  --push \
  .
