#!/bin/bash

set -o pipefail -o errexit -o nounset


mkdir -p /dev/shm
chmod 777 /dev/shm
mount -t tmpfs tmpfs /dev/shm

./start_all.sh >&2

# Start Chromium with display :1 and remote debugging, loading our recorder extension.
# Use ncat to listen on 0.0.0.0:9222 since chromium does not let you listen on 0.0.0.0 anymore: https://github.com/pyppeteer/pyppeteer/pull/379#issuecomment-217029626
cleanup () {
  echo "Cleaning up..."
  kill -TERM $pid
  kill -TERM $pid2
}
trap cleanup TERM INT
pid=
pid2=
INTERNAL_PORT=9223
CHROME_PORT=9222  # External port mapped to host
echo "Starting Chromium on internal port $INTERNAL_PORT"
DISPLAY=:1 chromium \
  --remote-debugging-port=$INTERNAL_PORT \
  --no-sandbox \
  --disable-dev-shm-usage \
  --disable-gpu \
  --disable-software-rasterizer \
  --remote-allow-origins=* \
  --no-zygote >&2 &
echo "Setting up ncat proxy on port $CHROME_PORT"
ncat \
  --sh-exec "ncat 0.0.0.0 $INTERNAL_PORT" \
  -l "$CHROME_PORT" \
  --keep-open & pid2=$!

./novnc_startup.sh

echo "✨ noVNC demo is ready to use!"

# Keep the container running
tail -f /dev/null
