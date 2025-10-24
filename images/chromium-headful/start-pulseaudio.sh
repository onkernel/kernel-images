#!/bin/bash

set -o pipefail -o errexit -o nounset

if [[ "$RUN_AS_ROOT" == "true" ]]; then
  echo "Not starting PulseAudio daemon when running as root"
else
  exec runuser -u kernel -- pulseaudio \
    --start \
    --exit-idle-time=-1 \
    --load="module-null-sink sink_name=DummyOutput" \
    --load="module-null-source source_name=DummyInput"
fi
