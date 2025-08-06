#!/usr/bin/env bash
set -e -o pipefail

IMAGE_TYPE=$1
if [ -z "$IMAGE_TYPE" ]; then
    echo "Usage: source ensure-common-build-run-vars.sh <image-type>"
    echo "e.g. source ensure-common-build-run-vars.sh chromium-headful"
    echo "This will set the defaults for the image name and test instance name"
    echo "You can override the defaults by setting the IMAGE and NAME variables"
    return 1
fi
IMAGE="${IMAGE:-onkernel/${IMAGE_TYPE}-test:latest}"
NAME="${NAME:-${IMAGE_TYPE}-test}"

UKC_INDEX="${UKC_INDEX:-index.unikraft.io}"

# fail if UKC_TOKEN, UKC_METRO are not set
errormsg=""
for var in UKC_TOKEN UKC_METRO; do
    if [ -z "${!var}" ]; then
        errormsg+="$var "
    fi
done
if [ -n "$errormsg" ]; then
    echo "Required variables not set: $errormsg"
    exit 1
fi
