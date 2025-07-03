#!/usr/bin/env bash
set -e -o pipefail

# fail if IMAGE, UKC_TOKEN, UKC_METRO, UKC_INDEX are not set
errormsg=""
for var in IMAGE UKC_TOKEN UKC_METRO UKC_INDEX; do
    if [ -z "${!var}" ]; then
        errormsg+="$var "
    fi
done
if [ -n "$errormsg" ]; then
    echo "Required variables not set: $errormsg"
    exit 1
fi
