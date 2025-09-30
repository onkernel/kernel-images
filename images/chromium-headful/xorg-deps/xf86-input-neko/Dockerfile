FROM debian:bullseye-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,sharing=private,id=debian-bullseye-aptcache \
    --mount=type=cache,target=/var/lib/apt,sharing=private,id=debian-bullseye-aptlib \
    rm -f /etc/apt/apt.conf.d/docker-clean; \
    echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache; \
    set -eux; \
    apt-get update; \
    apt-get install --no-install-recommends -y \
        gcc pkgconf autoconf automake libtool make xorg-dev xutils-dev;

WORKDIR /app

COPY ./ /app/

RUN set -eux; \
    ./autogen.sh --prefix=/usr; \
    ./configure; \
    make -j$(nproc); \
    make install;

# docker build -t xf86-input-neko .
# docker run -v $PWD/build:/app/build --rm xf86-input-neko make install DESTDIR=/app/build
