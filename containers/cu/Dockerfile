FROM ghcr.io/anthropics/anthropic-quickstarts:computer-use-demo-latest

USER root
RUN add-apt-repository -y ppa:xtradeb/apps
RUN apt update -y && apt install -y chromium ncat

# Switch to computeruse user
USER computeruse

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
}
trap cleanup TERM INT
pid=
pid2=
INTERNAL_PORT=9223
CHROME_PORT=9222  # External port mapped in Docker
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
  --sh-exec "ncat localhost $INTERNAL_PORT" \
  -l "$CHROME_PORT" \
  --keep-open & pid2=$!

./novnc_startup.sh >&2

python http_server.py >&2 &

STREAMLIT_SERVER_PORT=8501 python -m streamlit run computer_use_demo/streamlit.py >&2
EOL

WORKDIR /home/computeruse
ENTRYPOINT ["./entrypoint.sh"]
