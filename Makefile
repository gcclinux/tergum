# Tergum Build Configuration
# Build produces a single statically-linked binary (no CGO).

BINARY     = tergum
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS = -s -w \
	-X 'github.com/ricardopadilha/tergum/cmd.Version=$(VERSION)' \
	-X 'github.com/ricardopadilha/tergum/cmd.Commit=$(COMMIT)' \
	-X 'github.com/ricardopadilha/tergum/cmd.BuildDate=$(BUILD_DATE)'

.PHONY: build clean test lint

## build: Compile static binary with version info injected via ldflags.
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./

## test: Run all tests.
test:
	CGO_ENABLED=0 go test ./...

## clean: Remove build artifacts.
clean:
	rm -f $(BINARY)

## lint: Run go vet.
lint:
	go vet ./...
