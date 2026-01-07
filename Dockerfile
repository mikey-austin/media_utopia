# syntax=docker/dockerfile:1.7

FROM ubuntu:24.04 AS build
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    build-essential \
    golang \
    pkg-config \
    libglib2.0-dev \
    libgstreamer1.0-dev \
    libupnp-dev \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -tags "gstreamer upnp" -o /out/mud ./cmd/mud
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
    alsa-utils \
    libupnp17t64 \
    libasound2t64 \
    libgstreamer1.0-0 \
    libglib2.0-0t64 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/mud /usr/local/bin/mud
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mud"]

FROM gcr.io/distroless/static-debian12 AS mu
COPY --from=build /out/mu /usr/local/bin/mu
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mu"]
