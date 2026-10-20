#!/usr/bin/env bash
# Build and package the desktop application on its target platform.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$PROJECT_ROOT"

VERSION="${1:-$(sed -nE 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' internal/buildinfo/buildinfo.go)}"
VERSION="${VERSION#v}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "用法: $0 [Major.Minor.Patch]" >&2
    exit 1
fi

COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
PLATFORM="$(go env GOOS)"
ARCH="$(go env GOARCH)"
ARCHIVE_NAME="Vane-${PLATFORM}-${ARCH}"
DIST_ROOT="build/dist"
DIST_DIR="${DIST_ROOT}/${ARCHIVE_NAME}"
BINARY_NAME="Vane"
if [[ "$PLATFORM" == windows ]]; then
    BINARY_NAME="Vane.exe"
fi

# CGO and desktop libraries require a native toolchain; the platform Taskfile
# owns compiler flags, frontend resources and Windows version resources.
wails3 task build VERSION="$VERSION" COMMIT="$COMMIT"
mkdir -p "$DIST_DIR"
cp "build/bin/${BINARY_NAME}" README.md LICENSE "$DIST_DIR/"

if [[ "$PLATFORM" == windows ]]; then
    python3 - "$DIST_DIR" "${DIST_DIR}.zip" <<'PYEOF'
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile
import sys

source = Path(sys.argv[1])
with ZipFile(sys.argv[2], "w", compression=ZIP_DEFLATED) as archive:
    for path in sorted(source.iterdir()):
        if path.is_file():
            archive.write(path, Path(source.name) / path.name)
PYEOF
    ARCHIVE_PATH="${DIST_DIR}.zip"
else
    ARCHIVE_PATH="${DIST_DIR}.tar.gz"
    tar -czf "$ARCHIVE_PATH" -C "$DIST_ROOT" "$ARCHIVE_NAME"
fi

if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$ARCHIVE_PATH" > "${ARCHIVE_PATH}.sha256"
else
    shasum -a 256 "$ARCHIVE_PATH" > "${ARCHIVE_PATH}.sha256"
fi
echo "构建完成: $ARCHIVE_PATH"
