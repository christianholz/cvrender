#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="$ROOT_DIR/src"

cd "$SRC_DIR"

BIN_NAME="cvrender"
if [[ "${GOOS:-}" == "windows" ]]; then
  BIN_NAME="cvrender.exe"
fi

OUT_PATH="${1:-$ROOT_DIR/dist/$BIN_NAME}"
OUT_DIR="$(dirname "$OUT_PATH")"

echo "==> Resolving modules"
go mod tidy

echo "==> Compiling source packages"
go build ./...

echo "==> Building standalone binary"
mkdir -p "$OUT_DIR"
CGO_ENABLED="${CGO_ENABLED:-0}" go build -trimpath -ldflags="-s -w" -o "$OUT_PATH" ./cmd/cvrender

echo "==> Build complete: $OUT_PATH"
