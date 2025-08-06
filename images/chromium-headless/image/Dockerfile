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
    software-properties-common;

RUN add-apt-repository -y ppa:xtradeb/apps
RUN apt update -y && apt install -y chromium ncat

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

COPY ./xvfb_startup.sh /usr/bin/xvfb_startup.sh

# Wrapper script set environment
COPY ./wrapper.sh /usr/bin/wrapper.sh

# Copy the kernel-images API binary built during the build process
COPY bin/kernel-images-api /usr/local/bin/kernel-images-api
ENV WITH_KERNEL_IMAGES_API=false
