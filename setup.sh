#!/usr/bin/env bash
set -euo pipefail

# Downloads Perforce C++ API SDK libraries for the current (or specified) platform.
# Usage:
#   ./setup.sh              # auto-detect OS and arch
#   ./setup.sh linux x64    # explicit OS and arch

P4API_VERSION="r25.2"
P4API_BASE_URL="https://ftp.perforce.com/perforce/${P4API_VERSION}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
P4API_DIR="${SCRIPT_DIR}/p4api"

OS="${1:-}"
ARCH="${2:-}"

# Auto-detect if not provided
if [[ -z "$OS" ]]; then
    case "$(uname -s)" in
        Linux*)  OS="linux" ;;
        Darwin*) OS="macos" ;;
        MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
        *) echo "Unsupported OS: $(uname -s)"; exit 1 ;;
    esac
fi

if [[ -z "$ARCH" ]]; then
    case "$(uname -m)" in
        x86_64|amd64) ARCH="x64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) echo "Unsupported architecture: $(uname -m)"; exit 1 ;;
    esac
fi

# Map to Perforce download paths and local directory names
case "$OS" in
    linux)
        if [[ "$ARCH" == "arm64" ]]; then
            URL="${P4API_BASE_URL}/bin.linux26aarch64/p4api-openssl3.tgz"
            LIB_DIR="${P4API_DIR}/linux-aarch64/lib"
        else
            URL="${P4API_BASE_URL}/bin.linux26x86_64/p4api-glibc2.12-openssl3.tgz"
            LIB_DIR="${P4API_DIR}/linux-x86_64/lib"
        fi
        ARCHIVE_TYPE="tgz"
        ;;
    macos)
        URL="${P4API_BASE_URL}/bin.macosx12u/p4api-openssl3.tgz"
        LIB_DIR="${P4API_DIR}/macos/lib"
        ARCHIVE_TYPE="tgz"
        ;;
    windows)
        if [[ "$ARCH" == "arm64" ]]; then
            echo "Windows ARM64 is not supported by the Perforce C++ API SDK. Skipping."
            exit 0
        fi
        URL="${P4API_BASE_URL}/bin.mingw64x64/p4api-openssl3_gcc8_win32_seh.zip"
        LIB_DIR="${P4API_DIR}/windows-x86_64/lib"
        ARCHIVE_TYPE="zip"
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

# Skip if libs already present
if [[ -f "$LIB_DIR/libp4api.a" ]]; then
    echo "P4 API libs already present at $LIB_DIR"
    exit 0
fi

echo "Downloading P4 API SDK for $OS/$ARCH..."
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

if [[ "$ARCHIVE_TYPE" == "tgz" ]]; then
    curl -sL "$URL" -o "$TMPDIR/p4api.tgz"
    tar xzf "$TMPDIR/p4api.tgz" -C "$TMPDIR"
else
    curl -sL "$URL" -o "$TMPDIR/p4api.zip"
    unzip -q "$TMPDIR/p4api.zip" -d "$TMPDIR"
fi

# Find the extracted directory (e.g. p4api-2025.2.2907753)
EXTRACTED="$(find "$TMPDIR" -maxdepth 1 -type d -name 'p4api-*' | head -1)"
if [[ -z "$EXTRACTED" ]]; then
    echo "Error: Could not find extracted P4 API directory"
    exit 1
fi

mkdir -p "$LIB_DIR"
cp "$EXTRACTED"/lib/*.a "$LIB_DIR/"

# Update shared include directory (atomic via temp dir + mv to avoid races
# when multiple concurrent invocations run setup.sh in parallel).
INCLUDE_TMP="${P4API_DIR}/include.$$"
cp -r "$EXTRACTED/include" "$INCLUDE_TMP"
if ! mv -n "$INCLUDE_TMP" "$P4API_DIR/include" 2>/dev/null; then
    # Another invocation already placed the include dir — that's fine.
    rm -rf "$INCLUDE_TMP"
fi

echo "Installed P4 API libs to $LIB_DIR ($(ls "$LIB_DIR"/*.a | wc -l | tr -d ' ') files)"
