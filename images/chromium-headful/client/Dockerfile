ARG BASE_IMAGE=node:18-bullseye-slim
FROM $BASE_IMAGE AS client

WORKDIR /src

# Allow cross-compilation when building with BuildKit platforms
ARG TARGETARCH
ARG TARGETOS
ARG CACHEIDPREFIX=${TARGETOS:-linux}-${TARGETARCH:-amd64}-node18bullseye

#
# install dependencies
COPY package*.json ./
RUN --mount=type=cache,target=/root/.npm,id=$CACHEIDPREFIX-npm npm install

#
# build client
COPY . .
RUN --mount=type=cache,target=/root/.npm,id=$CACHEIDPREFIX-npm npm run build

#
# artifacts from this stage
# COPY --from=client /src/dist/ /var/www
