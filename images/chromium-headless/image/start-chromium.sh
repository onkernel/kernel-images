#!/bin/bash

set -o pipefail -o errexit -o nounset

echo "Starting Chromium launcher (headless)"

# Resolve internal port for the remote debugging interface
INTERNAL_PORT="${INTERNAL_PORT:-9223}"

# Load additional Chromium flags from env and optional file
CHROMIUM_FLAGS="${CHROMIUM_FLAGS:-}"
if [[ -f /chromium/flags ]]; then
  CHROMIUM_FLAGS="$CHROMIUM_FLAGS $(cat /chromium/flags)"
fi
echo "CHROMIUM_FLAGS: $CHROMIUM_FLAGS"

# Always use display :1 and point DBus to the system bus socket
export DISPLAY=":1"
export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/dbus/system_bus_socket"

RUN_AS_ROOT="${RUN_AS_ROOT:-false}"

if [[ "$RUN_AS_ROOT" == "true" ]]; then
  exec chromium \
    --headless=new \
    --remote-debugging-port="$INTERNAL_PORT" \
    --remote-allow-origins=* \
    --user-data-dir=/home/kernel/user-data \
    --password-store=basic \
    --no-first-run \
    ${CHROMIUM_FLAGS:-}
else
  exec runuser -u kernel -- env \
    DISPLAY=":1" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=/run/dbus/system_bus_socket" \
    XDG_CONFIG_HOME=/home/kernel/.config \
    XDG_CACHE_HOME=/home/kernel/.cache \
    HOME=/home/kernel \
    chromium \
    --headless=new \
    --remote-debugging-port="$INTERNAL_PORT" \
    --remote-allow-origins=* \
    --user-data-dir=/home/kernel/user-data \
    --password-store=basic \
    --no-first-run \
    ${CHROMIUM_FLAGS:-}
fi
