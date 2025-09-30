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

echo -e "Timestamp\tRSS (MB)\tCPU%\tTX Bytes (MB)\tKB/s\tNConns\tNReqs\tNQueued\tNTotal"

# Initialize previous values for calculations
prev_cpu_time=""
prev_tx_bytes=""

while true; do
    # Get current timestamp with millisecond resolution
    timestamp=$(date '+%Y-%m-%d %H:%M:%S.%3N')
    
    metrics=$(curl -s -H "Authorization: Bearer $UKC_TOKEN" "$UKC_METRO/instances/$instance_id/metrics")
    rss_bytes=$(echo "$metrics" | grep 'instance_rss_bytes{instance_uuid=' | cut -d' ' -f2)
    rss=$(echo "scale=2; $rss_bytes / 1048576" | bc)
    cpu_time=$(echo "$metrics" | grep 'instance_cpu_time_s{instance_uuid=' | cut -d' ' -f2)
    tx_bytes_raw=$(echo "$metrics" | grep 'instance_tx_bytes{instance_uuid=' | cut -d' ' -f2)
    tx_bytes=$(echo "scale=2; $tx_bytes_raw / 1048576" | bc)
    nconns=$(echo "$metrics" | grep 'instance_nconns{instance_uuid=' | cut -d' ' -f2)
    nreqs=$(echo "$metrics" | grep 'instance_nreqs{instance_uuid=' | cut -d' ' -f2)
    nqueued=$(echo "$metrics" | grep 'instance_nqueued{instance_uuid=' | cut -d' ' -f2)
    ntotal=$(echo "$metrics" | grep 'instance_ntotal{instance_uuid=' | cut -d' ' -f2)
    
    # Calculate CPU percentage (ensure it's >= 0)
    if [ -n "$prev_cpu_time" ] && [ -n "$cpu_time" ]; then
        cpu_diff=$(echo "scale=6; $cpu_time - $prev_cpu_time" | bc)
        cpu_percent=$(echo "scale=2; $cpu_diff * 100" | bc)
        # Ensure CPU percentage is not negative
        if (( $(echo "$cpu_percent < 0" | bc -l) )); then
            cpu_percent="0.00"
        fi
        # Format to exactly 2 decimal places
        cpu_percent=$(printf "%.2f" "$cpu_percent")
    else
        cpu_percent="0.00"
    fi
    
    # Calculate network speed in KB/s
    if [ -n "$prev_tx_bytes" ] && [ -n "$tx_bytes_raw" ]; then
        tx_diff=$(echo "scale=6; $tx_bytes_raw - $prev_tx_bytes" | bc)
        tx_kbps=$(echo "scale=2; $tx_diff / 1024" | bc)
        # Ensure network speed is not negative
        if (( $(echo "$tx_kbps < 0" | bc -l) )); then
            tx_kbps="0.00"
        fi
    else
        tx_kbps="0.00"
    fi
    
    echo -e "$timestamp\t${rss}MB\t${cpu_percent}%\t${tx_bytes}MB\t${tx_kbps}KB/s\t$nconns\t$nreqs\t$nqueued\t$ntotal"
    
    # Store current values for next iteration
    prev_cpu_time="$cpu_time"
    prev_tx_bytes="$tx_bytes_raw"
    sleep 1
done
