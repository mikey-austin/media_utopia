GOCACHE ?= $(CURDIR)/.gocache

.PHONY: build test fmt

build:
	go build ./cmd/mu ./cmd/mud

test:
	GOCACHE=$(GOCACHE) go test -count=1 -v ./...

fmt:
	gofmt -w cmd internal pkg
