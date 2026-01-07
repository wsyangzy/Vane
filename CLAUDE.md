# CLAUDE.md

本文件为 Claude Code 提供项目指导信息。

> 📅 **最后更新**: 2026-01-07 20:00:00
> 🏗️ **架构版本**: v6.0.7
> 📦 **模块数量**: 17 个核心模块（9 个后端 + 8 个前端）

## 项目概述

**CC-Forwarder Desktop** 是一款基于 Wails 构建的跨平台桌面应用，用于 Claude API 请求的智能转发、负载均衡和故障恢复。

- **当前版本**: v6.0.7
- **技术栈**: Go + Wails v2 + React + Vite
- **支持平台**: macOS / Windows / Linux

### 核心功能

- 多端点智能路由与优先级调度
- 自动故障转移与端点自愈
- **智能恢复策略** (v6.0.7+) - 三种策略可选（auto_wait / loop_retry / fail_fast）
- 请求生命周期追踪与状态管理
- 流式响应处理（SSE）
- **计费感知重试** (v6.0.5+) - 基于 Token 计费智能判断错误可重试性
- 内存热池架构优化数据库写入
- SQLite/MySQL 双数据库支持
- **可视化设置界面** (v6.0.7+) - 枚举类型支持、变更追踪、实时生效

## 快速开始

```bash
# 开发模式运行
wails dev

# 构建当前平台
wails build

# 构建 Windows 版本（交叉编译）
wails build -platform windows/amd64

# 构建 macOS 版本
wails build -platform darwin/amd64

# 运行测试
go test ./...
```

## 项目结构

```
claude-request-forwarder-desktop/
├── main.go                 # Wails 应用入口
├── app.go                  # 主应用逻辑，绑定前端 API
├── app_api_*.go           # 前端 API 接口模块
├── wails.json             # Wails 配置
├── frontend/              # React 前端
│   ├── src/
│   │   ├── pages/         # 页面组件
│   │   │   ├── overview/  # 概览页面
│   │   │   ├── endpoints/ # 端点管理
│   │   │   ├── requests/  # 请求追踪
│   │   │   ├── settings/  # 设置页面
│   │   │   └── ...
│   │   └── wailsjs/       # Wails 自动生成的绑定
│   └── dist/              # 构建输出
├── internal/              # 核心业务逻辑
│   ├── proxy/             # 请求转发引擎
│   │   ├── handler.go     # HTTP 请求协调器
│   │   ├── handlers/      # 专用处理器（streaming/regular）
│   │   ├── response/      # 响应处理与 Token 分析
│   │   ├── lifecycle_manager.go  # 状态机生命周期管理
│   │   └── error_recovery.go     # 智能错误恢复
│   ├── endpoint/          # 端点管理与健康检查
│   ├── tracking/          # 使用量追踪与数据库
│   │   ├── hot_pool.go    # 内存热池
│   │   ├── archive_manager.go    # 归档管理器
│   │   └── *_adapter.go   # 数据库适配器（SQLite/MySQL）
│   ├── events/            # 事件系统
│   ├── logging/           # 日志系统
│   └── service/           # 代理服务
├── config/                # 配置文件
│   ├── config.yaml        # 主配置
│   └── example*.yaml      # 配置示例
├── build/                 # 构建相关资源
│   ├── bin/               # 编译输出
│   ├── windows/           # Windows 资源
│   └── darwin/            # macOS 资源
└── docs/                  # 文档
```

## 架构全景图

```mermaid
graph TB
    subgraph "Frontend (React)"
        Pages["📱 Pages"]
        Overview["概览 Overview"]
        Channels["渠道 Channels"]
        Requests["请求追踪 Requests"]
        Settings["设置 Settings"]
        Pricing["定价 Pricing"]
        LogViewer["日志查看器"]

        Pages --> Overview
        Pages --> Channels
        Pages --> Requests
        Pages --> Settings
        Pages --> Pricing
        Pages --> LogViewer
    end

    subgraph "Backend (Go)"
        App["🎯 App Entry<br/>(app.go)"]

        subgraph "Core Modules"
            Proxy["🔄 Proxy<br/>请求转发引擎"]
            Endpoint["🎯 Endpoint<br/>端点管理"]
            Tracking["📊 Tracking<br/>使用追踪"]
            Service["⚙️ Service<br/>业务服务层"]
            Store["💾 Store<br/>数据持久化"]
        end

        subgraph "Support Modules"
            Events["📡 Events<br/>事件系统"]
            Logging["📝 Logging<br/>日志系统"]
            Middleware["🔐 Middleware<br/>中间件"]
            Monitor["📈 Monitor<br/>监控"]
            Tray["🖼️ Tray<br/>托盘管理"]
        end
    end

    subgraph "Storage"
        SQLite["SQLite DB"]
        MySQL["MySQL DB"]
        HotPool["🔥 Memory Hot Pool"]
    end

    Pages <-->|Wails Bindings| App
    App --> Proxy
    App --> Service

    Proxy --> Endpoint
    Proxy --> Tracking
    Proxy --> Events

    Service --> Store
    Service --> Endpoint

    Tracking --> HotPool
    HotPool --> SQLite
    HotPool --> MySQL

    Store --> SQLite

    Middleware --> Logging
    Events --> Logging

    style Proxy fill:#4a9eff
    style Endpoint fill:#50c878
    style Tracking fill:#ff6b6b
    style HotPool fill:#ffd700
```

## 模块索引

### 🎯 后端核心模块

| 模块 | 路径 | 职责 | 详细文档 |
|------|------|------|----------|
| **Endpoint** | `internal/endpoint/` | 端点管理、健康检查、故障转移 | [📄 CLAUDE.md](internal/endpoint/CLAUDE.md) |
| **Proxy** | `internal/proxy/` | 请求转发、流式处理、错误恢复 | [📄 CLAUDE.md](internal/proxy/CLAUDE.md) |
| **Tracking** | `internal/tracking/` | 使用追踪、热池架构、数据归档 | [📄 CLAUDE.md](internal/tracking/CLAUDE.md) |
| **Service** | `internal/service/` | 业务逻辑层（渠道/端点/定价） | [📄 CLAUDE.md](internal/service/CLAUDE.md) |
| **Store** | `internal/store/` | 数据持久化层（SQLite 访问） | [📄 CLAUDE.md](internal/store/CLAUDE.md) |
| **Events** | `internal/events/` | 事件总线系统 | 核心文件: `bus.go` |
| **Logging** | `internal/logging/` | 日志广播与事件发射 | 核心文件: `broadcast_handler.go` |
| **Middleware** | `internal/middleware/` | 认证、日志、监控中间件 | 核心文件: `auth.go` |
| **Monitor** | `internal/monitor/` | 系统监控 | 核心文件: `monitor.go` |

### 📱 前端页面模块

| 页面 | 路径 | 功能描述 | 详细文档 |
|------|------|----------|----------|
| **Overview** | `frontend/src/pages/overview/` | KPI 概览、图表展示 | [📄 CLAUDE.md](frontend/src/pages/CLAUDE.md#overview) |
| **Channels** | `frontend/src/pages/channels/` | 渠道管理、优先级配置 | [📄 CLAUDE.md](frontend/src/pages/CLAUDE.md#channels) |
| **Requests** | `frontend/src/pages/requests/` | 请求实时追踪与过滤 | [📄 CLAUDE.md](frontend/src/pages/CLAUDE.md#requests) |
| **Settings** | `frontend/src/pages/settings/` | 系统配置管理 | [📄 CLAUDE.md](frontend/src/pages/CLAUDE.md#settings) |
| **Pricing** | `frontend/src/pages/pricing/` | 模型定价管理 | [📄 CLAUDE.md](frontend/src/pages/CLAUDE.md#pricing) |
| **Log Viewer** | `frontend/src/pages/log-viewer/` | 实时日志查看器 | [📄 CLAUDE.md](frontend/src/pages/CLAUDE.md#log-viewer) |

### 🔧 配置与构建

| 类型 | 路径 | 说明 |
|------|------|------|
| **配置文件** | `config/` | YAML 配置管理 |
| **构建资源** | `build/` | 图标、manifest、平台资源 |

## 核心架构

### Wails 应用架构

```
┌─────────────────────────────────────────────────────┐
│                   Wails Runtime                      │
├─────────────────────────────────────────────────────┤
│  Frontend (React)          │    Backend (Go)        │
│  ├── Pages                 │    ├── App Struct      │
│  ├── Components            │    ├── Proxy Service   │
│  └── Wails Bindings  ←────→│    ├── Tracking        │
│                            │    └── Endpoint Mgr    │
└─────────────────────────────────────────────────────┘
```

### 请求处理流程

```
请求接收 → 端点选择 → 转发处理 → 响应分析 → 状态更新
    ↓         ↓          ↓          ↓          ↓
  Pending → Forwarding → Streaming → Processing → Completed
                    ↓
              错误恢复 → 重试/故障转移
```

### 状态机

```
业务状态: pending → forwarding → streaming → processing → completed/failed/cancelled
错误状态: retry / suspended（与业务状态独立）
```

## 配置说明

主配置文件: `config/config.yaml`

```yaml
# 代理服务
server:
  host: "127.0.0.1"
  port: 9090

# 端点配置
endpoints:
  - name: "primary"
    url: "https://api.anthropic.com"
    channel: "official"
    priority: 1
    failover_enabled: true
    token: "sk-xxx"

  - name: "backup"
    url: "https://api.example.com"
    channel: "third-party"
    priority: 2
    failover_enabled: true
    token: "sk-yyy"

# 故障转移配置
failover:
  enabled: true
  default_cooldown: "10m"

# 请求控制 (v6.0.7+)
request:
  # 恢复策略：所有渠道耗尽后的处理方式
  recovery_strategy: "auto_wait"  # auto_wait (推荐) | loop_retry | fail_fast

  # 智能等待参数（仅 auto_wait 策略生效）
  auto_wait_timeout: "300s"        # 最大等待时间（默认5分钟）
  auto_wait_max_requests: 100      # 最大挂起请求数（默认100）

# 内存热池（可选）
hot_pool:
  enabled: true
  max_active_requests: 1000
```

### 恢复策略详解（v6.0.7+）

当所有端点都不可用时，系统提供三种恢复策略：

| 策略 | 行为 | 优点 | 缺点 | 推荐场景 |
|------|------|------|------|----------|
| **auto_wait**<br/>（智能等待） | 挂起请求，等待端点自动恢复信号（其他请求成功、冷却期到期） | 避免循环重试，减少无效请求 | 可能等待较长时间（最多 auto_wait_timeout） | **生产环境（推荐）** |
| **loop_retry**<br/>（循环重试） | 回到最高优先级渠道继续尝试（跳过冷却中的端点） | 持续尝试，不会超时 | 可能在渠道间循环重试，浪费资源 | 测试环境或渠道极少场景 |
| **fail_fast**<br/>（快速失败） | 立即返回错误，不等待恢复，不循环重试 | 明确错误信号，快速反馈 | 可能错过即将恢复的端点 | 调试环境或需要明确错误的场景 |

**向后兼容**：旧版本配置（v6.0.x 之前）会自动迁移到新的 `recovery_strategy` 格式。


## 开发命令

```bash
# 前端开发
cd frontend && npm run dev
```

### 测试策略（重要）

**⚠️ 不要直接运行 `go test ./...`！** 集成测试包含大量 `time.Sleep` 等待（累计 20-30 秒），会严重拖慢开发效率。

```bash
# ✅ 推荐：只运行修改相关的单元测试
go test ./internal/proxy/...      # 代理模块
go test ./internal/endpoint/...   # 端点管理
go test ./internal/tracking/...   # 使用量追踪
go test ./config/...              # 配置模块

# ✅ 运行特定测试函数
go test ./internal/proxy/... -run "TestErrorRecovery"

# ⏳ 集成测试（耗时较长，提交前或 CI 运行）
go test ./tests/integration/...

# 带竞态检测（完整测试时使用）
go test -race ./internal/...
```

**耗时测试文件**（仅在需要时运行）:
- `tests/integration/*_duplicate_billing_*` - 重复计费保护测试
- `tests/integration/*_streaming_*` - 流式处理测试

## 设计模式

- **状态机模式**: 请求生命周期管理
- **适配器模式**: 多数据库支持
- **工厂模式**: 请求处理器创建
- **断路器模式**: 健康检查与故障转移

## 调试技巧

- **请求追踪**: 使用 `req-xxxxxxxx` 格式的请求 ID 过滤日志
- **端点状态**: 应用内「端点管理」页面查看健康状态
- **性能监控**: 应用内「概览」页面查看热池统计

## 版本历史

| 版本 | 日期 | 主要更新 |
|------|------|----------|
| v6.0.7 | 2026-01-07 | 智能恢复策略（auto_wait / loop_retry / fail_fast），配置简化（KISS & YAGNI） |
| v6.0.5 | 2025-12-29 | 计费感知重试，日志复制功能 |
| v5.0.0 | 2025-12-08 | Wails 桌面应用重构，移除服务器版本 |
| v4.1.0 | 2025-12-04 | 内存热池架构，80% 数据库写入优化 |
| v4.0.0 | 2025-12-03 | 配置简化，移除"组"概念 |
| v3.5.0 | 2025-10-12 | 状态机重构，MySQL 支持 |

详细更新记录请参阅 `CHANGELOG.md`。
