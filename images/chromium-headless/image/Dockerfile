FROM docker.io/golang:1.25.0 AS server-builder
WORKDIR /workspace/server

# Allow cross-compilation when building with BuildKit platforms
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

# Go module dependencies first for better layer caching
COPY server/go.mod ./
COPY server/go.sum ./
RUN go mod download

# Copy the rest of the server source and build the binary
COPY server/ .
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /out/kernel-images-api ./cmd/api

FROM docker.io/ubuntu:22.04

RUN set -xe; \
    apt-get -yqq update; \
    apt-get -yqq install \
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
    apt-get -yqq install \
    ca-certificates \
    curl \
    build-essential \
    libssl-dev \
    git \
    dbus \
    dbus-x11 \
    xvfb \
    x11-utils \
    software-properties-common \
    supervisor;

# install chromium and sqlite3 for debugging the cookies file
RUN add-apt-repository -y ppa:xtradeb/apps
RUN apt update -y && apt install -y chromium sqlite3

# Install FFmpeg (latest static build) for the recording server
RUN set -eux; \
    URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"; \
    echo "Downloading FFmpeg static build from $URL"; \
    curl -fsSL "$URL" -o /tmp/ffmpeg.tar.xz; \
    tar -xJf /tmp/ffmpeg.tar.xz -C /tmp; \
    install -m755 /tmp/ffmpeg-*/ffmpeg  /usr/local/bin/ffmpeg; \
    install -m755 /tmp/ffmpeg-*/ffprobe /usr/local/bin/ffprobe; \
    rm -rf /tmp/ffmpeg*

# Remove upower to prevent spurious D-Bus activations and logs
RUN apt-get -yqq purge upower || true && rm -rf /var/lib/apt/lists/*

# Create a non-root user with a home directory
RUN useradd -m -s /bin/bash kernel

# Xvfb helper and supervisor-managed start scripts
COPY images/chromium-headless/image/start-chromium.sh /images/chromium-headless/image/start-chromium.sh
COPY images/chromium-headless/image/start-xvfb.sh /images/chromium-headless/image/start-xvfb.sh
RUN chmod +x /images/chromium-headless/image/start-chromium.sh /images/chromium-headless/image/start-xvfb.sh

# Wrapper script set environment
COPY images/chromium-headless/image/wrapper.sh /usr/bin/wrapper.sh

# Supervisord configuration
COPY images/chromium-headless/image/supervisord.conf /etc/supervisor/supervisord.conf
COPY images/chromium-headless/image/supervisor/services/ /etc/supervisor/conf.d/services/

# Copy the kernel-images API binary built in the builder stage
COPY --from=server-builder /out/kernel-images-api /usr/local/bin/kernel-images-api
ENV WITH_KERNEL_IMAGES_API=false

ENTRYPOINT [ "/usr/bin/wrapper.sh" ]
