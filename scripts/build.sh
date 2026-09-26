#!/usr/bin/env bash
# Build ship-grip-fim into build/ (vet + tests first).
#
#   scripts/build.sh            build for this machine
#   scripts/build.sh --quick    skip vet and tests
#
# The desktop GUI (Fyne) needs cgo and the OpenGL/X11 development packages, so
# cross-compiling is not attempted here; build on (or for) the target platform.
set -euo pipefail
cd "$(dirname "$0")/.."

QUICK=0
[[ "${1:-}" == "--quick" ]] && QUICK=1

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
mkdir -p build

if [[ $QUICK -eq 0 ]]; then
  echo "==> go vet";  go vet ./...
  echo "==> go test"; go test ./...
fi

echo "==> go build ($VERSION)"
go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o build/ship-grip-fim .
ls -l build/ship-grip-fim
