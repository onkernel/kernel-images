#!/bin/bash

set -o pipefail -o errexit -o nounset

echo "[envoy-init] Preparing Envoy bootstrap configuration"
mkdir -p /etc/envoy

render_from_template=false
if [[ -f /etc/envoy/templates/bootstrap.yaml && -n "${INST_NAME:-}" && -n "${METRO_NAME:-}" ]]; then
  render_from_template=true
fi

if $render_from_template; then
  inst_esc=$(printf '%s' "$INST_NAME" | sed -e 's/[\/&]/\\&/g')
  metro_esc=$(printf '%s' "$METRO_NAME" | sed -e 's/[\/&]/\\&/g')
  sed -e "s|{INSTANCE_NAME}|$inst_esc|g" \
      -e "s|{METRO_NAME}|$metro_esc|g" \
      /etc/envoy/templates/bootstrap.yaml > /etc/envoy/bootstrap.yaml
else
  cp -f /etc/envoy/default.yaml /etc/envoy/bootstrap.yaml
fi

echo "[envoy-init] Starting Envoy via supervisord"
supervisorctl -c /etc/supervisor/supervisord.conf start envoy
echo "[envoy-init] Waiting for Envoy admin on 127.0.0.1:9901..."
for i in {1..50}; do
  if (echo >/dev/tcp/127.0.0.1/9901) >/dev/null 2>&1; then
    echo "[envoy-init] Envoy is started"
    break
  fi
  sleep 0.1
  if [[ $i -eq 50 ]]; then
    echo "[envoy-init] Failed to start Envoy - admin interface not responding after 5 seconds"
  fi
done