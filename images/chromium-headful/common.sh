#!/usr/bin/env bash
set -e -o pipefail

# Default UKC_INDEX to index.unikraft.io if not set
UKC_INDEX="${UKC_INDEX:-index.unikraft.io}"

# fail if IMAGE, UKC_TOKEN, UKC_METRO are not set
errormsg=""
for var in IMAGE UKC_TOKEN UKC_METRO; do
    if [ -z "${!var}" ]; then
        errormsg+="$var "
    fi
done
if [ -n "$errormsg" ]; then
    echo "Required variables not set: $errormsg"
    exit 1
fi
