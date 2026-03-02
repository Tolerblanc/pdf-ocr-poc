#!/usr/bin/env bash
set -euo pipefail

PRINT_SDK=false
if [[ "${1:-}" == "--print-sdk" ]]; then
  PRINT_SDK=true
fi

declare -a SDK_CANDIDATES=()

add_sdk_candidate() {
  local candidate="$1"
  if [[ -z "$candidate" ]]; then
    return 0
  fi
  if [[ ! -d "$candidate" ]]; then
    return 0
  fi
  local existing
  for existing in "${SDK_CANDIDATES[@]-}"; do
    if [[ "$existing" == "$candidate" ]]; then
      return 0
    fi
  done
  SDK_CANDIDATES+=("$candidate")
}

collect_candidates() {
  add_sdk_candidate "${SWIFT_SDK_PATH:-}"

  local default_sdk
  default_sdk="$(xcrun --show-sdk-path 2>/dev/null || true)"
  add_sdk_candidate "$default_sdk"

  local known
  for known in \
    /Library/Developer/CommandLineTools/SDKs/MacOSX15.5.sdk \
    /Library/Developer/CommandLineTools/SDKs/MacOSX15.4.sdk \
    /Library/Developer/CommandLineTools/SDKs/MacOSX15.2.sdk \
    /Library/Developer/CommandLineTools/SDKs/MacOSX15.sdk \
    /Library/Developer/CommandLineTools/SDKs/MacOSX14.5.sdk \
    /Library/Developer/CommandLineTools/SDKs/MacOSX14.sdk; do
    add_sdk_candidate "$known"
  done
}

probe_foundation_compile() {
  local sdk_path="$1"
  local tmp_base
  local tmp_swift
  tmp_base="$(mktemp -t vision-provider-doctor)"
  tmp_swift="${tmp_base}.swift"
  mv "$tmp_base" "$tmp_swift"

  cat > "$tmp_swift" <<'EOF'
import Foundation
print("foundation-ok")
EOF

  local stdout_file
  local stderr_file
  stdout_file="/tmp/vision-provider-doctor.stdout"
  stderr_file="/tmp/vision-provider-doctor.stderr"

  if swiftc -sdk "$sdk_path" "$tmp_swift" -o /tmp/vision-provider-doctor-check >"$stdout_file" 2>"$stderr_file"; then
    /tmp/vision-provider-doctor-check >/dev/null 2>&1 || true
    rm -f /tmp/vision-provider-doctor-check "$tmp_swift" "$stdout_file" "$stderr_file"
    return 0
  fi

  rm -f "$tmp_swift" "$stdout_file"
  return 1
}

select_compatible_sdk() {
  collect_candidates
  local sdk
  for sdk in "${SDK_CANDIDATES[@]}"; do
    if probe_foundation_compile "$sdk"; then
      printf '%s\n' "$sdk"
      return 0
    fi
  done
  return 1
}

if [[ "$PRINT_SDK" == "true" ]]; then
  if selected_sdk="$(select_compatible_sdk)"; then
    printf '%s\n' "$selected_sdk"
    exit 0
  fi
  exit 1
fi

echo "[vision-provider doctor]"
echo

echo "xcode-select path:"
xcode-select -p || true
echo

echo "swift version:"
swift --version || true
echo

echo "swiftc version:"
swiftc --version || true
echo

echo "checking Foundation module import..."
if SELECTED_SDK="$(select_compatible_sdk)"; then
  echo
  echo "doctor: OK"
  echo "compatible_sdk: $SELECTED_SDK"
  if [[ -n "${SWIFT_SDK_PATH:-}" ]]; then
    echo "using SWIFT_SDK_PATH override"
  elif [[ "$SELECTED_SDK" != "$(xcrun --show-sdk-path 2>/dev/null || true)" ]]; then
    echo "note: default SDK is incompatible; fallback SDK selected automatically"
  fi
  exit 0
fi

echo "doctor: FAILED"
echo
if [[ -f /tmp/vision-provider-doctor.stderr ]]; then
  cat /tmp/vision-provider-doctor.stderr || true
else
  echo "no compiler stderr captured"
fi
echo
echo "Possible fixes:"
echo "1) Reinstall/upgrade Command Line Tools to match current swift toolchain"
echo "2) Switch to matching full Xcode toolchain via xcode-select"
echo "3) Set SWIFT_SDK_PATH to a compatible SDK (example: MacOSX15.5.sdk)"
echo "4) Re-run: ./doctor.sh"

rm -f /tmp/vision-provider-doctor.stderr /tmp/vision-provider-doctor-check
exit 1
