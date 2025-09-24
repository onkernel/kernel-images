#!/bin/bash

set -o pipefail -o errexit -o nounset

echo "[envoy-init] Preparing Envoy bootstrap configuration"
mkdir -p /etc/envoy

# Generate self-signed certificates for TLS forward proxy
echo "[envoy-init] Generating self-signed certificates for TLS forward proxy"
mkdir -p /etc/envoy/certs

if [[ ! -f /etc/envoy/certs/proxy.crt || ! -f /etc/envoy/certs/proxy.key ]]; then
  echo "[envoy-init] Creating new self-signed certificate"
  openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
    -keyout /etc/envoy/certs/proxy.key \
    -out /etc/envoy/certs/proxy.crt \
    -subj "/C=US/ST=CA/O=Kernel/CN=localhost" \
    -addext "subjectAltName = DNS:localhost,IP:127.0.0.1" \
    2>&1 | sed 's/^/[envoy-init] /'
  echo "[envoy-init] Certificate generated successfully"
  
  # Add certificate to system trust store for Chrome/Chromium
  echo "[envoy-init] Adding certificate to system trust store"
  mkdir -p /usr/local/share/ca-certificates
  cp /etc/envoy/certs/proxy.crt /usr/local/share/ca-certificates/kernel-envoy-proxy.crt
  update-ca-certificates 2>&1 | sed 's/^/[envoy-init] /'
  echo "[envoy-init] Certificate added to system trust store"
else
  echo "[envoy-init] Certificates already exist, skipping generation"
fi

render_from_template=false
if [[ -f /etc/envoy/templates/bootstrap.yaml && -n "${INST_NAME:-}" && -n "${METRO_NAME:-}" && -n "${XDS_SERVER:-}" && -n "${XDS_JWT:-}" ]]; then
  render_from_template=true
fi

if $render_from_template; then
  echo "[envoy-init] Rendering template with INST_NAME=${INST_NAME}, METRO_NAME=${METRO_NAME}, XDS_SERVER=${XDS_SERVER}, XDS_JWT=***"
  inst_esc=$(printf '%s' "$INST_NAME" | sed -e 's/[\/&]/\\&/g')
  metro_esc=$(printf '%s' "$METRO_NAME" | sed -e 's/[\/&]/\\&/g')
  xds_esc=$(printf '%s' "$XDS_SERVER" | sed -e 's/[\/&]/\\&/g')
  jwt_esc=$(printf '%s' "$XDS_JWT" | sed -e 's/[\/&]/\\&/g')
  sed -e "s|{INST_NAME}|$inst_esc|g" \
      -e "s|{METRO_NAME}|$metro_esc|g" \
      -e "s|{XDS_SERVER}|$xds_esc|g" \
      -e "s|{XDS_JWT}|$jwt_esc|g" \
      /etc/envoy/templates/bootstrap.yaml > /etc/envoy/bootstrap.yaml
else
  echo "[envoy-init] Using default configuration (template vars not provided: INST_NAME=${INST_NAME:-unset}, METRO_NAME=${METRO_NAME:-unset}, XDS_SERVER=${XDS_SERVER:-unset}, XDS_JWT)"
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