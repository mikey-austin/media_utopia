GOCACHE ?= $(CURDIR)/.gocache
BIN_DIR ?= $(CURDIR)/bin

.PHONY: build test fmt integration

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/mu ./cmd/mu
	go build -o $(BIN_DIR)/mud ./cmd/mud

test:
	GOCACHE=$(GOCACHE) go test -count=1 -v ./...

fmt:
	gofmt -w cmd internal pkg

integration:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags=integration ./...
