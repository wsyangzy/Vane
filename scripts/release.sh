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

注意:
  发布说明含括号或空格时必须用引号包裹
  示例: $0 7.2 "chore(release): xxx"
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

# 跨平台 sed 选项（macOS 需要 -i ''，Linux/Git Bash 用 -i）
if [[ "$OSTYPE" == "darwin"* ]]; then
    SED_INPLACE=(-i '')
else
    SED_INPLACE=(-i)
fi

# ============================================
# 2. 更新文档类文件 (README.md / CLAUDE.md)
# ============================================
echo "📝 [1/12] 更新 README.md ..."
sed "${SED_INPLACE[@]}" "s/version-${CURRENT_VERSION}-blue/version-${NEW_VERSION}-blue/g" README.md

echo "📝 [2/12] 更新 CLAUDE.md ..."
# 更新顶部"架构版本"
sed "${SED_INPLACE[@]}" "s/🏗️ \*\*架构版本\*\*: v${CURRENT_VERSION}/🏗️ **架构版本**: v${NEW_VERSION}/g" CLAUDE.md
# 更新"项目概述"中的"当前版本"
sed "${SED_INPLACE[@]}" "s/- \*\*当前版本\*\*: v${CURRENT_VERSION}/- **当前版本**: v${NEW_VERSION}/g" CLAUDE.md

# 在版本历史表中插入新行
NEW_HISTORY_LINE="| v${NEW_VERSION} | ${TODAY} | ${RELEASE_NOTE} |"
awk -v new_line="${NEW_HISTORY_LINE}" '
    /^\| 版本 \| 日期 \| 主要更新 \|/ { print; getline; print; print new_line; next }
    { print }
' CLAUDE.md > CLAUDE.md.tmp && mv CLAUDE.md.tmp CLAUDE.md

# ============================================
# 3. 更新 Windows 资源版本 (versioninfo.json)
# ============================================
echo "📝 [3/12] 更新 versioninfo.json ..."
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
file_ver = '${NEW_VERSION}.${PATCH}.0' if '${PATCH}' != '0' else '${NEW_VERSION}.0.0'
prod_ver = '${NEW_VERSION}.${PATCH}' if '${PATCH}' != '0' else '${NEW_VERSION}.0'
data['StringFileInfo']['FileVersion'] = file_ver
data['StringFileInfo']['ProductVersion'] = prod_ver

with open('versioninfo.json', 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write('\n')
PYEOF

# ============================================
# 4. 更新 Wails 构建版本 (build/windows/info.json)
# ============================================
echo "📝 [4/12] 更新 build/windows/info.json ..."
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
# 5. 更新 Wails 主配置 (wails.json)
# ============================================
echo "📝 [5/12] 更新 wails.json ..."
sed "${SED_INPLACE[@]}" "s/\"productVersion\": \"${CURRENT_VERSION}\"/\"productVersion\": \"${NEW_VERSION}\"/g" wails.json

# ============================================
# 6. 更新 Wails Dev 配置 (build/config.yml)
# ============================================
echo "📝 [6/12] 更新 build/config.yml ..."
sed "${SED_INPLACE[@]}" "s/version: \"${CURRENT_VERSION}\"/version: \"${NEW_VERSION}\"/g" build/config.yml

# ============================================
# 7. 更新跨平台 Taskfile VERSION (darwin/linux/windows)
# ============================================
echo "📝 [7/12] 更新 build/darwin/Taskfile.yml ..."
sed "${SED_INPLACE[@]}" "s/{{default \"${CURRENT_VERSION}\" .VERSION}}/{{default \"${NEW_VERSION}\" .VERSION}}/g" build/darwin/Taskfile.yml

echo "📝 [8/12] 更新 build/linux/Taskfile.yml ..."
sed "${SED_INPLACE[@]}" "s/{{default \"${CURRENT_VERSION}\" .VERSION}}/{{default \"${NEW_VERSION}\" .VERSION}}/g" build/linux/Taskfile.yml

echo "📝 [9/12] 更新 build/windows/Taskfile.yml ..."
sed "${SED_INPLACE[@]}" "s/{{default \"${CURRENT_VERSION}\" .VERSION}}/{{default \"${NEW_VERSION}\" .VERSION}}/g" build/windows/Taskfile.yml

# ============================================
# 8. 更新 Windows app.manifest (X.Y.0.0 格式)
# ============================================
echo "📝 [10/12] 更新 build/windows/app.manifest ..."
sed "${SED_INPLACE[@]}" "s/version=\"${CURRENT_VERSION}\.0\.0\"/version=\"${NEW_VERSION}.0.0\"/g" build/windows/app.manifest

# ============================================
# 9. 更新 Go 全局变量 (globals.go)
# ============================================
echo "📝 [11/12] 更新 globals.go ..."
sed "${SED_INPLACE[@]}" "s/Version   = \"${CURRENT_VERSION}\"/Version   = \"${NEW_VERSION}\"/g" globals.go

# ============================================
# 10. 更新 Windows 打包脚本 (scripts/package-windows.sh)
# ============================================
echo "📝 [12/12] 更新 scripts/package-windows.sh ..."
sed "${SED_INPLACE[@]}" "s/VERSION=\"${CURRENT_VERSION}\"/VERSION=\"${NEW_VERSION}\"/g" scripts/package-windows.sh
sed "${SED_INPLACE[@]}" "s/Claude Request Forwarder v${CURRENT_VERSION} - Windows 版本/Claude Request Forwarder v${NEW_VERSION} - Windows 版本/g" scripts/package-windows.sh

# 清理可能产生的 .bak 备份文件（部分 sed 实现会创建）
find . -maxdepth 3 -name "*.bak" -type f -delete 2>/dev/null || true

# ============================================
# 总结输出
# ============================================
echo ""
echo "✅ 版本号已更新到 v${NEW_VERSION}"
echo ""
echo "已修改的 12 个文件:"
echo "  📄 README.md                       (badge: ${CURRENT_VERSION} → ${NEW_VERSION})"
echo "  📄 CLAUDE.md                       (架构版本 + 当前版本 + 版本历史表)"
echo "  📄 versioninfo.json                (Windows 资源版本)"
echo "  📄 build/windows/info.json         (Wails 构建版本)"
echo "  📄 wails.json                      (productVersion)"
echo "  📄 build/config.yml                (Dev 模式版本)"
echo "  📄 build/darwin/Taskfile.yml       (darwin VERSION 默认值)"
echo "  📄 build/linux/Taskfile.yml        (linux VERSION 默认值)"
echo "  📄 build/windows/Taskfile.yml      (windows VERSION 默认值)"
echo "  📄 build/windows/app.manifest      (Windows manifest)"
echo "  📄 globals.go                      (Go 全局变量 Version)"
echo "  📄 scripts/package-windows.sh      (打包脚本 VERSION + README)"
echo ""
echo "💡 后续步骤:"
echo "  1. git diff 检查改动是否正确"
echo "  2. git add -A README.md CLAUDE.md versioninfo.json wails.json globals.go"
echo "     git add -A scripts/package-windows.sh"
echo "     git add -f build/  # build/ 目录可能在 gitignore 中"
echo "  3. git commit -m \"chore(release): 发布 v${NEW_VERSION}\""
echo ""
