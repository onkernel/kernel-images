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

# Remove upower to prevent spurious D-Bus activations and logs
RUN apt-get -yqq purge upower || true && rm -rf /var/lib/apt/lists/*

# Create a non-root user with a home directory
RUN useradd -m -s /bin/bash kernel

COPY ./xvfb_startup.sh /usr/bin/xvfb_startup.sh

# Wrapper script set environment
COPY ./wrapper.sh /usr/bin/wrapper.sh
