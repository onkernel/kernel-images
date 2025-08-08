###############################################################################
# Stage 1 ─────────────────────────────────────────────────────────────────────
# Build WebRTC client frontend
###############################################################################
FROM node:22-bullseye-slim AS client

WORKDIR /src
COPY client/package*.json ./
RUN npm install
COPY client/ .
RUN npm run build


###############################################################################
# Stage 2 ─────────────────────────────────────────────────────────────────────
# Build custom Xorg dummy video driver (xf86-video-dummy v0.3.8 + RandR patch)
# and the custom input driver (xf86-input-neko)
###############################################################################
FROM ubuntu:22.04 AS xorg-deps
WORKDIR /xorg
ENV DEBIAN_FRONTEND=noninteractive


# Build-time dependencies for Xorg modules
RUN set -eux; \
    apt-get update; \
    apt-get install -y \
    git gcc pkgconf autoconf automake libtool make xorg-dev xutils-dev \
    && rm -rf /var/lib/apt/lists/*;

COPY xorg-deps/ /xorg/

# Build xf86-video-dummy v0.3.8 with RandR support
RUN cd xf86-video-dummy/v0.3.8 && \
    patch -p1 < ../01_v0.3.8_xdummy-randr.patch && \
    autoreconf -v --install && \
    ./configure && \
    make -j"$(nproc)" && \
    make install

# Build custom input driver
RUN cd xf86-input-neko && \
    ./autogen.sh --prefix=/usr && \
    ./configure && \
    make -j"$(nproc)" && \
    make install


###############################################################################
# Stage 3 ─────────────────────────────────────────────────────────────────────
# Extract neko executable from upstream image
###############################################################################
FROM ghcr.io/onkernel/neko/base:3.0.6-v1.0.1 AS neko


###############################################################################
# Stage 4 ─────────────────────────────────────────────────────────────────────
# Final runtime image
###############################################################################
FROM docker.io/ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV DEBIAN_PRIORITY=high

###############################################################################
# Base OS & core toolchain & apps
###############################################################################
RUN set -eux; \
    apt-get update; \
    apt-get -y upgrade; \
    apt-get -y install \
        xvfb xterm xdotool scrot imagemagick sudo mutter x11vnc upower \
        build-essential libssl-dev zlib1g-dev libbz2-dev libreadline-dev libsqlite3-dev \
        curl git libncursesw5-dev xz-utils tk-dev libxml2-dev libxmlsec1-dev libffi-dev liblzma-dev \
        net-tools netcat software-properties-common; \
    \
    sudo add-apt-repository ppa:mozillateam/ppa; \
    sudo apt-get install -y --no-install-recommends \
        chromium-browser libreoffice x11-apps xpdf gedit xpaint tint2 galculator pcmanfm \
        wget xdg-utils libvulkan1 fonts-liberation unzip; \
    apt-get clean


###############################################################################
# Locales & fonts
###############################################################################
RUN apt-get update && \
    apt-get install -y --no-install-recommends locales && \
    printf "%s\n" \
        "en_US.UTF-8 UTF-8" "en_GB.UTF-8 UTF-8" "es_ES.UTF-8 UTF-8" \
        "es_MX.UTF-8 UTF-8" "fr_FR.UTF-8 UTF-8" "de_DE.UTF-8 UTF-8" \
        "it_IT.UTF-8 UTF-8" "pt_PT.UTF-8 UTF-8" "pt_BR.UTF-8 UTF-8" \
        "nl_NL.UTF-8 UTF-8" "sv_SE.UTF-8 UTF-8" "no_NO.UTF-8 UTF-8" \
        "da_DK.UTF-8 UTF-8" "fi_FI.UTF-8 UTF-8" "tr_TR.UTF-8 UTF-8" \
        "vi_VN.UTF-8 UTF-8" "id_ID.UTF-8 UTF-8" "bn_IN.UTF-8 UTF-8" \
        "pa_IN.UTF-8 UTF-8" "zh_CN.UTF-8 UTF-8" "zh_TW.UTF-8 UTF-8" \
        "ja_JP.UTF-8 UTF-8" "ko_KR.UTF-8 UTF-8" "ar_SA.UTF-8 UTF-8" \
        "hi_IN.UTF-8 UTF-8" "ru_RU.UTF-8 UTF-8" "th_TH.UTF-8 UTF-8" \
        "el_GR.UTF-8 UTF-8" "he_IL.UTF-8 UTF-8" \
        > /etc/locale.gen && \
    locale-gen && \
    update-locale LANG=en_US.UTF-8 && \
    apt-get install -y --no-install-recommends \
        fonts-noto-core fonts-noto-ui-core fonts-noto-cjk fonts-noto-cjk-extra && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*


###############################################################################
# FFmpeg (static build, latest 7.x)
###############################################################################
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


###############################################################################
# Runtime dependencies, libxcvt, user creation, directory setup
###############################################################################
ARG KERNEL_USER=kernel
ARG KERNEL_UID=1000
ARG KERNEL_GID=$KERNEL_UID

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        wget ca-certificates python2 supervisor xclip xdotool \
        pulseaudio dbus-x11 xserver-xorg-video-dummy rtkit \
        libcairo2 libxcb1 libxrandr2 libxv1 libopus0 libvpx7 \
        gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
        gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly \
        gstreamer1.0-pulseaudio gstreamer1.0-omx && \
    \
    # libxcvt0 (Debian package, not in Ubuntu repo)
    ARCH=$(dpkg --print-architecture) && \
    curl -fsSL http://ftp.de.debian.org/debian/pool/main/libx/libxcvt/libxcvt0_0.1.2-1_${ARCH}.deb \
        -o /tmp/libxcvt.deb && \
    apt-get install -y --no-install-recommends /tmp/libxcvt.deb && \
    rm /tmp/libxcvt.deb && \
    \
    # Non-root user with audio/video privileges
    groupadd --gid "$KERNEL_GID" "$KERNEL_USER" && \
    useradd  --uid "$KERNEL_UID" --gid "$KERNEL_GID" \
             --shell /bin/bash --create-home "$KERNEL_USER" && \
    for g in audio video pulse pulse-access; do adduser "$KERNEL_USER" "$g"; done && \
    \
    # workaround for an X11 problem: http://blog.tigerteufel.de/?p=476
    mkdir -p /tmp/.X11-unix && \
    chmod 1777 /tmp/.X11-unix && \
    chown "$KERNEL_USER:$KERNEL_USER" /tmp/.X11-unix && \
    \
    # Make directories for neko
    mkdir -p /etc/neko /var/www /var/log/neko \
             /tmp/runtime-"$KERNEL_USER" \
             /home/"$KERNEL_USER"/.config \
             /home/"$KERNEL_USER"/.local/share/xorg && \
    chmod 1777 /var/log/neko && \
    chown -R "$KERNEL_USER:$KERNEL_USER" \
           /var/log/neko /tmp/runtime-"$KERNEL_USER" /home/"$KERNEL_USER" && \
    chmod 777 /etc/pulse || true && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /var/cache/apt/


###############################################################################
# Chromium (via xtradeb) & ncat
###############################################################################
RUN add-apt-repository -y ppa:xtradeb/apps && \
    apt-get update && \
    apt-get install -y --no-install-recommends chromium ncat && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*


###############################################################################
# noVNC (v1.5.0) + Websockify (v0.12.0)
###############################################################################
RUN git clone --branch v1.5.0 https://github.com/novnc/noVNC.git /opt/noVNC && \
    git clone --branch v0.12.0 https://github.com/novnc/websockify /opt/noVNC/utils/websockify && \
    ln -s /opt/noVNC/vnc.html /opt/noVNC/index.html


###############################################################################
# Environment
###############################################################################
ENV DISPLAY_NUM=1 \
    HEIGHT=768 \
    WIDTH=1024 \
    WITHDOCKER=true


###############################################################################
# Copy build artefacts & configurations
###############################################################################
COPY xorg.conf            /etc/neko/xorg.conf
COPY default.pa           /etc/pulse/default.pa
COPY daemon.conf          /etc/pulse/daemon.conf
COPY dbus-pulseaudio.conf /etc/dbus-1/system.d/pulseaudio.conf
COPY dbus-mpris.conf      /etc/dbus-1/system.d/mpris.conf
COPY neko.yaml            /etc/neko/neko.yaml

# Executables & drivers from previous stages
COPY --from=neko   /usr/bin/neko /usr/bin/neko
COPY --from=client /src/dist/    /var/www
COPY --from=xorg-deps /usr/local/lib/xorg/modules/drivers/dummy_drv.so \
                      /usr/lib/xorg/modules/drivers/dummy_drv.so
COPY --from=xorg-deps /usr/local/lib/xorg/modules/input/neko_drv.so  \
                      /usr/lib/xorg/modules/input/neko_drv.so

RUN chown -R "$KERNEL_USER:$KERNEL_USER" /var/www /etc/neko


###############################################################################
# Additional assets & wrapper
###############################################################################
COPY image-chromium/ /
COPY wrapper.sh      /wrapper.sh


###############################################################################
# Copy the kernel-images API binary built externally
###############################################################################
COPY bin/kernel-images-api /usr/local/bin/kernel-images-api
ENV WITH_KERNEL_IMAGES_API=false


###############################################################################
# Set user data
###############################################################################
RUN cp -r ./user-data /home/kernel/user-data


###############################################################################
# Runtime environment variables
###############################################################################
ENV USER=kernel \
    XDG_RUNTIME_DIR=/tmp/runtime-kernel \
    PULSE_SERVER=unix:${XDG_RUNTIME_DIR}/pulse/native \
    XDG_CONFIG_HOME=/tmp/.chromium \
    XDG_CACHE_HOME=/tmp/.chromium


###############################################################################
# Entrypoint
###############################################################################
ENTRYPOINT ["/wrapper.sh"]