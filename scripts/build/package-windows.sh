#!/usr/bin/env bash
# Package the Windows release build with the files needed for distribution.

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$PROJECT_ROOT"

VERSION="1.0.1"
APP_NAME="Vane"
PACKAGE_NAME="Vane-Windows-v${VERSION}"
DIST_ROOT="build/dist"
DIST_DIR="${DIST_ROOT}/${PACKAGE_NAME}"
ZIP_PATH="${DIST_ROOT}/${PACKAGE_NAME}.zip"
EXE_PATH="build/bin/${APP_NAME}.exe"

if [ ! -f "$EXE_PATH" ]; then
    echo "错误: 找不到 Windows 可执行文件: $EXE_PATH" >&2
    echo "请先运行: wails3 task build VERSION=${VERSION}" >&2
    exit 1
fi

echo "正在打包 ${APP_NAME} v${VERSION} Windows 版本..."
rm -rf "$DIST_DIR" "$ZIP_PATH" "${ZIP_PATH}.sha256"
mkdir -p "$DIST_DIR/data" "$DIST_DIR/logs"

cp "$EXE_PATH" "$DIST_DIR/${APP_NAME}.exe"

cat > "$DIST_DIR/README.txt" <<EOF
Vane v${VERSION} - Windows x64
============================================

快速开始

1. 双击 Vane.exe 启动应用。
2. 应用数据、运行配置和日志默认保存在用户 AppData 目录。
3. 在应用内配置 Claude 或 Codex 渠道、凭证分组和端点。

系统要求

- Windows 10/11 x64
- Microsoft Edge WebView2 Runtime

目录说明

- data/: 便携数据目录预留
- logs/: 便携日志目录预留
EOF

python3 - "$DIST_DIR" "$ZIP_PATH" <<'PYEOF'
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile
import sys

source = Path(sys.argv[1])
target = Path(sys.argv[2])
with ZipFile(target, "w", compression=ZIP_DEFLATED, compresslevel=9) as archive:
    for path in sorted(source.rglob("*")):
        if path.is_file():
            archive.write(path, Path(source.name) / path.relative_to(source))
PYEOF

sha256sum "$ZIP_PATH" > "${ZIP_PATH}.sha256"

echo "Windows 发布包创建完成:"
echo "  目录: $DIST_DIR"
echo "  压缩包: $ZIP_PATH"
echo "  校验: ${ZIP_PATH}.sha256"
ls -lh "$ZIP_PATH" "${ZIP_PATH}.sha256"
