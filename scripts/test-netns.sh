#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "network namespace tests require root (run with sudo)" >&2
  exit 1
fi
if ! command -v ip >/dev/null 2>&1; then
  echo "iproute2 is required" >&2
  exit 1
fi

NS="vpnctl-test-$$"
if ip netns list | grep -q "^${NS}\\b"; then
  echo "refusing to reuse existing network namespace $NS" >&2
  exit 1
fi
cleanup() {
  ip netns del "$NS" 2>/dev/null || true
}
trap cleanup EXIT

ip netns add "$NS"
ip -n "$NS" link set lo up
VPNCTL_NETNS_TEST=1 ip netns exec "$NS" go test ./internal/netcfg -run '^TestNetworkNamespaceRoutes$' -count=1
