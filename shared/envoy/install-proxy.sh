#!/bin/bash
set -eux

# Install Envoy proxy (official apt.envoyproxy.io)
ENVOY_PACKAGE="${ENVOY_PACKAGE:-envoy-1.32}"

echo "Installing Envoy proxy package: ${ENVOY_PACKAGE}"
mkdir -p /etc/apt/keyrings
curl -fsSL https://apt.envoyproxy.io/signing.key | gpg --dearmor -o /etc/apt/keyrings/envoy-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/envoy-keyring.gpg] https://apt.envoyproxy.io jammy main" > /etc/apt/sources.list.d/envoy.list
apt-get update
apt-get install -y --no-install-recommends "${ENVOY_PACKAGE}" || (apt-cache policy "${ENVOY_PACKAGE}" envoy && exit 1)
apt-mark hold "${ENVOY_PACKAGE}"
apt-get clean -y
rm -rf /var/lib/apt/lists/* /var/cache/apt/

# Create directory structure for Envoy configuration
mkdir -p /etc/envoy/templates

# Download and extract BrightData proxy certificate
echo "Downloading and extracting BrightData certificates"
mkdir -p /etc/envoy/brightdata
curl -fsSL https://brightdata.com/static/brightdata_proxy_ca.zip -o /tmp/brightdata_proxy_ca.zip
unzip -j /tmp/brightdata_proxy_ca.zip '*/*.crt' -d /etc/envoy/brightdata/ || true
rm /tmp/brightdata_proxy_ca.zip
echo "BrightData certificates extracted to /etc/envoy/brightdata/"

# List extracted certificates for verification
ls -la /etc/envoy/brightdata/
