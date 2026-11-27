#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_ARCH="${GOARCH:-amd64}"
VERSION="${VERSION:-0.1.0}"
OUT="${OUT:-$ROOT/dist}"
BIN_OUT="${BIN_OUT:-$ROOT/bin}"
BUNDLE_DIR="${BUNDLE_DIR:-$ROOT/bundle}"
ARCHIVE="$OUT/vpnctl_${VERSION}_linux_${GO_ARCH}.tar.gz"

if [[ "$GO_ARCH" != "amd64" ]]; then
  echo "portable bundles currently support GOARCH=amd64 only (adb bundle is Linux amd64)" >&2
  exit 1
fi

if [[ ! "$VERSION" =~ ^[A-Za-z0-9.+~:-]+$ ]]; then
  echo "invalid version: $VERSION" >&2
  exit 1
fi

for binary in adb tun2socks; do
  if [[ ! -x "$BUNDLE_DIR/$binary" ]]; then
    echo "missing $BUNDLE_DIR/$binary; run scripts/fetch-bundle.sh first" >&2
    exit 1
  fi
done
if [[ ! -f "$BUNDLE_DIR/adb-NOTICE.txt" ]]; then
  echo "missing $BUNDLE_DIR/adb-NOTICE.txt; run scripts/fetch-bundle.sh first" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/checksums.txt" ]]; then
  echo "missing $BUNDLE_DIR/checksums.txt" >&2
  exit 1
fi
(cd "$BUNDLE_DIR" && sha256sum --check checksums.txt)

mkdir -p "$OUT"
export CGO_ENABLED=0 GOOS=linux GOARCH="$GO_ARCH" VERSION
OUT="$BIN_OUT" bash "$ROOT/scripts/build.sh"

STAGE="$(mktemp -d "$OUT/.vpnctl-portable.XXXXXX")"
trap 'rm -rf -- "$STAGE"' EXIT
mkdir -p "$STAGE/$GO_ARCH"
for binary in vpnctl vpnctl-gui vpnctld vpnctl-installer; do
  install -m 755 "$BIN_OUT/$binary" "$STAGE/$GO_ARCH/$binary"
done
install -m 755 "$BUNDLE_DIR/adb" "$STAGE/$GO_ARCH/adb"
install -m 755 "$BUNDLE_DIR/tun2socks" "$STAGE/$GO_ARCH/tun2socks"
cp "$BUNDLE_DIR/checksums.txt" "$STAGE/$GO_ARCH/checksums.txt"
cp "$BUNDLE_DIR/adb-NOTICE.txt" "$STAGE/$GO_ARCH/NOTICE.adb"
cp "$ROOT/LICENSE" "$STAGE/$GO_ARCH/LICENSE"
cp "$ROOT/README.md" "$STAGE/$GO_ARCH/README.md"
tar -C "$STAGE" -czf "$ARCHIVE" "$GO_ARCH"
(
  OUT="$OUT" bash "$ROOT/scripts/update-checksums.sh" "$(basename "$ARCHIVE")"
)
echo "built $ARCHIVE"
