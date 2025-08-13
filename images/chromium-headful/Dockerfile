# webrtc client
FROM node:22-bullseye-slim AS client
WORKDIR /src
COPY client/package*.json ./
RUN npm install
COPY client/ .
RUN npm run build

# xorg dependencies
FROM docker.io/ubuntu:22.04 AS xorg-deps
WORKDIR /xorg
ENV DEBIAN_FRONTEND=noninteractive
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
    git gcc pkgconf autoconf automake libtool make xorg-dev xutils-dev \
    && rm -rf /var/lib/apt/lists/*;
COPY xorg-deps/ /xorg/
# build xf86-video-dummy v0.3.8 with RandR support
RUN set -eux; \
    cd xf86-video-dummy/v0.3.8; \
    patch -p1 < ../01_v0.3.8_xdummy-randr.patch; \
    autoreconf -v --install; \
    ./configure; \
    make -j$(nproc); \
    make install;
# build custom input driver
RUN set -eux; \
    cd xf86-input-neko; \
    ./autogen.sh --prefix=/usr; \
    ./configure; \
    make -j$(nproc); \
    make install;

FROM ghcr.io/onkernel/neko/base:3.0.6-v1.0.1 AS neko
# ^--- now has event.SYSTEM_PONG with legacy support to keepalive
FROM docker.io/ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV DEBIAN_PRIORITY=high
ENV container=docker

# Ensure Docker sends the correct stop signal understood by systemd
STOPSIGNAL SIGRTMIN+3

RUN apt-get update && \
    apt-get -y upgrade && \
    apt-get -y install --no-install-recommends \
    # UI Requirements
    xvfb \
    xterm \
    xdotool \
    scrot \
    imagemagick \
    sudo \
    mutter \
    x11vnc \
    # Python/pyenv reqs
    build-essential \
    libssl-dev  \
    zlib1g-dev \
    libbz2-dev \
    libreadline-dev \
    libsqlite3-dev \
    curl \
    git \
    libncursesw5-dev \
    xz-utils \
    tk-dev \
    libxml2-dev \
    libxmlsec1-dev \
    libffi-dev \
    liblzma-dev \
    # Network tools
    net-tools \
    netcat \
    # PPA req
    software-properties-common \
    gpg-agent \
    dirmngr && \
    # Userland apps
    sudo apt-get install -y --no-install-recommends \
    chromium-browser \
    libreoffice \
    x11-apps \
    xpdf \
    gedit \
    xpaint \
    tint2 \
    galculator \
    pcmanfm \
    wget \
    xdg-utils \
    libvulkan1 \
    fonts-liberation \
    unzip && \
    apt-get clean

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

# install ffmpeg manually since the version available in apt is from the 4.x branch due to #drama.
# as of writing these static builds will be the latest 7.0.x release.
RUN set -eux; \
    URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"; \
    echo "Downloading FFmpeg static build from $URL"; \
    curl -fsSL "$URL" -o /tmp/ffmpeg.tar.xz; \
    tar -xJf /tmp/ffmpeg.tar.xz -C /tmp; \
    install -m755 /tmp/ffmpeg-*/ffmpeg  /usr/local/bin/ffmpeg; \
    install -m755 /tmp/ffmpeg-*/ffprobe /usr/local/bin/ffprobe; \
    rm -rf /tmp/ffmpeg*

# runtime
ENV USERNAME=root
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
    wget ca-certificates python2 xclip xdotool \
    pulseaudio dbus-x11 xserver-xorg-video-dummy \
    libcairo2 libxcb1 libxrandr2 libxv1 libopus0 libvpx7 \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly \
    gstreamer1.0-pulseaudio gstreamer1.0-omx; \
    #
    # install libxcvt0 (not available in debian:bullseye)
    ARCH=$(dpkg --print-architecture); \
    wget http://ftp.de.debian.org/debian/pool/main/libx/libxcvt/libxcvt0_0.1.2-1_${ARCH}.deb; \
    apt-get install --no-install-recommends ./libxcvt0_0.1.2-1_${ARCH}.deb; \
    rm ./libxcvt0_0.1.2-1_${ARCH}.deb; \
    #
    # workaround for an X11 problem: http://blog.tigerteufel.de/?p=476
    mkdir /tmp/.X11-unix; \
    chmod 1777 /tmp/.X11-unix; \
    chown $USERNAME /tmp/.X11-unix/; \
    #
    # make directories for neko
    mkdir -p /etc/neko /var/www /var/log/neko \
    /tmp/runtime-$USERNAME \
    /home/$USERNAME/.config/pulse  \
    /home/$USERNAME/.local/share/xorg; \
    chmod 1777 /var/log/neko; \
    chown $USERNAME /var/log/neko/ /tmp/runtime-$USERNAME; \
    chown -R $USERNAME:$USERNAME /home/$USERNAME; \
    # clean up
    apt-get clean -y; \
    rm -rf /var/lib/apt/lists/* /var/cache/apt/

# install chromium & ncat for proxying the remote debugging port
RUN add-apt-repository -y ppa:xtradeb/apps
RUN apt update -y && apt install -y --no-install-recommends chromium ncat

# setup desktop env & app
ENV DISPLAY_NUM=1
ENV HEIGHT=768
ENV WIDTH=1024
ENV WITHDOCKER=true

COPY xorg.conf /etc/neko/xorg.conf
COPY neko.yaml /etc/neko/neko.yaml
COPY --from=neko /usr/bin/neko /usr/bin/neko
COPY --from=client /src/dist/ /var/www
COPY --from=xorg-deps /usr/local/lib/xorg/modules/drivers/dummy_drv.so /usr/lib/xorg/modules/drivers/dummy_drv.so
COPY --from=xorg-deps /usr/local/lib/xorg/modules/input/neko_drv.so /usr/lib/xorg/modules/input/neko_drv.so

COPY image-chromium/ /
COPY ./wrapper.sh /wrapper.sh

# Make wrapper executable and register a systemd service to run it as root
RUN set -eux; \
    chmod +x /wrapper.sh; \
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
    'ExecStart=/wrapper.sh' \
    'Restart=always' \
    'RestartSec=1s' \
    'WorkingDirectory=/' \
    '' \
    '[Install]' \
    'WantedBy=multi-user.target' \
    > /etc/systemd/system/wrapper.service; \
    mkdir -p /etc/systemd/system/multi-user.target.wants; \
    ln -sf /etc/systemd/system/wrapper.service /etc/systemd/system/multi-user.target.wants/wrapper.service

# copy the kernel-images API binary built externally
COPY bin/kernel-images-api /usr/local/bin/kernel-images-api
ENV WITH_KERNEL_IMAGES_API=false

RUN useradd -m -s /bin/bash kernel
RUN cp -r ./user-data /home/kernel/user-data

ENTRYPOINT [ "/sbin/init" ]

