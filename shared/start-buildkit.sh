#!/usr/bin/env bash

# This script MUST be sourced (i.e. use `source start-buildkit.sh`).

# Variable used by KraftKit, hence the requirement for sourcing the script
export KRAFTKIT_BUILDKIT_HOST=docker-container://buildkit

# Install container if not already installed.
docker container inspect buildkit > /dev/null 2>&1
if test $? -eq 0; then
    echo "Container 'buildkit' is already installed. Nothing to do."
else
    echo "Installing 'buildkit' container ... "
    docker run -d --name buildkit --privileged moby/buildkit:latest
    return $?
fi

test "$(docker container inspect -f '{{.State.Running}}' buildkit 2> /dev/null)" = "true"
if test $? -eq 0; then
    echo "Container 'buidlkitd' is already running. Nothing to do."
else
    echo "Starting 'buildkit' container ... "
    docker start buildkit
    return $?
fi