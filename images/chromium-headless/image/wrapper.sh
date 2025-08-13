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
export CHROMIUM_FLAGS

# -----------------------------------------------------------------------------
# House-keeping for the unprivileged "kernel" user ----------------------------
# -----------------------------------------------------------------------------
dirs=(
  /home/kernel/.pki/nssdb
  /home/kernel/.cache/dconf
  /var/log
  /var/log/supervisord
)
for dir in "${dirs[@]}"; do
  if [ ! -d "$dir" ]; then
    mkdir -p "$dir"
  fi
done
# Ensure correct ownership (ignore errors if already correct)
chown -R kernel:kernel /home/kernel/.pki /home/kernel/.cache 2>/dev/null || true

# Export common env used by services
export DISPLAY=:1
export HEIGHT=${HEIGHT:-768}
export WIDTH=${WIDTH:-1024}
export INTERNAL_PORT="${INTERNAL_PORT:-9223}"
export CHROME_PORT="${CHROME_PORT:-9222}"

# Cleanup handler
cleanup () {
  echo "[wrapper] Cleaning up..."
  supervisorctl -c /etc/supervisor/supervisord.conf stop chromium || true
  supervisorctl -c /etc/supervisor/supervisord.conf stop ncat || true
  supervisorctl -c /etc/supervisor/supervisord.conf stop xvfb || true
  supervisorctl -c /etc/supervisor/supervisord.conf stop dbus || true
  supervisorctl -c /etc/supervisor/supervisord.conf stop kernel-images-api || true
}
trap cleanup TERM INT

echo "[wrapper] Starting supervisord"
supervisord -c /etc/supervisor/supervisord.conf
echo "[wrapper] Waiting for supervisord socket..."
for i in {1..30}; do
  if [ -S /var/run/supervisor.sock ]; then
    break
  fi
  sleep 0.2
done

echo "[wrapper] Starting system D-Bus daemon via supervisord"
supervisorctl -c /etc/supervisor/supervisord.conf start dbus
for i in {1..50}; do
  if [ -S /run/dbus/system_bus_socket ]; then
    break
  fi
  sleep 0.2
done
export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/dbus/system_bus_socket"

echo "[wrapper] Starting Xvfb via supervisord"
supervisorctl -c /etc/supervisor/supervisord.conf start xvfb
for i in {1..50}; do
  if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

echo "[wrapper] Starting Chromium via supervisord on internal port $INTERNAL_PORT"
supervisorctl -c /etc/supervisor/supervisord.conf start chromium
for i in {1..100}; do
  if ncat -z 127.0.0.1 "$INTERNAL_PORT" 2>/dev/null; then
    break
  fi
  sleep 0.2
done

echo "[wrapper] Starting ncat proxy via supervisord on port $CHROME_PORT"
supervisorctl -c /etc/supervisor/supervisord.conf start ncat
for i in {1..50}; do
  if ncat -z 127.0.0.1 "$CHROME_PORT" 2>/dev/null; then
    break
  fi
  sleep 0.2
done

if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  echo "[wrapper] ✨ Starting kernel-images API via supervisord."
  supervisorctl -c /etc/supervisor/supervisord.conf start kernel-images-api
  API_PORT="${KERNEL_IMAGES_API_PORT:-10001}"
  echo "[wrapper] Waiting for kernel-images API on 127.0.0.1:${API_PORT}..."
  while ! ncat -z 127.0.0.1 "${API_PORT}" 2>/dev/null; do
    sleep 0.5
  done
fi

# Keep running while services are supervised; stream supervisor logs
tail -n +1 -F /var/log/supervisord/* 2>/dev/null &
wait
