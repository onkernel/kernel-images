#!/usr/bin/env bash
# ------------------------------------------------------------------------------
# wrapper.sh – container entrypoint
# ------------------------------------------------------------------------------

set -o errexit -o nounset -o pipefail

# ------------------------------------------------------------------------------
# Environment ──────────────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
export PULSE_SERVER="/tmp/runtime-kernel/pulse/native"
export XDG_CONFIG_HOME="/tmp/.chromium"
export XDG_CACHE_HOME="/tmp/.chromium"
export XDG_RUNTIME_DIR="/tmp/runtime-kernel"
export DISPLAY=":1"

# ------------------------------------------------------------------------------
# /dev/shm (only outside Docker) ───────────────────────────────────────────────
# If the WITHDOCKER environment variable is not set, it means we are not running inside a Docker container.
# Docker manages /dev/shm itself, and attempting to mount or modify it can cause permission or device errors.
# However, in a unikernel container environment (non-Docker), we need to manually create and mount /dev/shm as a tmpfs
# to support shared memory operations.
# ------------------------------------------------------------------------------
if [[ -z "${WITHDOCKER:-}" ]]; then
  mkdir -p /dev/shm
  chmod 777 /dev/shm
  mount -t tmpfs tmpfs /dev/shm
fi

# ------------------------------------------------------------------------------
# Scale-to-zero control ────────────────────────────────────────────────────────
# We disable scale-to-zero for the lifetime of this script and restore
# the original setting on exit.
# ------------------------------------------------------------------------------
SCALE_TO_ZERO_FILE="/uk/libukp/scale_to_zero_disable"

scale_to_zero_write() {
  # Skip when not running inside Unikraft Cloud (control file absent)
  [[ -e "$SCALE_TO_ZERO_FILE" ]] || return 0
  # Write the character, but do not fail the whole script if this errors out
  echo -n "$1" >"$SCALE_TO_ZERO_FILE" 2>/dev/null || \
    echo "[wrapper] WARN: cannot write scale-to-zero flag" >&2
}

disable_scale_to_zero() { scale_to_zero_write "+"; }
enable_scale_to_zero()  { scale_to_zero_write "-"; }

# Disable scale-to-zero for the duration of the script when not running under Docker
if [[ -z "${WITHDOCKER:-}" ]]; then
  echo "[wrapper] Disabling scale-to-zero"
  disable_scale_to_zero
fi

# ------------------------------------------------------------------------------
# Xorg ─────────────────────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
/usr/bin/Xorg :1 -config /etc/neko/xorg.conf -noreset -nolisten tcp &

# ------------------------------------------------------------------------------
# Input-socket permission fix : Force-change after creation ────────────────────
# ------------------------------------------------------------------------------
echo "[wrapper] Waiting for xf86-input socket"
for i in $(seq 1 10); do
  if [[ -S /tmp/xf86-input-neko.sock ]]; then
    chmod 666 /tmp/xf86-input-neko.sock
    echo "[wrapper] Socket chmod 666 applied"
    break
  fi
  sleep 0.5
done
[[ -S /tmp/xf86-input-neko.sock ]] || echo "[wrapper] WARN: socket not found" >&2

# ------------------------------------------------------------------------------
# Mutter ───────────────────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
./mutter_startup.sh

# ------------------------------------------------------------------------------
# system D-Bus Setup ───────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
echo "[wrapper] Starting system D-Bus"
mkdir -p /run/dbus
dbus-uuidgen --ensure
dbus-daemon --system \
  --address="unix:path=/run/dbus/system_bus_socket" \
  --nopidfile --nosyslog --nofork &
dbus_pid=$!

echo "[wrapper] Waiting for D-Bus socket"
for i in $(seq 1 20); do
  [[ -S /run/dbus/system_bus_socket ]] && break
  sleep 0.5
done
export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/dbus/system_bus_socket"

# ------------------------------------------------------------------------------
# PulseAudio Setup ─────────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
echo "[pulse] Setting up permissions"
chown -R kernel:kernel /home/kernel/ /home/kernel/.config /etc/pulse
chmod 777 /home/kernel/.config /etc/pulse
chown -R kernel:kernel /tmp/runtime-kernel

echo "[pulse] Starting daemon"
runuser -u kernel -- env \
  XDG_RUNTIME_DIR=/tmp/runtime-kernel \
  XDG_CONFIG_HOME=/home/kernel/.config \
  XDG_CACHE_HOME=/home/kernel/.cache \
  pulseaudio --log-level=error \
             --disallow-module-loading \
             --disallow-exit \
             --exit-idle-time=-1 &
pulse_pid=$!

echo "[pulse] Waiting for server"
for i in $(seq 1 20); do
  runuser -u kernel -- pactl info >/dev/null 2>&1 && break
  if [ "$i" -eq 20 ]; then
    echo "[pulse] ERROR: failed to start"
    exit 1
  fi
  sleep 0.5
done

# ------------------------------------------------------------------------------
# VNC / WebRTC ─────────────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
if [[ "${ENABLE_WEBRTC:-}" != "true" ]]; then
  ./x11vnc_startup.sh
fi

# ------------------------------------------------------------------------------
# Filesystem housekeeping for kernel user ──────────────────────────────────────
# Some Chromium subsystems want to create files under $HOME (NSS cert DB, dconf
# cache).  If those directories are missing or owned by root Chromium emits
# noisy error messages such as:
#   [ERROR:crypto/nss_util.cc:48] Failed to create /home/kernel/.pki/nssdb ...
#   dconf-CRITICAL **: unable to create directory '/home/kernel/.cache/dconf'
# Pre-create them and hand ownership to the user so the messages disappear.
# Also critical to avoid Permission Denied when running as kernel user
# ------------------------------------------------------------------------------
dirs=(
  /home/kernel/user-data
  /home/kernel/.config/chromium
  /home/kernel/.pki/nssdb
  /home/kernel/.cache/dconf
  /tmp
  /var/log
  /tmp/runtime-kernel/dconf
  /tmp/.chromium
)
for d in "${dirs[@]}"; do
  [[ -d "$d" ]] || mkdir -p "$d"
done
chown -R kernel:kernel /home/kernel/user-data /home/kernel/.config \
  /home/kernel/.pki /home/kernel/.cache /tmp/runtime-kernel /tmp/.chromium 2>/dev/null || true

# ------------------------------------------------------------------------------
# Cleanup handler ──────────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
cleanup() {
  echo "[wrapper] Cleaning up"
  enable_scale_to_zero
  kill -TERM "$pid" "$pid2"
  [[ -n "${pid3:-}" ]] && kill -TERM "$pid3" || true
  [[ -n "${dbus_pid:-}" ]] && kill -TERM "$dbus_pid" || true
}
trap cleanup TERM INT
pid=
pid2=
pid3=
export INTERNAL_PORT=9223
export CHROME_PORT=9222  

# ------------------------------------------------------------------------------
# Chromium ─────────────────────────────────────────────────────────────────────
# Start Chromium with display :1 and remote debugging, loading our recorder extension.
# Use ncat to listen on 0.0.0.0:9222
# since chromium does not let you listen on 0.0.0.0 anymore :
# https://github.com/pyppeteer/pyppeteer/pull/379#issuecomment-217029626
# ------------------------------------------------------------------------------

# Load additional Chromium flags from /chromium/flags if present
echo "[chromium] Preparing flags"
CHROMIUM_FLAGS="${CHROMIUM_FLAGS:-}"
[[ -f /chromium/flags ]] && CHROMIUM_FLAGS+=" $(< /chromium/flags)"
echo "[chromium] Flags: $CHROMIUM_FLAGS"

INTERNAL_PORT="${INTERNAL_PORT:-9223}"
CHROME_PORT="${CHROME_PORT:-9222}" # External port mapped to host
RUN_AS_ROOT="${RUN_AS_ROOT:-false}"

echo "[chromium] Launching on display ${DISPLAY}"
if [[ "$RUN_AS_ROOT" == "true" ]]; then
  DISPLAY="$DISPLAY" DBUS_SESSION_BUS_ADDRESS="$DBUS_SESSION_BUS_ADDRESS" chromium \
    --remote-debugging-port="$INTERNAL_PORT" \
    $CHROMIUM_FLAGS >&2 & pid=$!
else
  # required environment variables (DISPLAY, DBUS_SESSION_BUS_ADDRESS) are already exported
  # in this script's environment, so runuser will pass them to the child process.
  runuser -u kernel -- env \
    XDG_CONFIG_HOME=/tmp/.chromium \
    XDG_CACHE_HOME=/tmp/.chromium \
    HOME=/home/kernel \
    chromium --remote-debugging-port="$INTERNAL_PORT" $CHROMIUM_FLAGS \
    > /tmp/chromium.log 2>&1 & pid=$!
fi

# ncat proxy → expose INTERNAL_PORT as CHROME_PORT
echo "[chromium] Starting ncat proxy :$CHROME_PORT → :$INTERNAL_PORT"
ncat --sh-exec "ncat 0.0.0.0 $INTERNAL_PORT" \
     -l "$CHROME_PORT" --keep-open & pid2=$!

# ------------------------------------------------------------------------------
# Start Neko WebRTC / noVNC ────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
if [[ "${ENABLE_WEBRTC:-}" == "true" ]]; then
  echo "[neko] Starting WebRTC server"
  runuser -u kernel -- /usr/bin/neko serve \
    --server.static /var/www \
    --server.bind 0.0.0.0:8080 >/dev/null 2>&1 &
  neko_pid=$!

  echo "[neko] Waiting on port 8080"
  until nc -z 127.0.0.1 8080; do sleep 0.5; done
else
  ./novnc_startup.sh
  echo "[novnc] Ready"
fi

# ------------------------------------------------------------------------------
# Start kernel-images API ──────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  echo "[kernel-images:api] Starting service"

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

  # close the "--no-sandbox unsupported flag" warning when running as root
  # in the unikernel runtime we haven't been able to get chromium to launch as non-root
  # without cryptic crashpad errors
  # and when running as root you must use the --no-sandbox flag,
  # which generates a warning
  if [[ "${RUN_AS_ROOT:-}" == "true" ]]; then
    echo "Running as root, attempting to dismiss the --no-sandbox unsupported flag warning"
    if read -r WIDTH HEIGHT <<< "$(xdotool getdisplaygeometry 2>/dev/null)"; then
      # Work out an x-coordinate slightly inside the right-hand edge of the
      OFFSET_X=$(( WIDTH - 30 ))
      if (( OFFSET_X < 0 )); then
        OFFSET_X=0
      fi

      # Wait for kernel-images API port 10001 to be ready.
      echo "Waiting for kernel-images API port 127.0.0.1:10001..."
      while ! nc -z 127.0.0.1 10001 2>/dev/null; do
        sleep 0.5
      done
      echo "Port 10001 is open"

      # Wait for Chromium window to open before dismissing the --no-sandbox warning.
      target='New Tab - Chromium'
      echo "Waiting for Chromium window \"${target}\" to appear and become active..."
      while :; do
        win_id=$(xwininfo -root -tree 2>/dev/null | awk -v t="$target" '$0 ~ t {print $1; exit}')
        if [[ -n $win_id ]]; then
          win_id=${win_id%:}
          if xdotool windowactivate --sync "$win_id"; then
            echo "Focused window $win_id ($target) on $DISPLAY"
            break
          fi
        fi
        sleep 0.5
      done

      # wait... not sure but this just increases the likelihood of success
      # without the sleep you often open the live view and see the mouse hovering over the "X" to dismiss the warning, suggesting that it clicked before the warning or chromium appeared
      sleep 5

      # Attempt to click the warning's close button
      echo "Clicking the warning's close button at x=$OFFSET_X y=115"
      if curl -s -o /dev/null -X POST \
        http://localhost:10001/computer/click_mouse \
        -H "Content-Type: application/json" \
        -d "{\"x\":${OFFSET_X},\"y\":115}"; then
          echo "Successfully clicked the warning's close button"
      else
        echo "Failed to click the warning's close button" >&2
      fi
    else
      echo "xdotool failed to obtain display geometry; skipping sandbox warning dismissal." >&2
    fi
  fi
elif [[ "${WITH_KERNEL_OPERATOR_API:-}" == "true" ]]; then
# ------------------------------------------------------------------------------
# Start kernel-operator API (runs as user: kernel, with elevated caps; sudo available)
# ------------------------------------------------------------------------------
  echo "[kernel-operator:api] Starting service"

  # Start the kernel-operator API in the background
  (
    OP_ENV_FILE="/tmp/kernel-operator/.kernel-operator.env"
    [[ -f "$OP_ENV_FILE" ]] && { set -a; source "$OP_ENV_FILE"; set +a; }

    # Maximize file descriptor and process limits for heavy FS/exec workloads
    ulimit -n "${KERNEL_OPERATOR_ULIMIT_NOFILE:-1048576}" || true
    ulimit -u "${KERNEL_OPERATOR_ULIMIT_NPROC:-65535}"   || true
    umask "${KERNEL_OPERATOR_UMASK:-0002}"
    # When the binary shells out to privileged commands, it can use:
    #   sudo -n <cmd>        (passwordless per Dockerfile sudoers)
    # Capabilities on the binary already cover many privileged syscalls.
    
    # Debug log to print parsed .env content
    echo "[wrapper:kernel-operator:api] parsed operator .env content:"
    grep -v "^#" /tmp/kernel-operator/.kernel-operator.env | while read -r line; do
      echo "  $line"
    done
    
    # Run the operator API with the parsed environment variables
    grep -v "^#" /tmp/kernel-operator/.kernel-operator.env | xargs -I{} /usr/local/bin/kernel-operator-api {} & pid4=$!

    # close the "--no-sandbox unsupported flag" warning when running as root
    # in the unikernel runtime we haven't been able to get chromium to launch as non-root
    # without cryptic crashpad errors
    # and when running as root you must use the --no-sandbox flag,
    # which generates a warning
    if [[ "${RUN_AS_ROOT:-}" == "true" ]]; then
      echo "Running as root, attempting to dismiss the --no-sandbox unsupported flag warning"
      if read -r WIDTH HEIGHT <<< "$(xdotool getdisplaygeometry 2>/dev/null)"; then
        # Work out an x-coordinate slightly inside the right-hand edge of the
        OFFSET_X=$(( WIDTH - 30 ))
        if (( OFFSET_X < 0 )); then
          OFFSET_X=0
        fi

        # Wait for kernel-images API port 10001 to be ready.
        echo "Waiting for kernel-images API port 127.0.0.1:10001..."
        while ! nc -z 127.0.0.1 10001 2>/dev/null; do
          sleep 0.5
        done
        echo "Port 10001 is open"

        # Wait for Chromium window to open before dismissing the --no-sandbox warning.
        target='New Tab - Chromium'
        echo "Waiting for Chromium window \"${target}\" to appear and become active..."
        while :; do
          win_id=$(xwininfo -root -tree 2>/dev/null | awk -v t="$target" '$0 ~ t {print $1; exit}')
          if [[ -n $win_id ]]; then
            win_id=${win_id%:}
            if xdotool windowactivate --sync "$win_id"; then
              echo "Focused window $win_id ($target) on $DISPLAY"
              break
            fi
          fi
          sleep 0.5
        done

        # wait... not sure but this just increases the likelihood of success
        # without the sleep you often open the live view and see the mouse hovering over the "X" to dismiss the warning, suggesting that it clicked before the warning or chromium appeared
        sleep 5

        # Attempt to click the warning's close button
        echo "Clicking the warning's close button at x=$OFFSET_X y=115"
        if curl -s -o /dev/null -X POST \
          http://localhost:10001/computer/click_mouse \
          -H "Content-Type: application/json" \
          -d "{\"x\":${OFFSET_X},\"y\":115}"; then
            echo "Successfully clicked the warning's close button"
        else
          echo "Failed to click the warning's close button" >&2
        fi
      else
        echo "xdotool failed to obtain display geometry; skipping sandbox warning dismissal." >&2
      fi
    fi


    if [[ "${DEBUG_OPERATOR_TEST:-}" == "true" ]]; then
      echo "[kernel-operator:test] sleep 10 then Running tests once"
      sleep 10
      /usr/local/bin/kernel-operator-test --all > /tmp/kernel-operator/tests.log 2>&1 || echo "[kernel-operator:tests] Non-zero exit code"
    fi
    
    wait $pid4
  ) &
fi


# ------------------------------------------------------------------------------
# Scale-to-zero flag ───────────────────────────────────────────────────────────
# ------------------------------------------------------------------------------
if [[ -z "${WITHDOCKER:-}" ]]; then
  enable_scale_to_zero
fi

# ------------------------------------------------------------------------------
# Keep the container running ───────────────────────────────────────────────────
# ------------------------------------------------------------------------------
tail -f /dev/null
