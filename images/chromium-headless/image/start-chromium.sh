#!/bin/bash

set -o pipefail -o errexit -o nounset

echo "Starting Chromium launcher (headless)"

# Resolve internal port for the remote debugging interface
INTERNAL_PORT="${INTERNAL_PORT:-9223}"

# Load flags from env (base) and optional runtime overlay file
BASE_FLAGS="${CHROMIUM_FLAGS:-}"
RUNTIME_FLAGS=""
if [[ -f /chromium/flags ]]; then
  RUNTIME_FLAGS="$(cat /chromium/flags)"
fi

# When runtime overlay includes extension directives, strip conflicting flags
# from the base (e.g. --disable-extensions and prior extension directives)
has_extension_overlay=false
if [[ "$RUNTIME_FLAGS" == *"--load-extension"* || "$RUNTIME_FLAGS" == *"--disable-extensions-except"* ]]; then
  has_extension_overlay=true
fi

FILTERED_BASE=()
if [[ "$has_extension_overlay" == true ]]; then
  for tok in $BASE_FLAGS; do
    case "$tok" in
      --disable-extensions|--disable-extensions=*|--load-extension|--load-extension=*|--disable-extensions-except|--disable-extensions-except=*)
        # drop conflicting/duplicate extension-related flags from base
        ;;
      *)
        FILTERED_BASE+=("$tok")
        ;;
    esac
  done
else
  # no overlay, keep base as-is
  for tok in $BASE_FLAGS; do
    FILTERED_BASE+=("$tok")
  done
fi

# Merge filtered base with runtime overlay, deduplicating while preserving order
COMBINED=()
for tok in "${FILTERED_BASE[@]}"; do
  COMBINED+=("$tok")
done
for tok in $RUNTIME_FLAGS; do
  COMBINED+=("$tok")
done

declare -A SEEN
DEDUP=()
for tok in "${COMBINED[@]}"; do
  if [[ -z "${SEEN[$tok]:-}" && -n "$tok" ]]; then
    DEDUP+=("$tok")
    SEEN[$tok]=1
  fi
done

FINAL_FLAGS="${DEDUP[*]}"

echo "BASE_FLAGS: $BASE_FLAGS"
echo "RUNTIME_FLAGS: $RUNTIME_FLAGS"
echo "FINAL_FLAGS: $FINAL_FLAGS"

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
    ${FINAL_FLAGS:-}
else
  echo "Running chromium as kernel user"
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
    ${FINAL_FLAGS:-}
fi
