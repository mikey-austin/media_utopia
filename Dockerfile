# syntax=docker/dockerfile:1.7
#
# Multi-target Dockerfile for mud and mu.
#
# Targets:
#   mud           Full daemon with GStreamer + UPnP (Ubuntu runtime)
#   mud-library   Library-only daemon, no GStreamer/UPnP (distroless, static binary)
#   mu            CLI client (distroless, static binary)
#
# Build args:
#   BUILD_TAGS    Go build tags (default: "gstreamer upnp")
#   CGO           CGO_ENABLED value (default: "1")
#
# Examples:
#   Full build:    docker build --target mud -t mud:20260312 .
#   Library-only:  docker build --target mud-library --build-arg BUILD_TAGS="" --build-arg CGO=0 -t mud-library:20260312-nogst-noupnp .

FROM ubuntu:24.04 AS build
ENV DEBIAN_FRONTEND=noninteractive
ARG BUILD_TAGS="gstreamer upnp"
ARG CGO="1"
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    build-essential \
    golang \
    pkg-config \
    libglib2.0-dev \
    libgstreamer1.0-dev \
    libupnp-dev \
    libchromaprint-dev \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=${CGO} go build -trimpath -ldflags "-s -w" -tags "${BUILD_TAGS}" -o /out/mud ./cmd/mud
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/mu ./cmd/mu

FROM ubuntu:24.04 AS mud
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    glib-networking \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-alsa \
    gstreamer1.0-tools \
    gstreamer1.0-pipewire \
    libchromaprint1 \
    alsa-utils \
    libupnp17t64 \
    libasound2t64 \
    libgstreamer1.0-0 \
    libglib2.0-0t64 \
    python3 \
    python3-pip \
    python3-certifi \
 && pip3 install --break-system-packages yt-dlp \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/mud /usr/local/bin/mud
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mud"]

FROM gcr.io/distroless/static-debian12 AS mud-library
COPY --from=build /out/mud /usr/local/bin/mud
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mud"]

FROM gcr.io/distroless/static-debian12 AS mu
COPY --from=build /out/mu /usr/local/bin/mu
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mu"]
