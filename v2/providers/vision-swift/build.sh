#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="$SCRIPT_DIR/bin"
mkdir -p "$OUT_DIR"

if ! "$SCRIPT_DIR/doctor.sh" >/tmp/vision-provider-doctor.log 2>&1; then
  cat /tmp/vision-provider-doctor.log
  echo
  echo "build aborted: fix Swift toolchain first"
  exit 1
fi

swiftc "$SCRIPT_DIR/main.swift" -O -o "$OUT_DIR/vision-provider"
echo "built: $OUT_DIR/vision-provider"
