#!/bin/bash
# Tergum build script
# Usage: ./build.sh [--prod] [linux|darwin|windows|all]

set -e

PROD=0
TARGET=""
for arg in "$@"; do
    case "$arg" in
        --prod) PROD=1 ;;
        *) TARGET="$arg" ;;
    esac
done
TARGET="${TARGET:-all}"

if [ "$PROD" = "1" ]; then
    if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
        echo "WARNING: Working tree is dirty. Prod build will use clean version anyway." >&2
    fi
fi

# Read version from release file (single source of truth)
if [ -f "release" ]; then
    RELEASE_VERSION=$(cat release | tr -d '[:space:]')
fi

# Read from version.json if available
if [ -f "version.json" ]; then
    JSON_VERSION=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' version.json)
    JSON_BUILD=$(sed -n 's/.*"build": *"\([^"]*\)".*/\1/p' version.json)
fi

VERSION="${VERSION:-${RELEASE_VERSION:-${JSON_VERSION:-3.0.0}}}"
BUILD="${BUILD:-${JSON_BUILD:-dev}}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "none")}"
BUILD_DATE="${BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}"

LDFLAGS="-s -w \
  -X 'github.com/gcclinux/tergum/internal/version.Version=${VERSION}' \
  -X 'github.com/gcclinux/tergum/internal/version.Build=${BUILD}' \
  -X 'github.com/gcclinux/tergum/internal/version.Commit=${COMMIT}' \
  -X 'github.com/gcclinux/tergum/internal/version.BuildDate=${BUILD_DATE}' \
  -X 'github.com/gcclinux/tergum/cmd.Version=${VERSION}' \
  -X 'github.com/gcclinux/tergum/cmd.Commit=${COMMIT}' \
  -X 'github.com/gcclinux/tergum/cmd.BuildDate=${BUILD_DATE}'"

OUTPUT_DIR="${OUTPUT_DIR:-./dist}"
mkdir -p "$OUTPUT_DIR"

build_linux() {
    echo "Building for Linux (amd64 & aarch64)..."
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/tergum-amd64-linux" ./
    echo "  -> $OUTPUT_DIR/tergum-amd64-linux"
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/tergum-aarch64-linux" ./
    echo "  -> $OUTPUT_DIR/tergum-aarch64-linux"
}

build_darwin() {
    echo "Building for macOS (arm64)..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/tergum-arm64-macos" ./
    echo "  -> $OUTPUT_DIR/tergum-arm64-macos"
}

build_windows() {
    echo "Building for Windows (amd64)..."
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/tergum-amd64.exe" ./
    echo "  -> $OUTPUT_DIR/tergum-amd64.exe"
}

build_all() {
    build_linux
    build_darwin
    build_windows
}

case "$TARGET" in
    linux)
        build_linux
        ;;
    darwin|macos)
        build_darwin
        ;;
    windows|win)
        build_windows
        ;;
    all)
        build_all
        ;;
    *)
        echo "Usage: $0 [linux|darwin|windows|all]"
        echo ""
        echo "Options:"
        echo "  linux    Build for Linux (amd64)"
        echo "  darwin   Build for macOS (arm64)"
        echo "  windows  Build for Windows (amd64)"
        echo "  all      Build all platforms (default)"
        exit 1
        ;;
esac

echo ""
echo "Build complete! (version: $VERSION, commit: $COMMIT)"
