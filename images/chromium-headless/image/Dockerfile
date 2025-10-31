FROM docker.io/golang:1.25.0 AS server-builder
WORKDIR /workspace/server

# Allow cross-compilation when building with BuildKit platforms
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

# Go module dependencies first for better layer caching
COPY server/go.mod ./
COPY server/go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY server/ .

# Build kernel-images API
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /out/kernel-images-api ./cmd/api

# Build chromium launcher
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /out/chromium-launcher ./cmd/chromium-launcher

FROM node:22-bullseye-slim AS node-22
FROM docker.io/ubuntu:22.04
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    rm -f /etc/apt/apt.conf.d/docker-clean; \
    echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache; \
    set -xe; \
    apt-get -yqq update; \
    apt-get -yqq --no-install-recommends install \
    libcups2 \
    libnss3 \
    libatk1.0-0 \
    libnspr4 \
    libpango1.0-0 \
    libasound2 \
    libatspi2.0-0 \
    libxdamage1 \
    libatk-bridge2.0-0 \
    libxkbcommon0 \
    libdrm2 \
    libxcomposite1 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libnss3; \
    apt-get -yqq --no-install-recommends install \
    ca-certificates \
    curl \
    build-essential \
    libssl-dev \
    git \
    gpg-agent \
    dbus \
    dbus-x11 \
    xvfb \
    x11-utils \
    xdotool \
    fontconfig \
    fonts-noto-cjk \
    fonts-noto-color-emoji \
    fonts-nanum \
    software-properties-common \
    supervisor; \
    fc-cache -f

# install chromium and sqlite3 for debugging the cookies file
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    add-apt-repository -y ppa:xtradeb/apps
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    apt-get update -y && \
    apt-get -y install chromium && \
    apt-get --no-install-recommends -y install sqlite3;

# Copy Chromium policy configuration
RUN mkdir -p /etc/chromium/policies/managed
COPY shared/chromium-policies/managed/policy.json /etc/chromium/policies/managed/policy.json

# Install FFmpeg (latest static build) for the recording server
RUN --mount=type=cache,target=/tmp/cache/ffmpeg,id=ffmpeg \
    <<-'EOT'
    set -eux
    URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"
    echo "Downloading FFmpeg MD5 checksum"
    if ! curl --connect-timeout 10 -fsSL "${URL}.md5" -o /tmp/cache/ffmpeg/ffmpeg.tar.xz.md5; then
        echo "Failed to connect to ffmpeg static build provider for MD5 checksum."
        echo "Checking for cached version to use."
        if [ ! -f /tmp/cache/ffmpeg/ffmpeg.tar.xz.md5 ]; then
            echo "Unable to locate cached MD5 checksum. Exiting."
            exit 1
        else
            echo "Found cached MD5 checksum."
        fi
    fi
    sed -i -e 's/ .*$/ \/tmp\/cache\/ffmpeg\/ffmpeg.tar.xz/' /tmp/cache/ffmpeg/ffmpeg.tar.xz.md5
    echo "Checking cache for FFmpeg archive and validating MD5 checksum"
    if md5sum --check /tmp/cache/ffmpeg/ffmpeg.tar.xz.md5; then
        echo "Checksum validated, using cached FFmpeg archive"
    else
        echo "Downloading FFmpeg static build from $URL"
        curl -fsSL "$URL" -o /tmp/cache/ffmpeg/ffmpeg.tar.xz
        echo "Validating MD5 checksum of FFmpeg static build download"
        md5sum --check /tmp/cache/ffmpeg/ffmpeg.tar.xz.md5
    fi
    tar -xJf /tmp/cache/ffmpeg/ffmpeg.tar.xz -C /tmp
    install -m755 /tmp/ffmpeg-*/ffmpeg  /usr/local/bin/ffmpeg
    install -m755 /tmp/ffmpeg-*/ffprobe /usr/local/bin/ffprobe
    rm -rf /tmp/ffmpeg*
EOT

# Remove upower to prevent spurious D-Bus activations and logs
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    apt-get -yqq purge upower || true

# install Node.js 22.x by copying from the node:22-bullseye-slim stage
COPY --from=node-22 /usr/local/bin/node /usr/local/bin/node
COPY --from=node-22 /usr/local/lib/node_modules /usr/local/lib/node_modules
# Recreate symlinks for npm/npx/corepack to point into node_modules
RUN set -eux; \
    ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm; \
    ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx; \
    if [ -e /usr/local/lib/node_modules/corepack/dist/corepack.js ]; then \
    ln -sf /usr/local/lib/node_modules/corepack/dist/corepack.js /usr/local/bin/corepack; \
    fi

# Install TypeScript and Playwright globally
RUN npm install -g typescript playwright-core tsx

ENV WITHDOCKER=true

# Create a non-root user with a home directory
RUN useradd -m -s /bin/bash kernel

# supervisor  start scripts
COPY images/chromium-headless/image/start-xvfb.sh /images/chromium-headless/image/start-xvfb.sh
RUN chmod +x /images/chromium-headless/image/start-xvfb.sh

# Wrapper script to set environment
COPY images/chromium-headless/image/wrapper.sh /usr/bin/wrapper.sh

# Supervisord configuration
COPY images/chromium-headless/image/supervisord.conf /etc/supervisor/supervisord.conf
COPY images/chromium-headless/image/supervisor/services/ /etc/supervisor/conf.d/services/

# Copy the kernel-images API binary built in the builder stage
COPY --from=server-builder /out/kernel-images-api /usr/local/bin/kernel-images-api
COPY --from=server-builder /out/chromium-launcher /usr/local/bin/chromium-launcher

# Copy the Playwright executor runtime
COPY server/runtime/playwright-executor.ts /usr/local/lib/playwright-executor.ts

ENTRYPOINT [ "/usr/bin/wrapper.sh" ]
