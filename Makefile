GOCACHE ?= $(CURDIR)/.gocache

.PHONY: build test fmt integration

build:
	go build ./cmd/mu ./cmd/mud

test:
	GOCACHE=$(GOCACHE) go test -count=1 -v ./...

fmt:
	gofmt -w cmd internal pkg

integration:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags=integration ./...
