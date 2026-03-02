#!/usr/bin/env bash
set -euo pipefail

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
TMP_SWIFT=$(mktemp "/tmp/vision-provider-doctor-XXXXXX.swift")
cat > "$TMP_SWIFT" <<'EOF'
import Foundation
print("foundation-ok")
EOF

if swiftc "$TMP_SWIFT" -o /tmp/vision-provider-doctor-check >/tmp/vision-provider-doctor.stdout 2>/tmp/vision-provider-doctor.stderr; then
  /tmp/vision-provider-doctor-check || true
  echo
  echo "doctor: OK"
  rm -f /tmp/vision-provider-doctor-check "$TMP_SWIFT" /tmp/vision-provider-doctor.stdout /tmp/vision-provider-doctor.stderr
  exit 0
fi

echo "doctor: FAILED"
echo
cat /tmp/vision-provider-doctor.stderr || true
echo
echo "Possible fixes:"
echo "1) Reinstall/upgrade Command Line Tools to match current swift toolchain"
echo "2) Switch to matching full Xcode toolchain via xcode-select"
echo "3) Re-run: ./doctor.sh"

rm -f "$TMP_SWIFT" /tmp/vision-provider-doctor.stdout /tmp/vision-provider-doctor.stderr
exit 1
