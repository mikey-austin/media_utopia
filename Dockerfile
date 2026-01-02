# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    libglib2.0-dev \
    libgstreamer1.0-dev \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -tags "gstreamer" -o /out/mud ./cmd/mud
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/mu ./cmd/mu

FROM debian:bookworm-slim AS mud
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-alsa \
    gstreamer1.0-tools \
    alsa-utils \
    libasound2 \
    libgstreamer1.0-0 \
    libglib2.0-0 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/mud /usr/local/bin/mud
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mud"]

FROM gcr.io/distroless/static-debian12 AS mu
COPY --from=build /out/mu /usr/local/bin/mu
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mu"]
