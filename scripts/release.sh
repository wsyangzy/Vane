#!/usr/bin/env bash
# scripts/release.sh - 一键发布新版本
# 用法: ./scripts/release.sh <新版本号> [发布说明]
# 示例: ./scripts/release.sh 7.2 "Phase 5/6 流程层重构与长期演进"

set -euo pipefail

# ============================================
# 参数检查
# ============================================
if [ $# -lt 1 ]; then
    cat <<EOF
用法: $0 <新版本号> [发布说明]

参数:
  新版本号    例: 7.2 (Major.Minor 形式)
  发布说明    可选，写入版本历史表

示例:
  $0 7.2
  $0 7.2 "Phase 5/6 流程层重构与长期演进"
EOF
    exit 1
fi

NEW_VERSION="$1"
RELEASE_NOTE="${2:-未提供发布说明}"

# 校验版本号格式 (Major.Minor 或 Major.Minor.Patch)
if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
    echo "❌ 错误: 版本号格式无效，应为 X.Y 或 X.Y.Z (如 7.2 或 7.2.1)"
    exit 1
fi

# 解析 Major/Minor/Patch
IFS='.' read -ra VERSION_PARTS <<< "$NEW_VERSION"
MAJOR="${VERSION_PARTS[0]}"
MINOR="${VERSION_PARTS[1]}"
PATCH="${VERSION_PARTS[2]:-0}"

# 项目根目录（脚本位于 scripts/ 下）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# 获取当前日期 (YYYY-MM-DD)
TODAY="$(date +%Y-%m-%d)"

echo "═══════════════════════════════════════════════════════"
echo "  Claude Request Forwarder - 版本发布"
echo "═══════════════════════════════════════════════════════"
echo "  新版本号: v${NEW_VERSION}"
echo "  日期:    ${TODAY}"
echo "  说明:    ${RELEASE_NOTE}"
echo "═══════════════════════════════════════════════════════"
echo ""

# ============================================
# 1. 获取当前版本号（从 README.md 提取）
# ============================================
# 使用 sed 替代 grep -P（Windows Git Bash 不支持 -P 选项）
CURRENT_VERSION="$(sed -n 's/.*version-\([0-9][0-9]*\.[0-9][0-9]*\(\.[0-9][0-9]*\)\{0,1\}\)-blue.*/\1/p' README.md | head -1)"
if [ -z "$CURRENT_VERSION" ]; then
    echo "❌ 错误: 无法从 README.md 提取当前版本号"
    exit 1
fi
echo "📋 当前版本: v${CURRENT_VERSION}"
echo "📋 目标版本: v${NEW_VERSION}"
echo ""

if [ "$CURRENT_VERSION" = "$NEW_VERSION" ]; then
    echo "⚠️  警告: 目标版本与当前版本相同，无需更新"
    exit 0
fi

# ============================================
# 2. 更新 README.md badge
# ============================================
echo "📝 [1/4] 更新 README.md ..."
sed -i.bak "s/version-${CURRENT_VERSION}-blue/version-${NEW_VERSION}-blue/g" README.md
rm -f README.md.bak

# ============================================
# 3. 更新 CLAUDE.md (架构版本 + 当前版本 + 版本历史表)
# ============================================
echo "📝 [2/4] 更新 CLAUDE.md ..."
# 更新顶部"架构版本"
sed -i.bak "s/🏗️ \*\*架构版本\*\*: v${CURRENT_VERSION}/🏗️ **架构版本**: v${NEW_VERSION}/g" CLAUDE.md
# 更新"项目概述"中的"当前版本"
sed -i.bak "s/- \*\*当前版本\*\*: v${CURRENT_VERSION}/- **当前版本**: v${NEW_VERSION}/g" CLAUDE.md
rm -f CLAUDE.md.bak

# 在版本历史表中插入新行（位于 "| 版本 | 日期 | 主要更新 |" 标题行之后第 2 行）
# 找到第一个数据行（含 "| v" 的行），在其前面插入新行
NEW_HISTORY_LINE="| v${NEW_VERSION} | ${TODAY} | ${RELEASE_NOTE} |"
awk -v new_line="${NEW_HISTORY_LINE}" '
    /^\| 版本 \| 日期 \| 主要更新 \|/ { print; getline; print; print new_line; next }
    { print }
' CLAUDE.md > CLAUDE.md.tmp && mv CLAUDE.md.tmp CLAUDE.md

# ============================================
# 4. 更新 versioninfo.json (Windows 资源版本)
# ============================================
echo "📝 [3/4] 更新 versioninfo.json ..."
# 用 Python 处理 JSON 更安全（保留格式）
python3 - <<PYEOF
import json
with open('versioninfo.json', 'r', encoding='utf-8') as f:
    data = json.load(f)

# FixedFileInfo 中的数字版本
data['FixedFileInfo']['FileVersion']['Major'] = ${MAJOR}
data['FixedFileInfo']['FileVersion']['Minor'] = ${MINOR}
data['FixedFileInfo']['FileVersion']['Patch'] = ${PATCH}
data['FixedFileInfo']['ProductVersion']['Major'] = ${MAJOR}
data['FixedFileInfo']['ProductVersion']['Minor'] = ${MINOR}
data['FixedFileInfo']['ProductVersion']['Patch'] = ${PATCH}

# StringFileInfo 中的字符串版本
data['StringFileInfo']['FileVersion'] = '${NEW_VERSION}.${PATCH}.0' if '${PATCH}' != '0' else '${NEW_VERSION}.0.0'
data['StringFileInfo']['ProductVersion'] = '${NEW_VERSION}.${PATCH}' if '${PATCH}' != '0' else '${NEW_VERSION}.0'

with open('versioninfo.json', 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write('\n')
PYEOF

# ============================================
# 5. 更新 build/windows/info.json (Wails 构建版本)
# ============================================
echo "📝 [4/4] 更新 build/windows/info.json ..."
python3 - <<PYEOF
import json
with open('build/windows/info.json', 'r', encoding='utf-8') as f:
    data = json.load(f)

data['fixed']['file_version'] = '${NEW_VERSION}.${PATCH}.0' if '${PATCH}' != '0' else '${NEW_VERSION}.0.0'
data['info']['0000']['ProductVersion'] = '${NEW_VERSION}'

with open('build/windows/info.json', 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write('\n')
PYEOF

# ============================================
# 6. 总结输出
# ============================================
echo ""
echo "✅ 版本号已更新到 v${NEW_VERSION}"
echo ""
echo "已修改的文件:"
echo "  - README.md (badge: ${CURRENT_VERSION} → ${NEW_VERSION})"
echo "  - CLAUDE.md (架构版本 + 版本历史表)"
echo "  - versioninfo.json (Windows 资源版本)"
echo "  - build/windows/info.json (Wails 构建版本)"
echo ""
echo "💡 后续步骤:"
echo "  1. git diff 检查改动是否正确"
echo "  2. git add README.md CLAUDE.md versioninfo.json build/windows/info.json"
echo "  3. git commit -m \"chore(release): 发布 v${NEW_VERSION}\""
echo ""
