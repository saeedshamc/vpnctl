#!/usr/bin/env bash
# Download and verify pinned Linux amd64 adb + tun2socks into bundle/.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUNDLE="$ROOT/bundle"
mkdir -p "$BUNDLE"

TUN2SOCKS_VER="v2.5.2"
TUN2SOCKS_URL="https://github.com/xjasonlyu/tun2socks/releases/download/${TUN2SOCKS_VER}/tun2socks-linux-amd64.zip"
PLATFORM_TOOLS_VER="r37.0.3"
PLATFORM_TOOLS_SHA1="e1579b288a7ae4e9bc28dedec6a154d78b3c04ed"
PLATFORM_TOOLS_URL="https://dl.google.com/android/repository/platform-tools_${PLATFORM_TOOLS_VER}-linux.zip"

tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT

echo "fetching tun2socks ${TUN2SOCKS_VER}"
curl -fsSL "$TUN2SOCKS_URL" -o "$tmp/tun2socks.zip"
unzip -o "$tmp/tun2socks.zip" -d "$tmp"
mapfile -t tun_binaries < <(find "$tmp" -type f -name tun2socks ! -name '*.zip')
if [[ "${#tun_binaries[@]}" -ne 1 ]]; then
  echo "expected exactly one tun2socks binary in upstream archive, found ${#tun_binaries[@]}" >&2
  exit 1
fi
install -m 755 "${tun_binaries[0]}" "$BUNDLE/tun2socks"

echo "fetching Android platform-tools ${PLATFORM_TOOLS_VER} (adb)"
curl -fsSL "$PLATFORM_TOOLS_URL" -o "$tmp/platform-tools.zip"
printf '%s  %s\n' "$PLATFORM_TOOLS_SHA1" "$tmp/platform-tools.zip" | sha1sum --check
unzip -o "$tmp/platform-tools.zip" -d "$tmp"
install -m 755 "$tmp/platform-tools/adb" "$BUNDLE/adb"
if [[ ! -f "$tmp/platform-tools/NOTICE.txt" ]]; then
  echo "Android platform-tools archive does not contain NOTICE.txt" >&2
  exit 1
fi
install -m 644 "$tmp/platform-tools/NOTICE.txt" "$BUNDLE/adb-NOTICE.txt"

(
  cd "$BUNDLE"
  sha256sum adb tun2socks > checksums.txt.tmp
  mv checksums.txt.tmp checksums.txt
  printf 'tun2socks=%s\nplatform-tools=%s\n' "$TUN2SOCKS_VER" "$PLATFORM_TOOLS_VER" > versions.txt.tmp
  mv versions.txt.tmp versions.txt
)
echo "bundle ready:"
cat "$BUNDLE/checksums.txt"
