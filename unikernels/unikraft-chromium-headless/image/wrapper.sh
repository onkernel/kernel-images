#!/bin/sh

set -e

export HOME=/root
export DEBUG=pw:*
cd /app
exec $@
