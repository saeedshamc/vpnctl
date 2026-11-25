#!/usr/bin/env bash
# Download pinned adb + tun2socks into bundle/ (Linux amd64).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUNDLE="$ROOT/bundle"
mkdir -p "$BUNDLE"

TUN2SOCKS_VER="${TUN2SOCKS_VER:-v2.5.2}"
TUN2SOCKS_URL="https://github.com/xjasonlyu/tun2socks/releases/download/${TUN2SOCKS_VER}/tun2socks-linux-amd64.zip"

echo "fetching tun2socks ${TUN2SOCKS_VER}"
tmp="$(mktemp -d)"
curl -fsSL "$TUN2SOCKS_URL" -o "$tmp/tun2socks.zip"
unzip -o "$tmp/tun2socks.zip" -d "$tmp"
# archive layout may vary; find the binary
find "$tmp" -type f -name 'tun2socks*' ! -name '*.zip' -exec cp {} "$BUNDLE/tun2socks" \;
chmod +x "$BUNDLE/tun2socks"

PLATFORM_TOOLS_URL="${PLATFORM_TOOLS_URL:-https://dl.google.com/android/repository/platform-tools-latest-linux.zip}"
echo "fetching android platform-tools (adb)"
curl -fsSL "$PLATFORM_TOOLS_URL" -o "$tmp/platform-tools.zip"
unzip -o "$tmp/platform-tools.zip" -d "$tmp"
cp "$tmp/platform-tools/adb" "$BUNDLE/adb"
chmod +x "$BUNDLE/adb"

(
  cd "$BUNDLE"
  sha256sum adb tun2socks > checksums.txt
)
rm -rf "$tmp"
echo "bundle ready:"
cat "$BUNDLE/checksums.txt"
