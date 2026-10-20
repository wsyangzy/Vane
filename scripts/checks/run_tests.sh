#!/usr/bin/env bash
# Run current Go package tests without relying on historical tests/unit paths.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$PROJECT_ROOT"

MODE=all
RUN_PERFORMANCE=false
RUN_COVERAGE=false
TEST_FLAGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --unit) MODE=unit ;;
        --integration) MODE=integration ;;
        --performance) RUN_PERFORMANCE=true ;;
        --coverage) RUN_COVERAGE=true ;;
        --verbose) TEST_FLAGS+=(-v) ;;
        --help)
            echo "用法: $0 [--unit|--integration] [--performance] [--coverage] [--verbose]"
            echo "默认运行全部 Go 测试；coverage 在 build/test-reports/ 生成报告。"
            exit 0
            ;;
        *) echo "未知参数: $1" >&2; exit 1 ;;
    esac
    shift
done

if [[ ! -f frontend/dist/index.html ]]; then
    echo "缺少 frontend/dist/index.html，请先在 frontend/ 运行 npm run build。" >&2
    exit 1
fi

case "$MODE" in
    all) PACKAGES=(./...) ;;
    unit) PACKAGES=(./internal/... ./cmd/...) ;;
    integration) PACKAGES=(./tests/integration/...) ;;
esac

if [[ "$RUN_COVERAGE" == true ]]; then
    mkdir -p build/test-reports
    go test "${TEST_FLAGS[@]}" -coverprofile=build/test-reports/coverage.out "${PACKAGES[@]}"
    go tool cover -html=build/test-reports/coverage.out -o build/test-reports/coverage.html
else
    go test "${TEST_FLAGS[@]}" "${PACKAGES[@]}"
fi

if [[ "$RUN_PERFORMANCE" == true ]]; then
    go test -run '^$' -bench=. -benchmem "${PACKAGES[@]}"
fi
