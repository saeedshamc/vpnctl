#!/usr/bin/env bash
# Build vpnctl binaries. Cross-compile from any host with GOOS=linux.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${OUT:-$ROOT/bin}"
mkdir -p "$OUT"

VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X github.com/saeedshamc/vpnctl/internal/version.Version=${VERSION} -X github.com/saeedshamc/vpnctl/internal/version.Commit=${COMMIT} -X github.com/saeedshamc/vpnctl/internal/version.Date=${DATE}"

export CGO_ENABLED=0
export GOOS="${GOOS:-linux}"
export GOARCH="${GOARCH:-amd64}"

echo "building vpnctld → $OUT/vpnctld ($GOOS/$GOARCH)"
go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/vpnctld" "$ROOT/cmd/vpnctld"

# Later phases:
# go build ... -o "$OUT/vpnctl" "$ROOT/cmd/vpnctl"
# go build ... -o "$OUT/vpnctl-gui" "$ROOT/cmd/vpnctl-gui"

echo "ok"
