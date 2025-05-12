#!/bin/bash

set -o pipefail -o errexit -o nounset

if [ -z "${WITHDOCKER:-}" ]; then
  mkdir -p /dev/shm
  chmod 777 /dev/shm
  mount -t tmpfs tmpfs /dev/shm
fi

export DISPLAY=:1

/usr/bin/Xorg :1 -config /etc/neko/xorg.conf -noreset -nolisten tcp &

sleep 2
./mutter_startup.sh

sleep 2

if [[ "${ENABLE_WEBRTC:-}" != "true" ]]; then
  ./x11vnc_startup.sh
fi

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
  export NEKO_MEMBER_PROVIDER=noauth
  export NEKO_PLUGINS_ENABLED=false
  export NEKO_CHAT_ENABLED=false
  export NEKO_FILETRANSFER_ENABLED=false
  echo "✨ Starting neko (webrtc server)."
  /usr/bin/neko serve --server.static /var/www --server.bind 0.0.0.0:8080 >&2
else
  # use novnc
  ./novnc_startup.sh
  echo "✨ noVNC demo is ready to use!"
fi

# Keep the container running
tail -f /dev/null
