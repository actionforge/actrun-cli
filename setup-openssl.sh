#!/usr/bin/env bash
set -euo pipefail

# Builds static OpenSSL libraries for the given platform and architecture.
# Usage:
#   ./setup-openssl.sh <os> <arch> <output-lib-dir>
#
# Examples:
#   ./setup-openssl.sh macos arm64 ./ssl-libs
#   ./setup-openssl.sh macos x64   ./ssl-libs

OPENSSL_VERSION="3.5.0"

OS="${1:?Usage: $0 <os> <arch> <output-lib-dir>}"
ARCH="${2:?Usage: $0 <os> <arch> <output-lib-dir>}"
OUTPUT_LIB_DIR="${3:?Usage: $0 <os> <arch> <output-lib-dir>}"

if [[ -f "$OUTPUT_LIB_DIR/libssl.a" ]]; then
    echo "Static OpenSSL already present at $OUTPUT_LIB_DIR"
    exit 0
fi

# Determine OpenSSL configure target
case "$OS" in
    macos)
        if [[ "$ARCH" == "arm64" ]]; then
            OPENSSL_TARGET="darwin64-arm64-cc"
        else
            OPENSSL_TARGET="darwin64-x86_64-cc"
        fi
        ;;
    linux)
        if [[ "$ARCH" == "arm64" ]]; then
            OPENSSL_TARGET="linux-aarch64"
        else
            OPENSSL_TARGET="linux-x86_64"
        fi
        ;;
    windows)
        OPENSSL_TARGET="mingw64"
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

echo "Building static OpenSSL $OPENSSL_VERSION ($OPENSSL_TARGET) for $OS/$ARCH..."

TMPDIR_SSL="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_SSL"' EXIT

curl -sL "https://github.com/openssl/openssl/releases/download/openssl-${OPENSSL_VERSION}/openssl-${OPENSSL_VERSION}.tar.gz" \
    -o "$TMPDIR_SSL/openssl.tar.gz"
tar xzf "$TMPDIR_SSL/openssl.tar.gz" -C "$TMPDIR_SSL"

OPENSSL_SRC="$TMPDIR_SSL/openssl-${OPENSSL_VERSION}"

BUILD_LOG="$TMPDIR_SSL/build.log"
(
    cd "$OPENSSL_SRC"
    ./Configure "$OPENSSL_TARGET" no-shared no-tests no-apps no-docs --libdir=lib >> "$BUILD_LOG" 2>&1
    make -j"$(nproc 2>/dev/null || sysctl -n hw.ncpu)" build_libs >> "$BUILD_LOG" 2>&1
) || { echo "OpenSSL build failed. Last 30 lines of build log:"; tail -30 "$BUILD_LOG"; exit 1; }

mkdir -p "$OUTPUT_LIB_DIR"
cp "$OPENSSL_SRC/libssl.a" "$OPENSSL_SRC/libcrypto.a" "$OUTPUT_LIB_DIR/"
echo "Installed static OpenSSL $OPENSSL_VERSION to $OUTPUT_LIB_DIR"
