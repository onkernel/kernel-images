#!/bin/bash

set -o pipefail -o errexit -o nounset

echo "Starting Chromium launcher (headless)"

# Resolve internal port for the remote debugging interface
INTERNAL_PORT="${INTERNAL_PORT:-9223}"

# Load flags from env (base) and optional runtime overlay file
BASE_FLAGS="${CHROMIUM_FLAGS:-}"
RUNTIME_FLAGS=""
if [[ -f /chromium/flags ]]; then
  RUNTIME_FLAGS="$(cat /chromium/flags)"
fi

# Parse and merge flags, paying careful attention to extension-related flags.
# Rules:
# - Base extension directives always carry over and are merged (union) with runtime values.
# - Runtime still has precedence when it explicitly disables all extensions via --disable-extensions.
# - We preserve other non-extension flags as-is (base + runtime), with simple dedupe.

# Tokenize inputs into arrays so we can look ahead for flags that use space-separated values.
read -r -a BASE_TOKENS <<< "${BASE_FLAGS}"
read -r -a RUNTIME_TOKENS <<< "${RUNTIME_FLAGS}"

# Buckets for non-extension flags
BASE_NONEXT=()
RUNTIME_NONEXT=()

# Buckets for extension lists
BASE_LOAD_EXT=()
BASE_DISABLE_EXCEPT=()
RT_LOAD_EXT=()
RT_DISABLE_EXCEPT=()

# Track if runtime explicitly disables all extensions
RT_DISABLE_ALL_EXTENSIONS=""

# Helper: append comma-separated list into an array (splits on ',')
append_csv_into_array() {
  local csv="$1"
  local -n arr_ref=$2
  local IFS=','
  read -r -a _parts <<< "$csv"
  for _p in "${_parts[@]}"; do
    if [[ -n "$_p" ]]; then
      arr_ref+=("$_p")
    fi
  done
}

# Parse a token stream, extracting extension directives and preserving others
parse_tokens() {
  local -n TOKENS=$1
  local who="$2" # "base" or "runtime"
  local -n OUT_NONEXT=$3

  local i=0
  while (( i < ${#TOKENS[@]} )); do
    local tok="${TOKENS[i]}"
    case "$tok" in
      --load-extension=*)
        local val="${tok#--load-extension=}"
        if [[ "$who" == "base" ]]; then
          append_csv_into_array "$val" BASE_LOAD_EXT
        else
          append_csv_into_array "$val" RT_LOAD_EXT
        fi
        ;;
      --load-extension)
        local next_val=""
        if (( i + 1 < ${#TOKENS[@]} )); then
          next_val="${TOKENS[i+1]}"
          i=$((i+1))
        fi
        if [[ -n "$next_val" ]]; then
          if [[ "$who" == "base" ]]; then
            append_csv_into_array "$next_val" BASE_LOAD_EXT
          else
            append_csv_into_array "$next_val" RT_LOAD_EXT
          fi
        fi
        ;;
      --disable-extensions-except=*)
        local val="${tok#--disable-extensions-except=}"
        if [[ "$who" == "base" ]]; then
          append_csv_into_array "$val" BASE_DISABLE_EXCEPT
        else
          append_csv_into_array "$val" RT_DISABLE_EXCEPT
        fi
        ;;
      --disable-extensions-except)
        local next_val=""
        if (( i + 1 < ${#TOKENS[@]} )); then
          next_val="${TOKENS[i+1]}"
          i=$((i+1))
        fi
        if [[ -n "$next_val" ]]; then
          if [[ "$who" == "base" ]]; then
            append_csv_into_array "$next_val" BASE_DISABLE_EXCEPT
          else
            append_csv_into_array "$next_val" RT_DISABLE_EXCEPT
          fi
        fi
        ;;
      --disable-extensions|--disable-extensions=*)
        if [[ "$who" == "runtime" ]]; then
          RT_DISABLE_ALL_EXTENSIONS="$tok"
        fi
        ;;
      *)
        OUT_NONEXT+=("$tok")
        ;;
    esac
    i=$((i+1))
  done
}

parse_tokens BASE_TOKENS base BASE_NONEXT
parse_tokens RUNTIME_TOKENS runtime RUNTIME_NONEXT

# Merge helper: take base + runtime arrays and produce deduped merged union
merge_lists_union() {
  local -n base_arr=$1
  local -n rt_arr=$2
  local -n out_arr=$3
  declare -A seen=()
  local tmp=()
  for v in "${base_arr[@]}" "${rt_arr[@]}"; do
    if [[ -n "$v" && -z "${seen[$v]:-}" ]]; then
      tmp+=("$v")
      seen[$v]=1
    fi
  done
  out_arr=("${tmp[@]}")
}

MERGED_LOAD=()
MERGED_EXCEPT=()
merge_lists_union BASE_LOAD_EXT RT_LOAD_EXT MERGED_LOAD
merge_lists_union BASE_DISABLE_EXCEPT RT_DISABLE_EXCEPT MERGED_EXCEPT

# Reconstruct extension flags
EXT_FLAGS=()
if [[ -n "$RT_DISABLE_ALL_EXTENSIONS" ]]; then
  EXT_FLAGS+=("$RT_DISABLE_ALL_EXTENSIONS")
else
  if (( ${#MERGED_LOAD[@]} > 0 )); then
    merged=""
    for v in "${MERGED_LOAD[@]}"; do
      if [[ -z "$merged" ]]; then merged="$v"; else merged+=",$v"; fi
    done
    if [[ -n "$merged" ]]; then
      EXT_FLAGS+=("--load-extension=$merged")
    fi
  fi
  if (( ${#MERGED_EXCEPT[@]} > 0 )); then
    merged=""
    for v in "${MERGED_EXCEPT[@]}"; do
      if [[ -z "$merged" ]]; then merged="$v"; else merged+=",$v"; fi
    done
    if [[ -n "$merged" ]]; then
      EXT_FLAGS+=("--disable-extensions-except=$merged")
    fi
  fi
fi

# Combine: base non-extension flags + runtime non-extension flags + reconstructed extension flags
COMBINED=(
  "${BASE_NONEXT[@]}"
  "${RUNTIME_NONEXT[@]}"
  "${EXT_FLAGS[@]}"
)

declare -A SEEN
DEDUP=()
for tok in "${COMBINED[@]}"; do
  if [[ -z "${SEEN[$tok]:-}" && -n "$tok" ]]; then
    DEDUP+=("$tok")
    SEEN[$tok]=1
  fi
done

FINAL_FLAGS="${DEDUP[*]}"

echo "BASE_FLAGS: $BASE_FLAGS"
echo "RUNTIME_FLAGS: $RUNTIME_FLAGS"
echo "FINAL_FLAGS: $FINAL_FLAGS"

# Always use display :1 and point DBus to the system bus socket
export DISPLAY=":1"
export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/dbus/system_bus_socket"

RUN_AS_ROOT="${RUN_AS_ROOT:-false}"

if [[ "$RUN_AS_ROOT" == "true" ]]; then
  exec chromium \
    --headless=new \
    --remote-debugging-port="$INTERNAL_PORT" \
    --remote-allow-origins=* \
    --user-data-dir=/home/kernel/user-data \
    --password-store=basic \
    --no-first-run \
    ${FINAL_FLAGS:-}
else
  echo "Running chromium as kernel user"
  exec runuser -u kernel -- env \
    DISPLAY=":1" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=/run/dbus/system_bus_socket" \
    XDG_CONFIG_HOME=/home/kernel/.config \
    XDG_CACHE_HOME=/home/kernel/.cache \
    HOME=/home/kernel \
    chromium \
    --headless=new \
    --remote-debugging-port="$INTERNAL_PORT" \
    --remote-allow-origins=* \
    --user-data-dir=/home/kernel/user-data \
    --password-store=basic \
    --no-first-run \
    ${FINAL_FLAGS:-}
fi
