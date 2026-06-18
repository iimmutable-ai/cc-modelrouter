.PHONY: build release test coverage install snapshot clean

BINARY := ccrouter
CMD_DIR := ./cmd/ccrouter

# Build-time version injection (branch-aware)
BRANCH    ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
BUILDTIME ?= $(shell date +%y%m%d%H%M%S)
# Branch-aware tag: "dev" on dev-local, latest reachable tag elsewhere (v0.1.0 fallback).
ifeq ($(BRANCH),dev-local)
TAG ?= dev
else
TAG ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.1.0)
endif
LDFLAGS := -X github.com/iimmutable/cc-modelrouter/internal/version.Tag=$(TAG) \
           -X github.com/iimmutable/cc-modelrouter/internal/version.BuildTime=$(BUILDTIME)

build:
	go build -ldflags="$(LDFLAGS)" -o bin/debug/$(BINARY) $(CMD_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/linux-amd64/$(BINARY) $(CMD_DIR)
	GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/linux-arm64/$(BINARY) $(CMD_DIR)

release:
	go build -ldflags="-s -w $(LDFLAGS)" -o bin/release/$(BINARY) $(CMD_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w $(LDFLAGS)" -o bin/linux-amd64/$(BINARY) $(CMD_DIR)
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w $(LDFLAGS)" -o bin/linux-arm64/$(BINARY) $(CMD_DIR)

test:
	go test ./...

coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

install:
	go install $(CMD_DIR)

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin/ dist/