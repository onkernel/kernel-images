#!/usr/bin/env bash

source common.sh

docker run -it --rm \
  -p 9222:9222 \
  -e WITH_DOCKER=true \
  $IMAGE /usr/bin/wrapper.sh
