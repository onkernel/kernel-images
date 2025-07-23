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

echo -e "RSS\tCPU Time\tTX Bytes\tNConns\tNReqs\tNQueued\tNTotal"
while true; do
    metrics=$(curl -s -H "Authorization: Bearer $UKC_TOKEN" "$UKC_METRO/instances/$instance_id/metrics")
    rss=$(echo "$metrics" | grep 'instance_rss_bytes{instance_uuid=' | cut -d' ' -f2)
    cpu_time=$(echo "$metrics" | grep 'instance_cpu_time_s{instance_uuid=' | cut -d' ' -f2)
    tx_bytes=$(echo "$metrics" | grep 'instance_tx_bytes{instance_uuid=' | cut -d' ' -f2)
    nconns=$(echo "$metrics" | grep 'instance_nconns{instance_uuid=' | cut -d' ' -f2)
    nreqs=$(echo "$metrics" | grep 'instance_nreqs{instance_uuid=' | cut -d' ' -f2)
    nqueued=$(echo "$metrics" | grep 'instance_nqueued{instance_uuid=' | cut -d' ' -f2)
    ntotal=$(echo "$metrics" | grep 'instance_ntotal{instance_uuid=' | cut -d' ' -f2)
    echo -e "$rss\t$cpu_time\t$tx_bytes\t$nconns\t$nreqs\t$nqueued\t$ntotal"
    sleep 1
done
