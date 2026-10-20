# 测试入口

Go 单元测试跟随被测包放在 `internal/` 与 `cmd/` 下。`tests/integration/` 保留跨模块的代理请求、重试、Token 保留和重复计费回归；监控模块的测试位于 `internal/platform/monitor/`。

前端测试由 `frontend/` 下的 `npm test` 运行。发布说明生成与发布元数据检查位于 `scripts/checks/`。

在仓库根目录使用 PowerShell 运行：

```powershell
# Go 测试需要 frontend/dist/index.html，首次测试先构建前端。
Push-Location frontend
npm run build
Pop-Location

go test ./...
go test -tags desktop -run '^$' ./...
python -B -m unittest discover -s scripts/checks -p "test_*.py"
```

按范围运行：

```powershell
go test ./internal/providers/claude/...
go test ./internal/providers/codex/...
go test ./internal/tracking/...
go test ./tests/integration/...
```

Bash 环境也可运行 `bash scripts/checks/run_tests.sh`。`--unit` 选择 `internal/` 和 `cmd/`，`--integration` 选择跨模块测试，`--verbose` 展示详情。显式指定 `--coverage` 时在 `build/test-reports/` 生成覆盖率报告；`--performance` 在所选包中运行 Go benchmark。默认执行不创建额外报告文件。

`tests/legacy/react_migration_test.js` 保留旧 `internal/web/static/` 迁移检查的历史代码。它依赖已退出当前产品的 Web 目录和旧测试环境，不属于现行 React/Wails 验收入口，也不要求为它安装依赖。历史检查的字符串匹配和旧性能数据不能用于判定当前功能或性能。

新增单元测试应靠近被测包，跨模块回归放在 `tests/integration/`，测试数据由各包的 `testdata/` 管理。使用 `t.TempDir()` 等测试清理机制隔离数据库和文件；错误分支、重试和计费等行为变更需要有实际输入与输出断言。

GitHub CI 在 Windows x64、macOS ARM64 和 Linux x64 上运行 `go vet ./...`、`go test -tags desktop -run '^$' -timeout 2m ./...` 和 `go test -count=1 -timeout 10m ./...`。前端先完成 lint、测试与生产资源构建，Go 任务使用同一份资源进行 embed 与资源服务验证。Linux 任务安装 GTK4/WebKitGTK 6 开发依赖，并通过 Xvfb 运行测试。

`scripts/checks/` 的发布检查需要 Python；其中的真实脚本行为测试还分别需要 Bash 和 PowerShell 7。GitHub 的 Ubuntu runner 提供这两种 shell。本地缺少相应 shell 时，这部分用例会显示为 skipped。工作流语法使用固定版本的 `actionlint` 检查：`go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7`。

标签发布复用完整 CI；任一检查失败都会阻止后续发布程序编译和 GitHub Release 发布。
