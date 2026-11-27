#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${OUT:-$ROOT/dist}"
if [[ "$#" -eq 0 ]]; then
  echo "usage: update-checksums.sh <file>..." >&2
  exit 2
fi

mkdir -p "$OUT"
tmp="$(mktemp "$OUT/.SHA256SUMS.XXXXXX")"
trap 'rm -f -- "$tmp"' EXIT

if [[ -f "$OUT/SHA256SUMS" ]]; then
  cp "$OUT/SHA256SUMS" "$tmp"
fi

for file in "$@"; do
  if [[ "$file" != "$(basename "$file")" || ! -f "$OUT/$file" ]]; then
    echo "expected an output file directly inside $OUT: $file" >&2
    exit 1
  fi
  if [[ -s "$tmp" ]]; then
    filtered="$tmp.filtered"
    awk -v file="$file" '$2 != file { print }' "$tmp" > "$filtered"
    mv "$filtered" "$tmp"
  fi
  (cd "$OUT" && sha256sum -- "$file") >> "$tmp"
done

mv "$tmp" "$OUT/SHA256SUMS"
