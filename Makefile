GOCACHE ?= $(CURDIR)/.gocache
BIN_DIR ?= $(CURDIR)/bin
REGISTRY ?= registry.nas.jackiemclean.net
DATE_TAG := $(shell date +%Y%m%d)

.PHONY: build test fmt integration docker-library docker-library-push

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/mu ./cmd/mu
	go build -tags "gstreamer upnp chromaprint" -o $(BIN_DIR)/mud ./cmd/mud

test:
	GOCACHE=$(GOCACHE) go test -count=1 -v ./...

fmt:
	gofmt -w cmd internal pkg

integration:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags=integration ./...

docker-library:
	docker build --target mud-library --build-arg BUILD_TAGS="" --build-arg CGO=0 \
		-t $(REGISTRY)/mud-library:$(DATE_TAG)-nogst-noupnp .

docker-library-push: docker-library
	docker push $(REGISTRY)/mud-library:$(DATE_TAG)-nogst-noupnp
