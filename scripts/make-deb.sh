#!/usr/bin/env bash
# Build a Debian package for the selected Linux target. Requires dpkg-deb.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="${VERSION:-0.1.0}"
GO_ARCH="${GOARCH:-amd64}"
if [[ "$GO_ARCH" != "amd64" ]]; then
  echo "Debian packaging currently supports GOARCH=amd64 only (adb bundle is Linux amd64)" >&2
  exit 1
fi
ARCH="amd64"
OUT="${OUT:-$ROOT/dist}"
BIN_OUT="${BIN_OUT:-$ROOT/bin}"
BUNDLE_DIR="${BUNDLE_DIR:-$ROOT/bundle}"
PKG="vpnctl_${VER}_${ARCH}"

if [[ ! "$VER" =~ ^[A-Za-z0-9.+~:-]+$ ]]; then
  echo "invalid Debian package version: $VER" >&2
  exit 1
fi

mkdir -p "$OUT"
STAGE="$(mktemp -d "$OUT/.vpnctl-deb.XXXXXX")"
trap 'rm -rf -- "$STAGE"' EXIT

export CGO_ENABLED=0 GOOS=linux GOARCH="$GO_ARCH" VERSION="$VER"
OUT="$BIN_OUT" bash "$ROOT/scripts/build.sh"

if [[ ! -x "$BUNDLE_DIR/adb" || ! -x "$BUNDLE_DIR/tun2socks" ]]; then
  echo "missing bundle binaries; run scripts/fetch-bundle.sh first" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/adb-NOTICE.txt" ]]; then
  echo "missing $BUNDLE_DIR/adb-NOTICE.txt; run scripts/fetch-bundle.sh first" >&2
  exit 1
fi
if [[ -f "$BUNDLE_DIR/checksums.txt" ]]; then
  (cd "$BUNDLE_DIR" && sha256sum --check checksums.txt)
else
  echo "missing $BUNDLE_DIR/checksums.txt" >&2
  exit 1
fi

mkdir -p \
  "$STAGE/usr/bin" \
  "$STAGE/usr/lib/vpnctl" \
  "$STAGE/usr/share/applications" \
  "$STAGE/usr/share/doc/vpnctl" \
  "$STAGE/usr/share/icons/hicolor/scalable/apps" \
  "$STAGE/usr/share/polkit-1/actions" \
  "$STAGE/lib/systemd/system" \
  "$STAGE/etc/vpnctl" \
  "$STAGE/DEBIAN"

install -m 755 "$BIN_OUT/vpnctl" "$STAGE/usr/bin/vpnctl"
install -m 755 "$BIN_OUT/vpnctl-gui" "$STAGE/usr/bin/vpnctl-gui"
install -m 755 "$BIN_OUT/vpnctld" "$STAGE/usr/lib/vpnctl/vpnctld"
install -m 755 "$BUNDLE_DIR/adb" "$BUNDLE_DIR/tun2socks" "$STAGE/usr/lib/vpnctl/"
install -m 644 "$BUNDLE_DIR/adb-NOTICE.txt" "$STAGE/usr/share/doc/vpnctl/NOTICE.adb"
install -m 644 "$ROOT/LICENSE" "$STAGE/usr/share/doc/vpnctl/copyright"
cp "$ROOT/packaging/vpnctl.desktop" "$STAGE/usr/share/applications/"
cp "$ROOT/assets/logo.svg" "$STAGE/usr/share/icons/hicolor/scalable/apps/vpnctl.svg"
for size in 16 22 24 32 48 64 128 256 512; do
  mkdir -p "$STAGE/usr/share/icons/hicolor/${size}x${size}/apps"
  cp "$ROOT/assets/png/vpnctl-${size}.png" "$STAGE/usr/share/icons/hicolor/${size}x${size}/apps/vpnctl.png"
done
cp "$ROOT/packaging/org.vpnctl.daemon.policy" "$STAGE/usr/share/polkit-1/actions/"
cp "$ROOT/packaging/vpnctld.service" "$STAGE/lib/systemd/system/"
cp "$ROOT/packaging/config.example.toml" "$STAGE/etc/vpnctl/config.toml"
printf '%s\n' /etc/vpnctl/config.toml > "$STAGE/DEBIAN/conffiles"

cat >"$STAGE/DEBIAN/control" <<EOF
Package: vpnctl
Version: ${VER}
Section: net
Priority: optional
Architecture: ${ARCH}
Depends:
Maintainer: Saeed Shamsi <saeedshams2024@gmail.com>
Description: Use your Android phone VPN on Linux over USB
 System-wide SOCKS bridge via adb forward + tun2socks, with tray and CLI.
EOF

cat >"$STAGE/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
fi
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q /usr/share/icons/hicolor || true
fi
EOF
chmod 755 "$STAGE/DEBIAN/postinst"

cat >"$STAGE/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
case "$1" in
  remove|deconfigure)
    if command -v systemctl >/dev/null 2>&1; then
      systemctl stop vpnctld.service 2>/dev/null || true
    fi
    if [ -x /usr/lib/vpnctl/vpnctld ]; then
      /usr/lib/vpnctl/vpnctld --cleanup
    fi
    ;;
esac
EOF
chmod 755 "$STAGE/DEBIAN/prerm"

cat >"$STAGE/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
fi
if [ "$1" = purge ]; then
  rmdir /etc/vpnctl 2>/dev/null || true
fi
EOF
chmod 755 "$STAGE/DEBIAN/postrm"

dpkg-deb --root-owner-group --build "$STAGE" "$OUT/${PKG}.deb"
(
  OUT="$OUT" bash "$ROOT/scripts/update-checksums.sh" "${PKG}.deb"
)
echo "built $OUT/${PKG}.deb"
