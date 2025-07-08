#!/bin/bash

set -o pipefail -o errexit -o nounset

# If the WITHDOCKER environment variable is not set, it means we are not running inside a Docker container.
# Docker manages /dev/shm itself, and attempting to mount or modify it can cause permission or device errors.
# However, in a unikernel container environment (non-Docker), we need to manually create and mount /dev/shm as a tmpfs
# to support shared memory operations.
if [ -z "${WITHDOCKER:-}" ]; then
  mkdir -p /dev/shm
  chmod 777 /dev/shm
  mount -t tmpfs tmpfs /dev/shm
fi

export DISPLAY=:1

/usr/bin/Xorg :1 -config /etc/neko/xorg.conf -noreset -nolisten tcp &

./mutter_startup.sh

if [[ "${ENABLE_WEBRTC:-}" != "true" ]]; then
  ./x11vnc_startup.sh
fi

# Start Chromium with display :1 and remote debugging, loading our recorder extension.
# Use ncat to listen on 0.0.0.0:9222 since chromium does not let you listen on 0.0.0.0 anymore: https://github.com/pyppeteer/pyppeteer/pull/379#issuecomment-217029626
cleanup () {
  echo "Cleaning up..."
  kill -TERM $pid
  kill -TERM $pid2
  # Kill the API server if it was started
  if [[ -n "${pid3:-}" ]]; then
    kill -TERM $pid3 || true
  fi
}
trap cleanup TERM INT
pid=
pid2=
pid3=
INTERNAL_PORT=9223
CHROME_PORT=9222  # External port mapped to host
echo "Starting Chromium on internal port $INTERNAL_PORT"
chromium \
  --remote-debugging-port=$INTERNAL_PORT \
  ${CHROMIUM_FLAGS:-} >&2 &
echo "Setting up ncat proxy on port $CHROME_PORT"
ncat \
  --sh-exec "ncat 0.0.0.0 $INTERNAL_PORT" \
  -l "$CHROME_PORT" \
  --keep-open & pid2=$!

if [[ "${ENABLE_WEBRTC:-}" == "true" ]]; then
  # use webrtc
  echo "✨ Starting neko (webrtc server)."
  /usr/bin/neko serve --server.static /var/www --server.bind 0.0.0.0:8080 >&2 &
else
  # use novnc
  ./novnc_startup.sh
  echo "✨ noVNC demo is ready to use!"
fi

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

# Keep the container running
tail -f /dev/null
