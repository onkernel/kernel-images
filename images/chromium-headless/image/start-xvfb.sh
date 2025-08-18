#!/bin/bash

set -o pipefail -o errexit -o nounset

DISPLAY="${DISPLAY:-:1}"
WIDTH="${WIDTH:-1024}"
HEIGHT="${HEIGHT:-768}"
DPI="${DPI:-96}"

echo "Starting Xvfb on ${DISPLAY} with ${WIDTH}x${HEIGHT}x24, DPI ${DPI}"

exec Xvfb "$DISPLAY" -ac -screen 0 "${WIDTH}x${HEIGHT}x24" -retro -dpi "$DPI" -nolisten tcp -nolisten unix
