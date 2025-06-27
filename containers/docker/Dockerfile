FROM ghcr.io/anthropics/anthropic-quickstarts:computer-use-demo-latest

USER root
RUN add-apt-repository -y ppa:xtradeb/apps
RUN apt update -y && apt install -y chromium ncat ffmpeg

# Switch to computeruse user
USER computeruse

# copy the kernel-images API binary built by build.sh
COPY bin/kernel-images-api /usr/local/bin/kernel-images-api
ENV WITH_KERNEL_IMAGES_API=false

# Modify entrypoint script
# The original can be found here: https://github.com/anthropics/anthropic-quickstarts/blob/main/computer-use-demo/image/entrypoint.sh
COPY --chmod=0755 <<'EOL' /home/computeruse/entrypoint.sh
#!/bin/bash
set -o pipefail -o errexit -o nounset

./start_all.sh >&2

# Start Chromium with display :1 and remote debugging
# Use ncat to listen on 0.0.0.0:9222 since chromium does not let you listen on 0.0.0.0 anymore: https://github.com/pyppeteer/pyppeteer/pull/379#issuecomment-217029626
cleanup () {
  echo "Cleaning up..."
  kill -TERM $pid
  kill -TERM $pid2
  # Kill API pid if set
  if [[ -n "${pid3:-}" ]]; then
    kill -TERM $pid3 || true
  fi
}
trap cleanup TERM INT
pid=
pid2=
pid3=
INTERNAL_PORT=9223
CHROME_PORT=9222  # External port mapped in Docker
echo "Starting Chromium on internal port $INTERNAL_PORT"
DISPLAY=:1 chromium \
  --remote-debugging-port=$INTERNAL_PORT \
  ${CHROMIUM_FLAGS:-} >&2 &
  
echo "Setting up ncat proxy on port $CHROME_PORT"
ncat \
  --sh-exec "ncat localhost $INTERNAL_PORT" \
  -l "$CHROME_PORT" \
  --keep-open & pid2=$!

./novnc_startup.sh >&2

if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  echo "✨ Starting kernel-images API."

  API_PORT="${KERNEL_IMAGES_API_PORT:-10001}"
  API_FRAME_RATE="${KERNEL_IMAGES_API_FRAME_RATE:-10}"
  API_DISPLAY_NUM="${KERNEL_IMAGES_API_DISPLAY_NUM:-1}"
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

python http_server.py >&2 &

STREAMLIT_SERVER_PORT=8501 python -m streamlit run computer_use_demo/streamlit.py >&2
EOL

WORKDIR /home/computeruse
ENTRYPOINT ["./entrypoint.sh"]
