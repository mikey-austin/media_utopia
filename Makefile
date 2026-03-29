GOCACHE ?= $(CURDIR)/.gocache
BIN_DIR ?= $(CURDIR)/bin
REGISTRY ?= registry.nas.jackiemclean.net
DATE_TAG := $(shell date +%Y%m%d)

.PHONY: build mu-applet test fmt integration docker-library docker-library-push

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/mu ./cmd/mu
	go build -tags "gstreamer upnp chromaprint" -o $(BIN_DIR)/mud ./cmd/mud

mu-applet:
	mkdir -p $(BIN_DIR)
	go build -tags "gtk gtk_3_12" -o $(BIN_DIR)/mu-applet ./cmd/mu-applet

test:
	GOCACHE=$(GOCACHE) go test -count=1 -v ./...

fmt:
	gofmt -w cmd internal pkg

integration:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags=integration ./...

docker:
	docker build --target mud --build-arg BUILD_TAGS="upnp gstreamer chromaprint" \
		-t $(REGISTRY)/mud:$(DATE_TAG)-full .

docker-push: docker
	docker push $(REGISTRY)/mud:$(DATE_TAG)-full

docker-library:
	docker build --target mud-library --build-arg BUILD_TAGS="" --build-arg CGO=0 \
		-t $(REGISTRY)/mud-library:$(DATE_TAG)-nogst-noupnp .

docker-library-push: docker-library
	docker push $(REGISTRY)/mud-library:$(DATE_TAG)-nogst-noupnp
