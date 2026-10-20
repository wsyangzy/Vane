#!/bin/bash

set -euo pipefail
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$PROJECT_ROOT"
# 刷新 macOS 应用图标缓存

echo "🔄 刷新 macOS 图标缓存..."

# build/darwin/ contains tracked platform resources and must be preserved.

# 2. 清理 macOS 图标缓存
rm -rf ~/Library/Caches/com.apple.iconservices.store
killall Dock 2>/dev/null || true
killall Finder 2>/dev/null || true
echo "  ✅ 清理 macOS 图标缓存并重启 Dock"

# 3. 触摸图标文件更新时间戳
touch assets/vane-app.png
echo "  ✅ 更新图标时间戳"

echo ""
echo "✅ 图标缓存刷新完成！"
echo "💡 请重新运行: wails3 task dev"
