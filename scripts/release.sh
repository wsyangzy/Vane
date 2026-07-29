#!/usr/bin/env bash
# scripts/release.sh - 一键发布新版本
# 用法: ./scripts/release.sh <新版本号> [发布说明]
# 示例: ./scripts/release.sh 7.3a "Codex 全局配置弹窗 + 健康检查对齐"
#
# 版本号格式:
#   Major.Minor[Suffix]       例: 7.3, 7.3a, 7.3b
#   Major.Minor.Patch[Suffix] 例: 7.3.1, 7.3.1a
#
# Suffix 为可选的英文字母后缀（a-z），用于标记小迭代。
# Windows manifest 和 versioninfo.json 中仅使用纯数字部分（X.Y.Z.0）。

set -euo pipefail

# ============================================
# 参数检查
# ============================================
if [ $# -lt 1 ]; then
    cat <<EOF
用法: $0 <新版本号> [发布说明]

参数:
  新版本号    例: 7.3 / 7.3a / 7.3.1 / 7.3.1a
  发布说明    可选，写入版本历史表

示例:
  $0 7.3a
  $0 7.3a "Codex 全局配置弹窗 + 健康检查对齐"

注意:
  - 支持字母后缀（a-z），用于标记小迭代
  - Windows manifest 中仅使用纯数字部分
  - 发布说明含括号或空格时必须用引号包裹
EOF
    exit 1
fi

NEW_VERSION="$1"
RELEASE_NOTE="${2:-未提供发布说明}"

# 校验版本号格式: Major.Minor[Suffix] 或 Major.Minor.Patch[Suffix]
# Suffix 为可选的单个英文字母（a-z/A-Z）
if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?[a-zA-Z]?$ ]]; then
    echo "❌ 错误: 版本号格式无效"
    echo "   支持格式: X.Y / X.Ya / X.Y.Z / X.Y.Za"
    echo "   示例: 7.3, 7.3a, 7.3.1, 7.3.1b"
    exit 1
fi

# 解析版本号：分离数字部分和字母后缀
# 例: "7.3a" → NUMERIC_VERSION="7.3", SUFFIX="a"
# 例: "7.3.1" → NUMERIC_VERSION="7.3.1", SUFFIX=""
SUFFIX=""
NUMERIC_VERSION="$NEW_VERSION"
if [[ "$NEW_VERSION" =~ [a-zA-Z]$ ]]; then
    SUFFIX="${NEW_VERSION: -1}"
    NUMERIC_VERSION="${NEW_VERSION%?}"
fi

# 解析 Major/Minor/Patch（纯数字）
IFS='.' read -ra VERSION_PARTS <<< "$NUMERIC_VERSION"
MAJOR="${VERSION_PARTS[0]}"
MINOR="${VERSION_PARTS[1]}"
PATCH="${VERSION_PARTS[2]:-0}"

# Windows manifest 版本（恰好 4 段纯数字）
WIN_VERSION="${MAJOR}.${MINOR}.${PATCH}.0"

# 项目根目录（脚本位于 scripts/ 下）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# 获取当前日期 (YYYY-MM-DD)
TODAY="$(date +%Y-%m-%d)"

echo "═══════════════════════════════════════════════════════"
echo "  AI Relay Forwarder - 版本发布"
echo "═══════════════════════════════════════════════════════"
echo "  新版本号:      v${NEW_VERSION}"
echo "  数字版本:      ${WIN_VERSION} (Windows manifest)"
echo "  日期:          ${TODAY}"
echo "  说明:          ${RELEASE_NOTE}"
echo "═══════════════════════════════════════════════════════"
echo ""

# ============================================
# 1. 获取当前版本号（从 README.md 提取，支持字母后缀）
# ============================================
CURRENT_VERSION="$(sed -n 's/.*version-\([0-9][0-9]*\.[0-9][0-9]*\(\.[0-9][0-9]*\)\{0,1\}[a-zA-Z]\{0,1\}\)-blue.*/\1/p' README.md | head -1)"
if [ -z "$CURRENT_VERSION" ]; then
    echo "❌ 错误: 无法从 README.md 提取当前版本号"
    exit 1
fi

# 解析当前版本的数字部分（用于 manifest 匹配）
CURRENT_NUMERIC="$CURRENT_VERSION"
if [[ "$CURRENT_VERSION" =~ [a-zA-Z]$ ]]; then
    CURRENT_NUMERIC="${CURRENT_VERSION%?}"
fi
IFS='.' read -ra CUR_PARTS <<< "$CURRENT_NUMERIC"
CUR_MAJOR="${CUR_PARTS[0]}"
CUR_MINOR="${CUR_PARTS[1]}"
CUR_PATCH="${CUR_PARTS[2]:-0}"
CUR_WIN_VERSION="${CUR_MAJOR}.${CUR_MINOR}.${CUR_PATCH}.0"

echo "📋 当前版本: v${CURRENT_VERSION} (manifest: ${CUR_WIN_VERSION})"
echo "📋 目标版本: v${NEW_VERSION} (manifest: ${WIN_VERSION})"
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
echo "📝 [1/13] 更新 README.md ..."
sed "${SED_INPLACE[@]}" "s/version-${CURRENT_VERSION}-blue/version-${NEW_VERSION}-blue/g" README.md

echo "📝 [2/13] 更新 CLAUDE.md ..."
sed "${SED_INPLACE[@]}" "s/🏗️ \*\*架构版本\*\*: v${CURRENT_VERSION}/🏗️ **架构版本**: v${NEW_VERSION}/g" CLAUDE.md
sed "${SED_INPLACE[@]}" "s/- \*\*当前版本\*\*: v${CURRENT_VERSION}/- **当前版本**: v${NEW_VERSION}/g" CLAUDE.md

# 在版本历史表中插入新行
NEW_HISTORY_LINE="| v${NEW_VERSION} | ${TODAY} | ${RELEASE_NOTE} |"
awk -v new_line="${NEW_HISTORY_LINE}" '
    /^\| 版本 \| 日期 \| 主要更新 \|/ { print; getline; print; print new_line; next }
    { print }
' CLAUDE.md > CLAUDE.md.tmp && mv CLAUDE.md.tmp CLAUDE.md

# ============================================
# 3. 更新 Windows 资源版本 (versioninfo.json)
#    数字版本用 MAJOR/MINOR/PATCH，字符串版本用完整 NEW_VERSION
# ============================================
echo "📝 [3/13] 更新 versioninfo.json ..."
python3 - <<PYEOF
import json
with open('versioninfo.json', 'r', encoding='utf-8') as f:
    data = json.load(f)

# FixedFileInfo 中的数字版本（纯数字，4 段）
data['FixedFileInfo']['FileVersion']['Major'] = ${MAJOR}
data['FixedFileInfo']['FileVersion']['Minor'] = ${MINOR}
data['FixedFileInfo']['FileVersion']['Patch'] = ${PATCH}
data['FixedFileInfo']['FileVersion']['Build'] = 0
data['FixedFileInfo']['ProductVersion']['Major'] = ${MAJOR}
data['FixedFileInfo']['ProductVersion']['Minor'] = ${MINOR}
data['FixedFileInfo']['ProductVersion']['Patch'] = ${PATCH}
data['FixedFileInfo']['ProductVersion']['Build'] = 0

# StringFileInfo 中的字符串版本（可含字母后缀，供 Windows 属性页展示）
data['StringFileInfo']['FileVersion'] = '${WIN_VERSION}'
data['StringFileInfo']['ProductVersion'] = '${NEW_VERSION}'

with open('versioninfo.json', 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write('\n')
PYEOF

# ============================================
# 4. 更新 Wails 构建版本 (build/windows/info.json)
# ============================================
echo "📝 [4/13] 更新 build/windows/info.json ..."
python3 - <<PYEOF
import json
with open('build/windows/info.json', 'r', encoding='utf-8') as f:
    data = json.load(f)

# fixed.file_version 必须是 4 段纯数字
data['fixed']['file_version'] = '${WIN_VERSION}'
# ProductVersion 可以是展示用的完整版本号
data['info']['0000']['ProductVersion'] = '${NEW_VERSION}'

with open('build/windows/info.json', 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write('\n')
PYEOF

# ============================================
# 5. 更新 Wails 主配置 (wails.json)
# ============================================
echo "📝 [5/13] 更新 wails.json ..."
sed "${SED_INPLACE[@]}" "s/\"productVersion\": \"${CURRENT_VERSION}\"/\"productVersion\": \"${NEW_VERSION}\"/g" wails.json

# ============================================
# 6. 更新 Wails Dev 配置 (build/config.yml)
# ============================================
echo "📝 [6/13] 更新 build/config.yml ..."
sed "${SED_INPLACE[@]}" "s/version: \"${CURRENT_VERSION}\"/version: \"${NEW_VERSION}\"/g" build/config.yml

# ============================================
# 7. 更新跨平台 Taskfile VERSION (darwin/linux/windows)
# ============================================
echo "📝 [7/13] 更新 build/darwin/Taskfile.yml ..."
sed "${SED_INPLACE[@]}" "s/{{default \"${CURRENT_VERSION}\" .VERSION}}/{{default \"${NEW_VERSION}\" .VERSION}}/g" build/darwin/Taskfile.yml

echo "📝 [8/13] 更新 build/linux/Taskfile.yml ..."
sed "${SED_INPLACE[@]}" "s/{{default \"${CURRENT_VERSION}\" .VERSION}}/{{default \"${NEW_VERSION}\" .VERSION}}/g" build/linux/Taskfile.yml

echo "📝 [9/13] 更新 build/windows/Taskfile.yml ..."
sed "${SED_INPLACE[@]}" "s/{{default \"${CURRENT_VERSION}\" .VERSION}}/{{default \"${NEW_VERSION}\" .VERSION}}/g" build/windows/Taskfile.yml

# ============================================
# 8. 更新 Windows app.manifest（恰好 4 段纯数字）
# ============================================
echo "📝 [10/13] 更新 build/windows/app.manifest ..."
sed "${SED_INPLACE[@]}" "s/version=\"${CUR_WIN_VERSION}\"/version=\"${WIN_VERSION}\"/g" build/windows/app.manifest

# ============================================
# 9. 更新 Go 全局变量 (globals.go)
# ============================================
echo "📝 [11/13] 更新 globals.go ..."
sed "${SED_INPLACE[@]}" "s/Version   = \"${CURRENT_VERSION}\"/Version   = \"${NEW_VERSION}\"/g" globals.go

# ============================================
# 10. 更新 Windows 打包脚本 (scripts/package-windows.sh)
# ============================================
echo "📝 [12/13] 更新 scripts/package-windows.sh ..."
sed "${SED_INPLACE[@]}" "s/VERSION=\"${CURRENT_VERSION}\"/VERSION=\"${NEW_VERSION}\"/g" scripts/package-windows.sh
sed "${SED_INPLACE[@]}" "s/AI Relay Forwarder v${CURRENT_VERSION}/AI Relay Forwarder v${NEW_VERSION}/g" scripts/package-windows.sh

# ============================================
# 11. 根据上一个发布节点后的提交生成新版功能提示
# ============================================
echo "📝 [13/13] 生成 frontend/src/release-notes.json ..."
python3 scripts/generate_release_notes.py \
    --version "$NEW_VERSION" \
    --previous-version "$CURRENT_VERSION" \
    --release-note "$RELEASE_NOTE" \
    --output frontend/src/release-notes.json

# 清理可能产生的 .bak 备份文件（部分 sed 实现会创建）
find . -maxdepth 3 -name "*.bak" -type f -delete 2>/dev/null || true

# ============================================
# 总结输出
# ============================================
echo ""
echo "✅ 版本号已更新到 v${NEW_VERSION}"
echo ""
echo "已修改的 13 个文件:"
echo "  📄 README.md                       (badge: ${CURRENT_VERSION} → ${NEW_VERSION})"
echo "  📄 CLAUDE.md                       (架构版本 + 当前版本 + 版本历史表)"
echo "  📄 versioninfo.json                (Windows 资源: ${WIN_VERSION})"
echo "  📄 build/windows/info.json         (Wails 构建: ${WIN_VERSION})"
echo "  📄 wails.json                      (productVersion: ${NEW_VERSION})"
echo "  📄 build/config.yml                (Dev 模式版本)"
echo "  📄 build/darwin/Taskfile.yml       (darwin VERSION 默认值)"
echo "  📄 build/linux/Taskfile.yml        (linux VERSION 默认值)"
echo "  📄 build/windows/Taskfile.yml      (windows VERSION 默认值)"
echo "  📄 build/windows/app.manifest      (manifest: ${WIN_VERSION})"
echo "  📄 globals.go                      (Go 全局变量 Version: ${NEW_VERSION})"
echo "  📄 scripts/package-windows.sh      (打包脚本 VERSION)"
echo "  📄 frontend/src/release-notes.json      (新版功能 Toast 内容)"
echo ""
echo "💡 后续步骤:"
echo "  1. git diff 检查改动是否正确"
echo "  2. git add -A && git commit -m \"chore(release): 发布 v${NEW_VERSION}\""
echo ""
