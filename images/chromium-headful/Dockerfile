FROM docker.io/golang:1.25.0 AS server-builder
WORKDIR /workspace/server

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

COPY server/go.mod ./
COPY server/go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY server/ .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /out/kernel-images-api ./cmd/api

# webrtc client
FROM node:22-bullseye-slim AS client
WORKDIR /src
COPY images/chromium-headful/client/package*.json ./
RUN --mount=type=cache,target=/root/.npm npm install
COPY images/chromium-headful/client/ .
RUN --mount=type=cache,target=/root/.npm npm run build

# xorg dependencies
FROM docker.io/ubuntu:22.04 AS xorg-deps
WORKDIR /xorg
ENV DEBIAN_FRONTEND=noninteractive
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    rm -f /etc/apt/apt.conf.d/docker-clean; \
    echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache; \
    set -eux; \
    apt-get update; \
    apt-get --no-install-recommends -y install \
    git gcc pkgconf autoconf automake libtool make xorg-dev xutils-dev;
COPY images/chromium-headful/xorg-deps/ /xorg/
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

RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    rm -f /etc/apt/apt.conf.d/docker-clean; \
    echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache; \
    apt-get update && \
    apt-get -y upgrade && \
    apt-get --no-install-recommends -y install  \
    gpg-agent \
    # UI Requirements
    xvfb \
    xterm \
    xdotool \
    scrot \
    imagemagick \
    sudo \
    mutter \
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
    software-properties-common && \
    # Userland apps
    sudo add-apt-repository ppa:mozillateam/ppa && \
    sudo apt-get --no-install-recommends -y install \
    x11-apps \
    tint2 \
    wget \
    xdg-utils \
    libvulkan1 \
    fonts-liberation \
    unzip;

# install ffmpeg manually since the version available in apt is from the 4.x branch due to #drama.
RUN --mount=type=cache,target=/tmp/cache/ffmpeg,id=ffmpeg \
    <<-'EOT'
    set -eux
    ARCHIVE_NAME="ffmpeg-master-latest-linux64-gpl.tar.xz"
    URL="https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/$ARCHIVE_NAME"
    echo "Downloading FFmpeg checksum"
    if ! curl --connect-timeout 10 -fsSL "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/checksums.sha256" -o /tmp/cache/ffmpeg/ffmpeg.tar.xz.sha256; then
        echo "Failed to connect to ffmpeg static build provider for checksum."
        echo "Checking for cached version to use."
        if [ ! -f /tmp/cache/ffmpeg/ffmpeg.tar.xz.sha256 ]; then
            echo "Unable to locate cached checksum. Exiting."
            exit 1
        else
            echo "Found cached checksum."
        fi
    fi
    sed -i -n -e "/$ARCHIVE_NAME/p" /tmp/cache/ffmpeg/ffmpeg.tar.xz.sha256
    sed -i -e 's/ .*$/ \/tmp\/cache\/ffmpeg\/ffmpeg.tar.xz/' /tmp/cache/ffmpeg/ffmpeg.tar.xz.sha256
    echo "Checking cache for FFmpeg archive and validating checksum"
    if sha256sum --check /tmp/cache/ffmpeg/ffmpeg.tar.xz.sha256; then
        echo "Checksum validated, using cached FFmpeg archive"
    else
        echo "Downloading FFmpeg static build from $URL"
        curl -fsSL "$URL" -o /tmp/cache/ffmpeg/ffmpeg.tar.xz
        echo "Validating checksum of FFmpeg static build download"
        sha256sum --check /tmp/cache/ffmpeg/ffmpeg.tar.xz.sha256
    fi
    tar -xJf /tmp/cache/ffmpeg/ffmpeg.tar.xz -C /tmp
    install -m755 /tmp/ffmpeg-*/bin/ffmpeg  /usr/local/bin/ffmpeg
    install -m755 /tmp/ffmpeg-*/bin/ffprobe /usr/local/bin/ffprobe
    rm -rf /tmp/ffmpeg*
EOT

# runtime
ENV USERNAME=root
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    set -eux; \
    apt-get update; \
    apt-get --no-install-recommends -y install \
    wget ca-certificates python2 supervisor xclip xdotool \
    pulseaudio dbus-x11 xserver-xorg-video-dummy \
    libcairo2 libxcb1 libxrandr2 libxv1 libopus0 libvpx7 \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly \
    gstreamer1.0-pulseaudio gstreamer1.0-omx; \
    #
    # install libxcvt0 (not available in debian:bullseye)
    ARCH=$(dpkg --print-architecture); \
    wget http://ftp.de.debian.org/debian/pool/main/libx/libxcvt/libxcvt0_0.1.2-1_${ARCH}.deb; \
    apt-get --no-install-recommends install ./libxcvt0_0.1.2-1_${ARCH}.deb; \
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
    chown -R $USERNAME:$USERNAME /home/$USERNAME;

# install chromium and sqlite3 for debugging the cookies file
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    add-apt-repository -y ppa:xtradeb/apps;
RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=ubuntu2204-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=ubuntu2204-aptlib \
    apt update -y && \
    apt -y install chromium && \
    apt --no-install-recommends -y install sqlite3;

# setup desktop env & app
ENV DISPLAY_NUM=1
ENV HEIGHT=768
ENV WIDTH=1024
ENV WITHDOCKER=true

COPY images/chromium-headful/xorg.conf /etc/neko/xorg.conf
COPY images/chromium-headful/neko.yaml /etc/neko/neko.yaml
COPY --from=neko /usr/bin/neko /usr/bin/neko
COPY --from=client /src/dist/ /var/www
COPY --from=xorg-deps /usr/local/lib/xorg/modules/drivers/dummy_drv.so /usr/lib/xorg/modules/drivers/dummy_drv.so
COPY --from=xorg-deps /usr/local/lib/xorg/modules/input/neko_drv.so /usr/lib/xorg/modules/input/neko_drv.so

COPY images/chromium-headful/image-chromium/ /
COPY images/chromium-headful/start-chromium.sh /images/chromium-headful/start-chromium.sh
RUN chmod +x /images/chromium-headful/start-chromium.sh
COPY images/chromium-headful/wrapper.sh /wrapper.sh
COPY images/chromium-headful/supervisord.conf /etc/supervisor/supervisord.conf
COPY images/chromium-headful/supervisor/services/ /etc/supervisor/conf.d/services/

# copy the kernel-images API binary built in the builder stage
COPY --from=server-builder /out/kernel-images-api /usr/local/bin/kernel-images-api

RUN useradd -m -s /bin/bash kernel

ENTRYPOINT [ "/wrapper.sh" ]
