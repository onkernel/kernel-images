#!/usr/bin/env bash
set -e -o pipefail

IMAGE_TYPE=$1
if [ -z "$IMAGE_TYPE" ]; then
    echo "Usage: source ensure-common-build-run-vars.sh <image-type> [require-ukc-vars]"
    echo "e.g. source ensure-common-build-run-vars.sh chromium-headful"
    echo "     source ensure-common-build-run-vars.sh chromium-headful require-ukc-vars"
    echo "This will set the defaults for the image name and test instance name"
    echo "You can override the defaults by setting the IMAGE and NAME variables"
    echo "Pass 'require-ukc-vars' as second argument to require UKC_TOKEN/UKC_METRO"
    return 1
fi
IMAGE="${IMAGE:-onkernel/${IMAGE_TYPE}-test:latest}"
NAME="${NAME:-${IMAGE_TYPE}-test}"

UKC_INDEX="${UKC_INDEX:-index.unikraft.io}"

# Only require UKC_TOKEN and UKC_METRO when explicitly requested
# Pass "require-ukc-vars" as second argument to enable this check
REQUIRE_UKC_VARS="${2:-}"

if [ "$REQUIRE_UKC_VARS" == "require-ukc-vars" ]; then
    # fail if UKC_TOKEN, UKC_METRO are not set
    errormsg=""
    for var in UKC_TOKEN UKC_METRO; do
        if [ -z "${!var}" ]; then
            errormsg+="$var "
        fi
    done
    if [ -n "$errormsg" ]; then
        echo "Required variables not set: $errormsg"
        return 1
    fi
fi
