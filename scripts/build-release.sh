#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
RELEASE_DIR="$ROOT_DIR/release"

VERSION="${VERSION:-demo}"
APP_NAME="DMAPC"

targets=(
  "darwin arm64 macos-arm64 dmapc"
  "darwin amd64 macos-amd64 dmapc"
  "windows amd64 windows-amd64 dmapc.exe"
)

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_cmd go
require_cmd zip

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

echo "Running tests..."
(cd "$ROOT_DIR" && go test ./...)

for target in "${targets[@]}"; do
  read -r GOOS GOARCH LABEL BIN_NAME <<<"$target"
  PKG_NAME="${APP_NAME}-${VERSION}-${LABEL}"
  PKG_DIR="$DIST_DIR/$PKG_NAME"

  echo "Building $PKG_NAME..."
  mkdir -p "$PKG_DIR"

  (
    cd "$ROOT_DIR"
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
      -trimpath \
      -ldflags="-s -w" \
      -o "$PKG_DIR/$BIN_NAME" \
      ./cmd/
  )

  cp "$RELEASE_DIR/QUICKSTART.md" "$PKG_DIR/QUICKSTART.md"

  if [[ "$GOOS" == "windows" ]]; then
    cp "$RELEASE_DIR/run-windows.bat" "$PKG_DIR/run-windows.bat"
  else
    cp "$RELEASE_DIR/run-mac.command" "$PKG_DIR/run-mac.command"
    chmod +x "$PKG_DIR/run-mac.command" "$PKG_DIR/$BIN_NAME"
  fi

  (
    cd "$DIST_DIR"
    zip -qr "$PKG_NAME.zip" "$PKG_NAME"
  )

  echo "Created dist/$PKG_NAME.zip"
done

echo ""
echo "Release packages are ready in:"
echo "$DIST_DIR"
