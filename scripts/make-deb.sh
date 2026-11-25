#!/usr/bin/env bash
# Build vpnctl_<ver>_amd64.deb (empty Depends). Requires Linux host tools: dpkg-deb.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="${VERSION:-0.1.0}"
ARCH="${GOARCH:-amd64}"
OUT="${OUT:-$ROOT/dist}"
STAGE="$OUT/deb-root"
PKG="vpnctl_${VER}_${ARCH}"

export CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH"
OUT="$ROOT/bin" "$ROOT/scripts/build.sh"

if [[ ! -x "$ROOT/bundle/adb" || ! -x "$ROOT/bundle/tun2socks" ]]; then
  echo "missing bundle binaries; run scripts/fetch-bundle.sh first" >&2
  exit 1
fi

rm -rf "$STAGE"
mkdir -p \
  "$STAGE/usr/bin" \
  "$STAGE/usr/lib/vpnctl" \
  "$STAGE/usr/share/applications" \
  "$STAGE/usr/share/icons/hicolor/scalable/apps" \
  "$STAGE/usr/share/polkit-1/actions" \
  "$STAGE/lib/systemd/system" \
  "$STAGE/etc/vpnctl" \
  "$STAGE/DEBIAN"

cp "$ROOT/bin/vpnctl" "$STAGE/usr/bin/"
cp "$ROOT/bin/vpnctl-gui" "$STAGE/usr/bin/"
cp "$ROOT/bin/vpnctld" "$STAGE/usr/lib/vpnctl/"
cp "$ROOT/bundle/adb" "$ROOT/bundle/tun2socks" "$STAGE/usr/lib/vpnctl/"
cp "$ROOT/packaging/vpnctl.desktop" "$STAGE/usr/share/applications/"
cp "$ROOT/assets/logo.svg" "$STAGE/usr/share/icons/hicolor/scalable/apps/vpnctl.svg"
cp "$ROOT/packaging/org.vpnctl.daemon.policy" "$STAGE/usr/share/polkit-1/actions/"
cp "$ROOT/packaging/vpnctld.service" "$STAGE/lib/systemd/system/"
cp "$ROOT/packaging/config.example.toml" "$STAGE/etc/vpnctl/config.toml"

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
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop vpnctld.service 2>/dev/null || true
fi
if [ -x /usr/lib/vpnctl/vpnctld ]; then
  /usr/lib/vpnctl/vpnctld --cleanup 2>/dev/null || true
fi
EOF
chmod 755 "$STAGE/DEBIAN/prerm"

mkdir -p "$OUT"
dpkg-deb --build "$STAGE" "$OUT/${PKG}.deb"
(
  cd "$OUT"
  sha256sum "${PKG}.deb" > SHA256SUMS
)
echo "built $OUT/${PKG}.deb"
