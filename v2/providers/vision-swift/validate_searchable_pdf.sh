#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SDK_PATH="$($SCRIPT_DIR/doctor.sh --print-sdk 2>/dev/null || true)"

if [[ -z "$SDK_PATH" ]]; then
  echo "validate aborted: no compatible SDK detected" >&2
  exit 1
fi

swift -sdk "$SDK_PATH" "$SCRIPT_DIR/validate_searchable_pdf.swift" "$@"
