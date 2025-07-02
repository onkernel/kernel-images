#!/usr/bin/env bash

set -euo pipefail
echo "$@"

sleep_pid=""

cleanup_and_exit() {
  # Force-kill the background sleep instantly (signal 9) if it exists.
  if [[ -n "$sleep_pid" ]]; then
    kill -9 "$sleep_pid" 2>/dev/null || true
  fi
  exit "${MOCK_FFMPEG_EXIT_CODE:-101}"
}

# Gracefully stop when recorder sends SIGINT or SIGTERM.
trap cleanup_and_exit INT TERM

# Keep the process alive until a signal is delivered. Store PID for cleanup.
sleep "${MOCK_FFMPEG_SLEEP_SECONDS:-600}" &
sleep_pid=$!

# Wait on the background job (it will already be gone after SIGKILL).
wait "$sleep_pid" 2>/dev/null || true
