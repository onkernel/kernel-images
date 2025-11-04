#!/bin/bash

set -o pipefail -o errexit -o nounset

if [[ "${RUN_AS_ROOT:-false}" == "true" ]]; then
  echo "[pulseaudio] Not starting PulseAudio daemon when running as root"
  exit 0
fi

# Set up permissions before starting PulseAudio (strict - fail if this doesn't work)
echo "[pulseaudio] Setting up permissions"
chown -R kernel:kernel /home/kernel/ /home/kernel/.config /etc/pulse
chmod 777 /home/kernel/.config /etc/pulse
chown -R kernel:kernel /tmp/runtime-kernel

# Start PulseAudio as kernel user using config files
echo "[pulseaudio] Starting daemon as kernel user"
exec runuser -u kernel -- env \
  XDG_RUNTIME_DIR=/tmp/runtime-kernel \
  XDG_CONFIG_HOME=/home/kernel/.config \
  XDG_CACHE_HOME=/home/kernel/.cache \
  pulseaudio --log-level=error \
             --disallow-module-loading \
             --disallow-exit \
             --exit-idle-time=-1
