FROM docker.io/ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV container=docker

# Ensure Docker sends the correct stop signal understood by systemd
STOPSIGNAL SIGRTMIN+3

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
    gpg-agent \
    dirmngr;

RUN add-apt-repository -y ppa:xtradeb/apps
RUN apt update -y && apt install -y --no-install-recommends chromium ncat

# Install systemd and D-Bus for service management
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends systemd systemd-sysv dbus; \
    mkdir -p /var/log/journal; \
    # Trim services that don't make sense in a container
    rm -f /lib/systemd/system/multi-user.target.wants/* \
    /etc/systemd/system/*.wants/* \
    /lib/systemd/system/sysinit.target.wants/systemd-tmpfiles-setup* \
    /lib/systemd/system/sockets.target.wants/*udev*; \
    apt-get clean -y; \
    rm -rf /var/lib/apt/lists/* /var/cache/apt/

# Forward journald to the container console so /sbin/init shows all systemd-managed logs
RUN set -eux; \
    mkdir -p /etc/systemd/journald.conf.d; \
    printf '%s\n' \
    '[Journal]' \
    'ForwardToConsole=yes' \
    'TTYPath=/dev/console' \
    'MaxLevelConsole=debug' \
    > /etc/systemd/journald.conf.d/forward-to-console.conf

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

# Make wrapper executable and register a systemd service to run it as root
RUN set -eux; \
    chmod +x /usr/bin/wrapper.sh; \
    printf '%s\n' \
    '[Unit]' \
    'Description=Kernel wrapper supervisor' \
    'After=network.target' \
    '' \
    '[Service]' \
    'Type=simple' \
    'User=root' \
    'Group=root' \
    'PassEnvironment=DISPLAY_NUM HEIGHT WIDTH RUN_AS_ROOT WITH_KERNEL_IMAGES_API ENABLE_WEBRTC CHROMIUM_FLAGS' \
    'PassEnvironment=KERNEL_IMAGES_API_PORT KERNEL_IMAGES_API_FRAME_RATE KERNEL_IMAGES_API_DISPLAY_NUM KERNEL_IMAGES_API_MAX_SIZE_MB KERNEL_IMAGES_API_OUTPUT_DIR' \
    'ExecStart=/usr/bin/wrapper.sh' \
    'Restart=always' \
    'RestartSec=1s' \
    'WorkingDirectory=/' \
    '' \
    '[Install]' \
    'WantedBy=multi-user.target' \
    > /etc/systemd/system/wrapper.service; \
    mkdir -p /etc/systemd/system/multi-user.target.wants; \
    ln -sf /etc/systemd/system/wrapper.service /etc/systemd/system/multi-user.target.wants/wrapper.service

# Copy the kernel-images API binary built during the build process
COPY bin/kernel-images-api /usr/local/bin/kernel-images-api
ENV WITH_KERNEL_IMAGES_API=false

ENTRYPOINT [ "/sbin/init" ]
