#!/bin/bash

set -o pipefail -o errexit -o nounset

# If we are outside Docker-in-Docker make sure /dev/shm exists
if [ -z "${WITH_DOCKER:-}" ]; then
  mkdir -p /dev/shm
  chmod 777 /dev/shm
  mount -t tmpfs tmpfs /dev/shm
fi

# if CHROMIUM_FLAGS is not set, default to the flags used in playwright_stealth
if [ -z "${CHROMIUM_FLAGS:-}" ]; then
  CHROMIUM_FLAGS="--accept-lang=en-US,en \
    --allow-pre-commit-input \
    --blink-settings=primaryHoverType=2,availableHoverTypes=2,primaryPointerType=4,availablePointerTypes=4 \
    --crash-dumps-dir=/tmp/chromium-dumps \
    --disable-back-forward-cache \
    --disable-background-networking \
    --disable-background-timer-throttling \
    --disable-backgrounding-occluded-windows \
    --disable-blink-features=AutomationControlled \
    --disable-breakpad \
    --disable-client-side-phishing-detection \
    --disable-component-extensions-with-background-pages \
    --disable-component-update \
    --disable-crash-reporter \
    --disable-crashpad \
    --disable-default-apps \
    --disable-dev-shm-usage \
    --disable-extensions \
    --disable-features=AcceptCHFrame,AutoExpandDetailsElement,AvoidUnnecessaryBeforeUnloadCheckSync,CertificateTransparencyComponentUpdater,DeferRendererTasksAfterInput,DestroyProfileOnBrowserClose,DialMediaRouteProvider,ExtensionManifestV2Disabled,GlobalMediaControls,HttpsUpgrades,ImprovedCookieControls,LazyFrameLoading,LensOverlay,MediaRouter,PaintHolding,ThirdPartyStoragePartitioning,Translate \
    --disable-field-trial-config \
    --disable-gcm-registration \
    --disable-gpu \
    --disable-gpu-compositing \
    --disable-hang-monitor \
    --disable-ipc-flooding-protection \
    --disable-notifications \
    --disable-popup-blocking \
    --disable-prompt-on-repost \
    --disable-renderer-backgrounding \
    --disable-search-engine-choice-screen \
    --disable-software-rasterizer \
    --enable-automation \
    --enable-use-zoom-for-dsf=false \
    --export-tagged-pdf \
    --force-color-profile=srgb \
    --hide-scrollbars \
    --metrics-recording-only \
    --mute-audio \
    --no-default-browser-check \
    --no-first-run \
    --no-sandbox \
    --no-service-autorun \
    --no-startup-window \
    --ozone-platform=headless \
    --password-store=basic \
    --unsafely-disable-devtools-self-xss-warnings \
    --use-angle \
    --use-gl=disabled \
    --use-mock-keychain"
fi

# -----------------------------------------------------------------------------
# House-keeping for the unprivileged "kernel" user --------------------------------
# Some Chromium subsystems want to create files under $HOME (NSS cert DB, dconf
# cache).  If those directories are missing or owned by root Chromium emits
# noisy error messages such as:
#   [ERROR:crypto/nss_util.cc:48] Failed to create /home/kernel/.pki/nssdb ...
#   dconf-CRITICAL **: unable to create directory '/home/kernel/.cache/dconf'
# Pre-create them and hand ownership to the user so the messages disappear.

for dir in /home/kernel/.pki/nssdb /home/kernel/.cache/dconf; do
  if [ ! -d "$dir" ]; then
    mkdir -p "$dir"
  fi
done
# Ensure correct ownership (ignore errors if already correct)
chown -R kernel:kernel /home/kernel/.pki /home/kernel/.cache 2>/dev/null || true

# -----------------------------------------------------------------------------
# System-bus setup --------------------------------------------------------------
# -----------------------------------------------------------------------------
# Start a lightweight system D-Bus daemon if one is not already running.  We
# will later use this very same bus as the *session* bus as well, avoiding the
# autolaunch fallback that produced many "Connection refused" errors.
# Start a lightweight system D-Bus daemon if one is not already running (Chromium complains otherwise)
if [ ! -S /run/dbus/system_bus_socket ]; then
  echo "Starting system D-Bus daemon"
  mkdir -p /run/dbus
  # Ensure a machine-id exists (required by dbus-daemon)
  dbus-uuidgen --ensure
  # Launch dbus-daemon in the background and remember its PID for cleanup
  dbus-daemon --system \
    --address=unix:path=/run/dbus/system_bus_socket \
    --nopidfile --nosyslog --nofork >/dev/null 2>&1 &
  dbus_pid=$!
fi

# We will point DBUS_SESSION_BUS_ADDRESS at the system bus socket to suppress
# autolaunch attempts that failed and spammed logs.
export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/dbus/system_bus_socket"

# Start Chromium in headless mode with remote debugging
# Use ncat to listen on 0.0.0.0:9222 since chromium does not let you listen on 0.0.0.0 anymore: https://github.com/pyppeteer/pyppeteer/pull/379#issuecomment-217029626
cleanup () {
  echo "Cleaning up..."
  kill -TERM $pid 2>/dev/null || true
  kill -TERM $pid2 2>/dev/null || true
  if [[ -n "${pid3:-}" ]]; then
    kill -TERM $pid3 2>/dev/null || true
  fi
  if [ -n "${dbus_pid:-}" ]; then
    kill -TERM $dbus_pid 2>/dev/null || true
  fi
}
trap cleanup TERM INT
pid=
pid2=
pid3=
INTERNAL_PORT=9223
CHROME_PORT=9222  # External port mapped to host
echo "Starting Chromium on internal port $INTERNAL_PORT"
export CHROMIUM_FLAGS
# Launch Chromium as the non-root user "kernel"
export HEIGHT=768
export WIDTH=1024
export DISPLAY=:1
echo "Starting Xvfb"
/usr/bin/xvfb_startup.sh
runuser -u kernel -- env DISPLAY=:1 DBUS_SESSION_BUS_ADDRESS="$DBUS_SESSION_BUS_ADDRESS" chromium \
  --headless \
  --remote-debugging-port=$INTERNAL_PORT \
  --remote-allow-origins=* \
  ${CHROMIUM_FLAGS:-} 2>&1 \
    | grep -vE "org\.freedesktop\.UPower|Failed to connect to the bus|google_apis" >&2 &
pid=$!
echo "Setting up ncat proxy on port $CHROME_PORT"
ncat \
  --sh-exec "ncat 0.0.0.0 $INTERNAL_PORT" \
  -l "$CHROME_PORT" \
  --keep-open & pid2=$!

# Optionally start the kernel-images API server file i/o
if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  echo "✨ Starting kernel-images API."
  API_PORT="${KERNEL_IMAGES_API_PORT:-10001}"
  API_FRAME_RATE="${KERNEL_IMAGES_API_FRAME_RATE:-10}"
  API_DISPLAY_NUM="${KERNEL_IMAGES_API_DISPLAY_NUM:-${DISPLAY_NUM:-1}}"
  API_MAX_SIZE_MB="${KERNEL_IMAGES_API_MAX_SIZE_MB:-500}"
  API_OUTPUT_DIR="${KERNEL_IMAGES_API_OUTPUT_DIR:-/recordings}"

  mkdir -p "$API_OUTPUT_DIR"

  PORT="$API_PORT" \
  FRAME_RATE="$API_FRAME_RATE" \
  DISPLAY_NUM="$API_DISPLAY_NUM" \
  MAX_SIZE_MB="$API_MAX_SIZE_MB" \
  OUTPUT_DIR="$API_OUTPUT_DIR" \
  /usr/local/bin/kernel-images-api & pid3=$!
fi

# Wait for Chromium to exit; propagate its exit code
wait "$pid"
exit_code=$?
echo "Chromium exited with code $exit_code"
# Ensure ncat proxy is terminated
kill -TERM "$pid2" 2>/dev/null || true
# Ensure kernel-images API server is terminated
if [[ -n "${pid3:-}" ]]; then
  kill -TERM "$pid3" 2>/dev/null || true
fi

exit "$exit_code"
