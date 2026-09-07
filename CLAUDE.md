# CLAUDE.md

本文件为 Claude Code 提供项目指导信息。

> 📅 **最后更新**: 2026-08-29
> 🏗️ **架构版本**: v8.6
> 📦 **模块数量**: 17 个核心模块（9 个后端 + 8 个前端）

## 项目概述

**AI Relay Forwarder** 是一款基于 Wails 构建的跨平台桌面应用，用于多 AI 提供商 API 请求的智能中继转发、负载均衡和故障恢复。

- **当前版本**: v8.6
- **技术栈**: Go + Wails v3 + React + Vite
- **支持平台**: macOS / Windows / Linux

### 核心功能

- 多端点智能路由与优先级调度
- 自动故障转移与端点自愈
- **故障转移策略 v2** - `loop_retry` 开关 + `max_loops` 循环重试上限
- 请求生命周期追踪与状态管理
- 流式响应处理（SSE）
- **计费感知重试** - 基于 Token 计费智能判断错误可重试性
- 内存热池架构优化数据库写入
- **SQLite 持久化** - 单文件数据库（MySQL 支持已在 v4.1 移除）
- **可视化设置界面** - 枚举类型支持、变更追踪、实时生效

## 快速开始

```bash
# 开发模式运行
wails3 dev

# 构建当前平台
wails3 task build

# 仅构建后端（开发调试用）
wails3 task dev:build

# 运行测试
go test ./...
```

## 项目结构

```
claude-request-forwarder-desktop/        # 注：仓库已更名为 AI-Relay-Forwarder
├── main.go                 # Wails 应用入口
├── globals.go              # 全局变量和共享函数
├── app.go                  # 主应用逻辑，绑定前端 API
├── app_api_*.go           # 前端 API 接口模块
├── Taskfile.yml           # Wails v3 构建配置
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
├── config/                # 运行时配置类型与代码默认值
│   ├── config.go          # 配置结构
│   └── defaults.go        # 默认值
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
| **Endpoint** | `internal/providers/claude/proxy/endpoint/` | 端点管理、健康检查、故障转移 | [📄 CLAUDE.md](internal/providers/claude/proxy/endpoint/CLAUDE.md) |
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
| **运行配置** | 设置页面 + SQLite | 代码默认值初始化，设置页面修改并持久化 |
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

运行配置由代码默认值初始化，并持久化到应用自己的 SQLite 数据库。服务器、故障转移、重试、请求、日志、代理、流式传输和时区等参数在应用内「设置」页面管理；端点、渠道和凭证在对应管理页面维护。仓库和发行包不再读取或提供外部文本配置文件。

### 恢复策略详解

当所有端点都不可用时，系统提供三种恢复策略：

| 策略 | 行为 | 优点 | 缺点 | 推荐场景 |
|------|------|------|------|----------|
| **auto_wait**<br/>（智能等待） | 挂起请求，等待端点自动恢复信号（其他请求成功、冷却期到期） | 避免循环重试，减少无效请求 | 可能等待较长时间（最多 auto_wait_timeout） | **生产环境（推荐）** |
| **loop_retry**<br/>（循环重试） | 回到最高优先级渠道继续尝试（跳过冷却中的端点） | 持续尝试，不会超时 | 可能在渠道间循环重试，浪费资源 | 测试环境或渠道极少场景 |
| **fail_fast**<br/>（快速失败） | 立即返回错误，不等待恢复，不循环重试 | 明确错误信号，快速反馈 | 可能错过即将恢复的端点 | 调试环境或需要明确错误的场景 |

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
go test ./internal/providers/claude/proxy/endpoint/...   # 端点管理
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

## 请求透传约束（重要）

### 流式/非流请求透传规范

**核心原则：反代必须忠实透传客户端的原始请求意图，不得擅自修改请求体中的 `stream` 字段。**

**禁止行为：**
- ❌ 不得向请求体中添加 `"stream": true` 或 `"stream": false`
- ❌ 不得将非流式请求强制转为流式请求
- ❌ 不得将流式请求强制转为非流式请求

**原因：** 部分提供商（如 Anthropic 官方）会检测请求来源，若检测到非 Claude Code CLI 发出的流式请求，会返回"不准在 Claude Code 以外的 CLI 中使用"的错误响应。擅自添加 `stream` 参数会触发此类检测。

**正确处理方式：**
- 客户端发送流式请求 → 路由到支持流式的端点，原样透传
- 客户端发送非流式请求 → 路由到支持非流式的端点，原样透传
- 若端点配置了 `stream_only: true` → 该端点不参与非流式请求的端点选择

### Codex `model` 字段例外（v7.4+）

**例外规则**：`model` 字段属于「路由控制」语义，反代会按当前激活渠道的 `channel.model` 配置覆盖请求体中的 `model` 值。

**为什么算例外**：不同 Codex 服务商支持的模型名集合不同（如 `gpt-5-codex` vs `gpt-5` vs `codex-1`），全局 model 无法兼顾。通过反代按渠道改写，用户切换渠道时无需手动改全局配置。

**不影响其他字段**：除 `model` 外的所有业务字段（`stream`、`input`、`reasoning`、`tools`、`max_tokens` 等）严禁修改，仍遵循「忠实透传客户端原始请求意图」原则。

**实现位置**：
- `internal/codexproxy/model_rewriter.go` → `rewriteModelIfNeeded` 纯函数
- `internal/codexproxy/handler.go` → `ServeHTTP` 接入点

### 隐私脱敏的字节级约束（v8.6+）

隐私脱敏是唯一被允许改写请求体内容的环节，因此它必须比其他环节更严格地遵守透传约束。

**硬性要求：**
- ✅ 未命中任何规则时，转发的 body 与客户端原始请求 **byte-identical**
- ✅ 命中时**只替换命中字段的字节区间**，其余字节（含对象键序、转义形式、数字精度）保持原样
- ❌ 严禁把 body 解析成 `map[string]any` 再 `json.Marshal` 回去 —— 该往返会重排键序、把 `< > &` 转成 Unicode 转义、改变大数字精度，等于伪造客户端请求，属于会触发提供商指纹检测的改动
- ❌ 严禁对替换值做 HTML escape

**实现方式：** `internal/privacy/json_walker.go` 用 `json.Decoder.InputOffset()` 定位字符串 token 的原始字节区间，只重写命中区间。扫描字段由 API path 白名单锁定，协议字段（`model`/`stream`/`tools`/`metadata`/`reasoning`）天然不参与匹配。

**隐私数据红线：** 命中原文只允许出现在本地 `slog` 输出。禁止落库、禁止经 Wails API 回传前端 —— 否则隐私防护功能自身会变成一份客户信息的集中副本。`privacy.RuleHit.Matches` 用 `json:"-"` 在类型层面阻断序列化。

### `stream_only` 端点配置（v8.7 起可在 UI 配置）

部分中转商的 `/v1/messages` 仅支持 SSE 流式，非流式请求会被上游直接拒绝（典型表现为 Cloudflare 520，平均 2 秒即返回；少数走到 60 秒超时后 502）。由于这类请求多为 CLI 后台小任务，失败不会打断主对话，用户往往只能在请求追踪里发现。

勾选端点的「仅支持流式」后，非流式请求不再路由到该端点：从「挨个端点重试、耗 2–60 秒后失败」变为立即跳过。

**配置方式：** 端点管理页面 → 编辑端点 → 勾选「仅支持流式」。默认关闭，不影响既有端点。

**实现位置：**
- `internal/tracking/schema.sql` + `sqlite_adapter.go` → `endpoints.stream_only` 列与旧库迁移
- `internal/providers/claude/store/endpoint.go` → `EndpointRecord.StreamOnly` 读写
- `internal/providers/claude/service/endpoint.go` → 与 `config.EndpointConfig` 双向转换
- `config/config.go` → `EndpointConfig.StreamOnly` 字段
- `internal/providers/claude/proxy/handlers/regular.go` → `filterStreamOnlyEndpoints()` 过滤函数
- `frontend/src/pages/providers/claude/components/EndpointForm.jsx` → UI 开关

> ⚠️ **该配置存于 SQLite `endpoints` 表。** 所有运行配置均由代码默认值初始化，并通过设置页面和 SQLite 管理；应用不再读取或打包外部文本配置。
>
> 该字段曾长期是死配置：结构体、过滤函数、单测俱在，但 DB 无此列、`service` 转换未赋值，导致 `ep.Config.StreamOnly` 恒为 false、过滤函数空转。新增此类跨层字段时务必逐层核对。

**不受影响：** `/v1/messages/count_tokens` 虽也是非流式，但在 `handler.go` 的 `ServeHTTP` 开头即被拦截，走独立的 `CountTokensHandler`（按 `SupportsCountTokens` 选端点），不经过本过滤。

## 计费倍率解析约束（v8.7+）

### 总原则：功能看齐，数据隔离

Claude 与 Codex 在**功能实现上尽量统一看齐**（相同的分层、相同的配置形态、相同的 UI 交互），但在**运行时数据上必须严格隔离**，尤其是定价与计费。

- 凡是两个 provider 共用的内存结构或数据表，其**键必须包含 provider 维度**
- 凡是共用的查找入口，必须**透传 provider**
- 判据：把两侧的渠道名 / 分组名 / 端点名 / 模型名全部设为相同，两边的计费结果必须互不影响

这条原则不是洁癖。两个 provider 的名称空间天然会重叠——同一个中转商往往同时提供 Claude 与 Codex 两种协议，渠道名、分组名、端点名会被用户配成一样；模型名也会（Claude provider 下已存在 `MiniMax-M2` 这类非 Anthropic 模型）。键不含 provider 时，冲突表现为**静默覆盖**：不报错、不告警，只是账单悄悄算错。

### 倍率缓存 key 必须包含 provider

Claude 与 Codex 共用 `UsageTracker.endpointMu` 一张倍率 map。两侧允许配置**同名**的渠道 / 凭证分组 / 端点（用户实际存在 `anyrouter::默认分组::主站直连` 在两个 provider 下同时成立的情况）。

`app.go` 的 `syncEndpointMultipliersToTracker` 先写 Claude 段再写 Codex 段。**若 key 不含 provider，后写的 Codex 条目会静默覆盖 Claude 条目**，导致 Claude 请求按 Codex 的倍率计费。该缺陷曾使 685 条 Claude 请求按 0.4 计价，成本少算 $551.96。

因此 key 结构为 `provider::channel::group::endpoint`，查找回退链：

```
provider::channel::group::endpoint   ← 主 key
provider::channel::endpoint
provider::group::endpoint            ← channel 为空时
provider::endpoint
channel::group::endpoint             ← legacy，仅兼容直接构造 map 的调用点
channel::endpoint
group::endpoint
endpoint
```

写入侧只写 provider 域的 key。`defaultProvider("")` 归一化为 `claude`，因此未传 provider 的旧调用点自动落到 Claude 域。

**新增倍率查找入口时必须透传 provider**，否则会退化回冲突状态。相关位置：
- `internal/tracking/endpoint_multiplier_key.go` → key 构造与回退链
- `app.go` → 写入（Claude 段传 `"claude"`，Codex 段传 `"codex"`）
- `internal/tracking/tracker.go` / `archive_manager.go` → 查找
- `internal/providers/{claude,codex}/proxy/` → 快照调用点

### 定价缓存 key 与 `model_pricing` 唯一键同样包含 provider

原则 0 的直接推论。`model_pricing` 的唯一键是 **`(provider, model_name, service_tier)`** 三元组，不是 `model_name` 单列：

- **provider** —— 两侧模型名空间会重叠。Claude provider 下已存在 `MiniMax-M2` 这类非 Anthropic 模型，说明 Claude 渠道会转发第三方模型；反向同理。旧的 `UNIQUE(model_name)` 让两侧无法配置同名模型的不同定价。
- **service_tier** —— `''` 是标准档兼兜底行，`flex` / `batch` / `fast` 为档位行。档位是模型的固有属性（OpenAI 官方定价即按 model × tier 组织），每行自带 long context 四价，因此「档位 × 上下文」四象限天然成立，不必靠组合规则表达。

定价缓存 key 为 `provider::model::tier`（`internal/tracking/model_pricing_key.go`），查找回退链：

```
provider::model::tier               档位专属定价
provider::model::                   标准档/兜底行
model                               legacy，仅兼容直接构造 map 的调用点
defaultModelPricingKey(provider)    provider 自己的兜底价
```

档位归一化：标准档（含 `default` / `standard_only`）与 `auto` / 未知档一律落到空串——`auto` 意味着上游实际选用的档位无法从请求判定，套标准价是保守做法。

> ⚠️ 兜底定价（`_default` 归 claude、`_codex_default` 归 codex）本身也是 `model_pricing` 的普通记录，入缓存时同样经 `ModelPricingKey` 加前缀。`defaultModelPricingKey` 必须用相同方式构造 key，否则查找会静默落空、全部退到硬编码默认价。
>
> `BatchUpsert` 的 `ON CONFLICT` 子句必须与唯一索引完全一致，否则 SQLite 直接报错而非静默降级。

以 `modelName` 单参数定位的 store 方法（`Get` / `SetDefault`）语义为「标准档主行」；跨 provider 同名模型的精确定位需走 `GetByID` 或带 provider 的方法。

### 端点级倍率已停用，倍率唯一来源是凭证分组

Claude 与 Codex 均不再读取 `endpoints.cost_multiplier` 等端点级倍率字段。这些列保留为兼容存储（前端表单原样回传以免抹掉历史值），但 `app.go` 构造 `EndpointMultiplier` 时只取凭证分组的倍率。

> 端点级倍率的 UI 入口在 v8.0（`67fc4915`）目录重组时被移除，但计算逻辑遗留至 v8.7 才对齐。DB 中残留 12 个非 1 的端点倍率值，因倍率落库链路直到 2026-09-01 才接通，这些值从未影响过任何一条历史成本。

## 调试技巧

- **请求追踪**: 使用 `req-xxxxxxxx` 格式的请求 ID 过滤日志
- **端点状态**: 应用内「端点管理」页面查看健康状态
- **性能监控**: 应用内「概览」页面查看热池统计

## 版本历史

| 版本 | 日期 | 主要更新 |
|------|------|----------|
| v8.6 | 2026-08-29 | 隐私防护重建：字节级透传脱敏、明文零落库、仅检测模式、内置 PII 强校验、规则批量管理；凭证分组分段条；Codex 多维计费 |
| v8.5 | 2026-07-29 | 凭证分组每日成本限额与详情、隐私脱敏强化、Claude/Codex 故障转移一致性与首次配置优化、Provider 架构收敛、新版功能分类提示 |
| v8.4 | 2026-07-15 | 未提供发布说明 |
| v8.3 | 2026-07-12 | 未提供发布说明 |
| v8.2 | 2026-07-10 | 未提供发布说明 |
| v8.1 | 2026-07-09 | 未提供发布说明 |
| v8.0 | 2026-07-08 | 未提供发布说明 |
| v7.9a | 2026-07-02 | 未提供发布说明 |
| v7.9 | 2026-06-27 | 未提供发布说明 |
| v7.8b | 2026-06-27 | 未提供发布说明 |
| v7.9a | 2026-06-27 | 未提供发布说明 |
| v7.8a | 2026-06-27 | 未提供发布说明 |
| v7.8 | 2026-06-27 | 未提供发布说明 |
| v7.7 | 2026-06-26 | 未提供发布说明 |
| v7.6 | 2026-06-26 | 未提供发布说明 |
| v7.5a | 2026-06-26 | 未提供发布说明 |
| v7.5 | 2026-06-25 | 未提供发布说明 |
| v7.4c | 2026-06-12 | 未提供发布说明 |
| v7.4b | 2026-06-11 | 未提供发布说明 |
| v7.4a | 2026-06-08 | 未提供发布说明 |
| v7.4 | 2026-06-06 | 未提供发布说明 |
| v7.3c | 2026-06-02 | 未提供发布说明 |
| v7.3b | 2026-05-29 | 未提供发布说明 |
| v7.3a | 2026-05-29 | 未提供发布说明 |
| v7.3 | 2026-05-28 | 未提供发布说明 |
| v7.2 | 2026-05-28 | 未提供发布说明 |
| v7.3 | 2026-05-27 | 品牌升级：更名为 AI Relay Forwarder，新 Logo，数据目录自动迁移，仓库重命名 |
| v7.2 | 2026-05-25 | Phase 5/6 流程层重构与长期演进（全局重试预算、健康检查节流、配置统一管理等） |
| v7.1 | 2026-05-14 | Codex 渠道-端点两层管理、配置同步稳定性修复、SQLite 写连接污染修复、日志去抖 |
| v7.0 | 2026-03-19 | 流式/非流式透传与计费误判修复、模态遮挡修复、请求追踪展示优化 |
| v6.9 | 2026-03-14 | Codex 提供商管理与 1M 上下文能力、表单状态修复、导入工具 SQLite 兼容 |
| v6.8 | 2026-03-10 | SQLite 适配器读写连接分离、归档写事务串行化、示例配置注释清理 |
| v6.7 | 2026-02-11 | 端点链路结构化存储（`{hop,channel,endpoint}` JSON）、链路 Tab 纵向时间线重构、版本号同步修复 |
| v6.5 | 2026-02-10 | 用户菜单、版本信息优化 |
| v6.4 | 2026-02-06 | **Wails v3 升级**：原生系统托盘、Taskfile 构建系统、前端 runtime 迁移 |
| v6.0.7 | 2026-01-07 | 智能恢复策略（auto_wait / loop_retry / fail_fast），配置简化（KISS & YAGNI） |
| v6.0.5 | 2025-12-29 | 计费感知重试，日志复制功能 |
| v5.0.0 | 2025-12-08 | Wails 桌面应用重构，移除服务器版本 |
| v4.1.0 | 2025-12-04 | 内存热池架构，80% 数据库写入优化 |
| v4.0.0 | 2025-12-03 | 配置简化，移除"组"概念 |
| v3.5.0 | 2025-10-12 | 状态机重构，MySQL 支持 |

详细更新记录请参阅 `CHANGELOG.md`。
