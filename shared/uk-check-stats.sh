#!/usr/bin/env bash
set -e -o pipefail

# fail if IMAGE, UKC_TOKEN, UKC_METRO are not set
errormsg=""
for var in UKC_TOKEN UKC_METRO; do
    if [ -z "${!var}" ]; then
        errormsg+="$var "
    fi
done
if [ -n "$errormsg" ]; then
    echo "Required variables not set: $errormsg"
    exit 1
fi

# get instance ID from arg
instance_id=$1
if [ -z "$instance_id" ]; then
    echo "Instance ID not provided"
    exit 1
fi

# get instance stats in a loop until ctrl-c
trap 'echo "Stopping stats collection..."; exit 0' INT

while true; do
    metrics=$(curl -s -H "Authorization: Bearer $UKC_TOKEN" "$UKC_METRO/instances/$instance_id/metrics")
    rss=$(echo "$metrics" | grep 'instance_rss_bytes{instance_uuid=' | cut -d' ' -f2)
    cpu_time=$(echo "$metrics" | grep 'instance_cpu_time_s{instance_uuid=' | cut -d' ' -f2)
    tx_bytes=$(echo "$metrics" | grep 'instance_tx_bytes{instance_uuid=' | cut -d' ' -f2)
    echo "RSS: $rss"
    echo "CPU Time: $cpu_time"
    echo "TX Bytes: $tx_bytes"
    sleep 1
done
