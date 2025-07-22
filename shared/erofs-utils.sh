#!/usr/bin/env bash
# erofs-utils.sh
# ----------------
# Shared utility functions for verifying that mkfs.erofs (provided by the
# erofs-utils package) is available on the host and installing it when it is
# missing.  This script is meant to be sourced by other build scripts.

set -e -o pipefail

# Returns 0 if mkfs.erofs is on the PATH, 1 otherwise.
check_mkfs_erofs() {
    if command -v mkfs.erofs &>/dev/null; then
        return 0
    else
        return 1
    fi
}

# Installs the erofs-utils package using the host's package manager.
install_erofs_utils() {
    if command -v apt-get &>/dev/null; then
        echo "Detected Debian/Ubuntu system. Installing erofs-utils …"
        sudo apt update
        sudo apt install -y erofs-utils
    elif command -v dnf &>/dev/null; then
        echo "Detected Fedora system. Installing erofs-utils …"
        sudo dnf install -y erofs-utils
    elif command -v yum &>/dev/null; then
        echo "Detected CentOS/RHEL system. Installing erofs-utils …"
        sudo yum install -y erofs-utils
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        if command -v brew &>/dev/null; then
            echo "Detected macOS system. Installing erofs-utils via Homebrew …"
            brew install erofs-utils
        else
            echo "Homebrew is required but not found. Please install Homebrew first."
            exit 1
        fi
    else
        echo "Unsupported operating system or package manager; please install erofs-utils manually."
        exit 1
    fi
} 

# on debian 12 you have to grab mkfs.erofs from sid:
# echo "deb http://deb.debian.org/debian unstable main" \
#     | sudo tee /etc/apt/sources.list.d/unstable.list
# cat <<'EOF' | sudo tee /etc/apt/preferences.d/90-unstable
# Package: *
# Pin: release a=unstable
# Pin-Priority: 90
# EOF
# sudo apt update
# sudo apt -t unstable install erofs-utils
