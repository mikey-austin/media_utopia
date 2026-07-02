GOCACHE ?= $(CURDIR)/.gocache
BIN_DIR ?= $(CURDIR)/bin
REGISTRY ?= registry.nas.jackiemclean.net
DATE_TAG := $(shell date +%Y%m%d)

.PHONY: build build-mpv mu-applet test test-mpv fmt integration integration-mpv docker-library docker-library-push

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/mu ./cmd/mu
	go build -tags "gstreamer upnp chromaprint" -o $(BIN_DIR)/mud ./cmd/mud

# Requires libmpv-dev; builds mud with the mpv renderer in addition to the
# default module set.
build-mpv:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/mu ./cmd/mu
	go build -tags "gstreamer upnp chromaprint mpv" -o $(BIN_DIR)/mud ./cmd/mud

mu-applet:
	mkdir -p $(BIN_DIR)
	CGO_LDFLAGS="-Wl,--allow-multiple-definition" go build -tags "gstreamer gtk gtk_3_12" -o $(BIN_DIR)/mu-applet ./cmd/mu-applet

test:
	GOCACHE=$(GOCACHE) go test -count=1 -v ./...

fmt:
	gofmt -w cmd internal pkg

integration:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags=integration ./...

# Requires libmpv-dev.
test-mpv:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags mpv ./internal/modules/renderer_mpv/...

# Requires libmpv-dev. Set MU_SOAK_STREAMS="url,url,..." to also run the
# real-stream soak pass.
integration-mpv:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags "mpv integration" ./internal/modules/renderer_mpv/...

docker:
	docker build --target mud --build-arg BUILD_TAGS="upnp gstreamer chromaprint mpv" \
		-t $(REGISTRY)/mud:$(DATE_TAG)-full .

docker-push: docker
	docker push $(REGISTRY)/mud:$(DATE_TAG)-full

docker-library:
	docker build --target mud-library --build-arg BUILD_TAGS="chromaprint" --build-arg CGO=1 \
		-t $(REGISTRY)/mud-library:$(DATE_TAG)-chromaprint .

docker-library-push: docker-library
	docker push $(REGISTRY)/mud-library:$(DATE_TAG)-chromaprint
