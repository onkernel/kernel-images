#!/usr/bin/env bash

source common.sh
name=chromium-headful-test

  CHROMIUM_FLAGS="--accept-lang=en-US,en \
    --allow-pre-commit-input \
    --blink-settings=primaryHoverType=2,availableHoverTypes=2,primaryPointerType=4,availablePointerTypes=4 \
    --crash-dumps-dir=/tmp/chromium-dumps \
    --disable-back-forward-cache \
    --disable-background-networking \
    --disable-background-timer-throttling \
    --disable-backgrounding-occluded-windows \
    --disable-blink-features=AutomationControlled \
    --disable-breakpad \
    --disable-client-side-phishing-detection \
    --disable-component-extensions-with-background-pages \
    --disable-component-update \
    --disable-crash-reporter \
    --disable-crashpad \
    --disable-default-apps \
    --disable-dev-shm-usage \
    --disable-extensions \
    --disable-features=AcceptCHFrame,AutoExpandDetailsElement,AvoidUnnecessaryBeforeUnloadCheckSync,CertificateTransparencyComponentUpdater,DeferRendererTasksAfterInput,DestroyProfileOnBrowserClose,DialMediaRouteProvider,ExtensionManifestV2Disabled,GlobalMediaControls,HttpsUpgrades,ImprovedCookieControls,LazyFrameLoading,LensOverlay,MediaRouter,PaintHolding,ThirdPartyStoragePartitioning,Translate \
    --disable-field-trial-config \
    --disable-gcm-registration \
    --disable-gpu \
    --disable-gpu-compositing \
    --disable-hang-monitor \
    --disable-ipc-flooding-protection \
    --disable-notifications \
    --disable-popup-blocking \
    --disable-prompt-on-repost \
    --disable-renderer-backgrounding \
    --disable-search-engine-choice-screen \
    --disable-software-rasterizer \
    --enable-automation \
    --enable-use-zoom-for-dsf=false \
    --export-tagged-pdf \
    --force-color-profile=srgb \
    --hide-scrollbars \
    --metrics-recording-only \
    --mute-audio \
    --no-default-browser-check \
    --no-first-run \
    --no-sandbox \
    --no-zygote \
    --no-service-autorun \
    --no-startup-window \
    --password-store=basic \
    --remote-allow-origins=* \
    --start-maximized \
    --unsafely-disable-devtools-self-xss-warnings \
    --use-angle \
    --use-gl=disabled \
    --use-mock-keychain"

#   -e CHROMIUM_FLAGS="--user-data-dir=/home/kernel/user-data --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --disable-breakpad --crash-dumps-dir=/tmp --no-sandbox --no-zygote"


deploy_args=(
  -M 8192
  -p 9222:9222/tls
  -p 8080:8080/tls
  -e DISPLAY_NUM=1
  -e HEIGHT=768
  -e WIDTH=1024
  -e HOME=/
  -e CHROMIUM_FLAGS="$CHROMIUM_FLAGS"
  -n "$name"
)

if [[ "${WITH_KERNEL_IMAGES_API:-}" == "true" ]]; then
  deploy_args+=( -p 444:10001/tls )
  deploy_args+=( -e WITH_KERNEL_IMAGES_API=true )
fi

kraft cloud inst rm $name || true

if [[ "${ENABLE_WEBRTC:-}" == "true" ]]; then
  echo "Deploying with WebRTC enabled"
  kraft cloud inst create --start \
    "${deploy_args[@]}" \
    -p 443:8080/http+tls \
    -e ENABLE_WEBRTC=true \
    -e NEKO_ICESERVERS="${NEKO_ICESERVERS:-}" "$IMAGE"
else
  echo "Deploying without WebRTC"
  kraft cloud inst create --start \
    "${deploy_args[@]}" \
    -p 443:6080/http+tls \
    "$IMAGE"
fi
