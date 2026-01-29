#!/bin/bash
# actrun - ActionForge graph runner
#
# This script is deployed at:
#   https://www.actionforge.dev/actrun.sh
#
# This script is deployed from:
#   https://github.com/actionforge/actrun-cli/blob/main/actrun.sh
#
# Usage:
#   curl -fsSL https://www.actionforge.dev/actrun.sh | bash -s -- <file.act> [options]
#   curl -fsSL https://www.actionforge.dev/actrun.sh | bash -s -- <shared-url> [options]
#
# Examples:
#   curl -fsSL https://www.actionforge.dev/actrun.sh | bash -s -- hello-world.act
#   curl -fsSL https://www.actionforge.dev/actrun.sh | bash -s -- workflow.act --verbose
#   curl -fsSL https://www.actionforge.dev/actrun.sh | bash -s -- https://app.actionforge.dev/shared/wispy-paper-a49c664e.act
#   curl -fsSL https://www.actionforge.dev/actrun.sh | bash -s -- --help
#
set -e

DOWNLOAD_BASE="https://www.actionforge.dev/download"
API_URL="https://app.actionforge.dev/api/v2/releases/list"
SHARE_API_URL="https://app.actionforge.dev/api/v2/share/graph/read"
CACHE_DIR="${HOME}/.cache/actrun"

# Detect OS
case "$(uname -s)" in
    Linux*)  OS="linux" ;;
    Darwin*) OS="macos" ;;
    MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
    *) echo "❌ Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

# Detect architecture
case "$(uname -m)" in
    x86_64|amd64) ARCH="x64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "❌ Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

# Get latest version from API (find highest version, excluding prereleases)
VERSION=$(curl -fsSL "$API_URL" | \
    grep -o '"version":"v[0-9]*\.[0-9]*\.[0-9]*"' | \
    cut -d'"' -f4 | sort -uV | tail -1)

if [ -z "$VERSION" ]; then
    echo "❌ Failed to fetch latest version" >&2
    exit 1
fi

# Check cache
CACHE_VERSION_DIR="$CACHE_DIR/$VERSION"
if [ "$OS" = "windows" ]; then
    CACHED_BINARY="$CACHE_VERSION_DIR/actrun.exe"
else
    CACHED_BINARY="$CACHE_VERSION_DIR/actrun"
fi

if [ -x "$CACHED_BINARY" ]; then
    echo "✅ Using cached actrun $VERSION"

    # Process shared URL if first argument matches the pattern
    if [ $# -gt 0 ] && [[ "$1" =~ ^https://app\.actionforge\.dev/shared/(.+\.act)$ ]]; then
        share_id="${BASH_REMATCH[1]}"
        echo "🔗 Fetching shared graph: $share_id"

        graph_tmp=$(mktemp -t "actrun-shared-XXXXXX.act")
        trap "rm -f '$graph_tmp'" EXIT

        if ! curl -fsSL "${SHARE_API_URL}?id=${share_id}" -o "$graph_tmp"; then
            echo "❌ Failed to fetch shared graph: $share_id" >&2
            exit 1
        fi

        shift
        exec "$CACHED_BINARY" "$graph_tmp" "$@"
    fi

    exec "$CACHED_BINARY" "$@"
fi

# Determine file extension
case "$OS" in
    linux)   EXT="tar.gz" ;;
    windows) EXT="zip" ;;
    macos)   EXT="pkg" ;;
esac

FILENAME="actrun-${VERSION}.cli-${ARCH}-${OS}.${EXT}"
URL="${DOWNLOAD_BASE}/${FILENAME}"

echo "⬇️  Downloading $FILENAME..."

TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

curl -fsSL "$URL" -o "$TMP_DIR/$FILENAME"

echo "📦 Unpacking..."

# Extract and find binary
case "$OS" in
    linux)
        tar -xzf "$TMP_DIR/$FILENAME" -C "$TMP_DIR"
        BINARY="$TMP_DIR/actrun"
        ;;
    windows)
        unzip -q "$TMP_DIR/$FILENAME" -d "$TMP_DIR"
        BINARY="$TMP_DIR/actrun.exe"
        ;;
    macos)
        pkgutil --expand-full "$TMP_DIR/$FILENAME" "$TMP_DIR/pkg_expanded"
        BINARY=$(find "$TMP_DIR/pkg_expanded" -name "actrun" -type f -perm +111 2>/dev/null | head -1)
        if [ -z "$BINARY" ]; then
            BINARY=$(find "$TMP_DIR/pkg_expanded" -name "actrun" -type f | head -1)
        fi
        ;;
esac

if [ ! -f "$BINARY" ]; then
    echo "❌ Failed to find actrun binary" >&2
    exit 1
fi

# Cache the binary
mkdir -p "$CACHE_VERSION_DIR"
cp "$BINARY" "$CACHED_BINARY"
chmod +x "$CACHED_BINARY"

echo "✅ Unpacked actrun $VERSION"

# Process shared URL if first argument matches the pattern
if [ $# -gt 0 ] && [[ "$1" =~ ^https://app\.actionforge\.dev/shared/(.+\.act)$ ]]; then
    share_id="${BASH_REMATCH[1]}"
    echo "🔗 Fetching shared graph: $share_id"

    graph_tmp=$(mktemp -t "actrun-shared-XXXXXX.act")
    trap "rm -rf '$TMP_DIR'; rm -f '$graph_tmp'" EXIT

    if ! curl -fsSL "${SHARE_API_URL}?id=${share_id}" -o "$graph_tmp"; then
        echo "❌ Failed to fetch shared graph: $share_id" >&2
        exit 1
    fi

    shift
    exec "$CACHED_BINARY" "$graph_tmp" "$@"
fi

exec "$CACHED_BINARY" "$@"
