# AI Relay Forwarder

> 原名 Claude Request Forwarder，v7.3 起更名为 AI Relay Forwarder 以反映多 AI 提供商支持。

<p align="center">
  <img src="build/appicon.png" width="128" height="128" alt="AI Relay Forwarder">
</p>

<p align="center">
  <strong>AI API 智能中继转发代理</strong><br>
  多端点负载均衡 · 自动故障转移 · 实时使用统计
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-8.8-blue.svg" alt="Version">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey.svg" alt="Platform">
  <img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8.svg" alt="Go">
  <img src="https://img.shields.io/badge/React-18-61DAFB.svg" alt="React">
</p>

---

## 概述

AI Relay Forwarder 是一款基于 [Wails](https://wails.io) 构建的跨平台桌面应用，专为多 AI 提供商 API 用户设计。它作为本地代理运行，提供智能请求转发、多端点管理、自动故障恢复等企业级功能，同时记录完整的使用统计和成本数据。

### 为什么需要它？

- **多账号/多端点管理** - 统一管理多个 API 端点，无需频繁切换配置
- **高可用保障** - 主端点故障时自动切换到备用端点，确保服务不中断
- **成本透明** - 实时追踪 Token 用量和费用，支持不同端点设置成本倍率
- **请求可观测** - 完整的请求生命周期追踪，便于问题排查

![overview](images/overview.png)

## 功能特性

### 🚀 智能转发引擎

- **优先级路由** - 按优先级自动选择最优端点
- **故障转移** - 端点异常时自动切换，支持配置冷却时间
- **故障转移策略 v2** - `loop_retry` 开关 + `max_loops` 循环重试上限
- **全局重试预算**（新增）- 总尝试次数与总时长双重上限，避免三层重试导致客户端 socket 失效
- **健康检查节流**（新增）- 5s 窗口防止多触发源并发探活风暴
- **端点自愈** - 持续监测故障端点，恢复后自动重新启用
- **流式传输** - 完整支持 SSE 流式响应，零延迟透传
- **计费感知重试** - 基于 Token 计费智能判断错误是否可重试，避免重复计费

### 📊 使用统计

- **实时监控** - Token 用量、请求成功率、响应时间一目了然
- **成本追踪** - 自动计算费用，支持按端点设置成本倍率
- **历史记录** - 完整的请求日志，支持筛选和导出
- **可视化图表** - 请求趋势图，直观展示使用情况
- **系统指标 API**（新增）- `GetSystemMetrics()` 暴露 HotPool 状态、内存使用、健康端点数等运行指标

### 🎛️ 端点管理

- **可视化配置** - 图形界面管理端点，无需手动编辑文本配置
- **渠道分组** - 按渠道标签组织端点，清晰分类
- **状态监控** - 实时显示端点健康状态和响应延迟
- **灵活配置** - 支持自定义请求头、超时时间、成本倍率等

### 🔧 其他特性

- **请求生命周期追踪** - 从接收到完成的全程状态管理
- **请求状态枚举**（新增）- 状态转移合法性校验，避免非法状态变更
- **热池架构** - 内存缓存 + 异步写入，高性能低延迟
- **热池满告警**（新增）- 数据写入压力大时主动 Toast 提示
- **持久化失败队列**（新增）- HotPool 失败时自动降级到本地 JSONL，杜绝静默丢数据
- **本地存储** - SQLite 数据库，无需额外依赖
- **数据库自维护**（新增）- WAL checkpoint 调优、增量 VACUUM、分批清理过期记录
- **跨平台** - macOS、Windows、Linux 全平台支持
- **可视化设置** - 设置页面支持枚举类型下拉选择、变更追踪、实时生效
- **配置统一管理**（新增）- Proxy / Logging / Timezone 等运行配置统一由设置 UI 管理并持久化到 SQLite
- **启动状态机**（新增）- 明确 Starting / Running / Degraded / Failed 状态，降级时停止无用后台任务
- **崩溃恢复**（新增）- 启动时自动扫描僵尸请求并标记为 `crashed_during_processing`
- **日志复制** - 日志查看器支持一键复制，便于问题排查

## 截图

|              概览              |             Claude 管理             |
| :----------------------------: | :------------------------------: |
| ![overview](images/overview.png) | ![endpoints](images/endpoints.png) |

|            请求追踪            |              设置              |
| :----------------------------: | :----------------------------: |
| ![requests](images/requests.png) | ![settings](images/settings.png) |

## 快速开始

### 方式一：下载安装包

从 [Releases](https://github.com/xiaozhaodong/AI-Relay-Forwarder/releases) 页面下载：

| 平台                  | 文件                                            |
| --------------------- | ----------------------------------------------- |
| macOS (Intel)         | `AI-Relay-Forwarder-darwin-amd64.zip`   |
| macOS (Apple Silicon) | `AI-Relay-Forwarder-darwin-arm64.zip`   |
| Windows               | `AI-Relay-Forwarder-windows-amd64.zip`  |
| Linux                 | `AI-Relay-Forwarder-linux-amd64.tar.gz` |

### 方式二：从源码构建

```bash
# 1. 安装 Wails v3 CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@latest

# 2. 克隆项目
git clone https://github.com/xiaozhaodong/AI-Relay-Forwarder.git
cd AI-Relay-Forwarder

# 3. 安装前端依赖
cd frontend && npm install && cd ..

# 4. 开发模式运行
wails3 dev
wails3 task dev:build    # 开发构建

# 5. 构建生产版本
wails3 task build
```

### 配置 Claude Code

启动应用后，在 Claude Code 中设置代理地址：

```bash
# 设置 API 代理地址（默认端口 9090）
claude config set --global apiBaseUrl http://127.0.0.1:9090
```

## 配置说明

### 端点配置

端点通过应用内「Claude 管理」页面进行管理，支持以下配置项：

| 配置项   | 说明                 | 示例                          |
| -------- | -------------------- | ----------------------------- |
| 渠道     | 分组标签             | `官方`、`第三方`          |
| 名称     | 唯一标识（不可修改） | `claude-primary`            |
| URL      | API 端点地址         | `https://api.anthropic.com` |
| Token    | Bearer Token         | `sk-ant-xxx`                |
| 优先级   | 数字越小优先级越高   | `1`                         |
| 故障转移 | 是否参与自动切换     | `启用`                      |
| 成本倍率 | 费用计算倍率         | `1.0`                       |

### 全局配置

运行配置由代码默认值初始化，并持久化到应用自己的 SQLite 数据库。启动后在应用内「设置」页面修改服务器、故障转移、重试、日志、代理、流式传输和时区等参数；端点、渠道和凭证也只通过管理页面维护。发行包不需要额外的配置文件。

#### 故障转移策略

> 旧版本的"三种恢复策略"（auto_wait / loop_retry / fail_fast）已合并简化为更简洁的 `loop_retry` 开关 + `max_loops` 上限。

| 配置                       | 行为                                                       | 适用场景             |
| -------------------------- | ---------------------------------------------------------- | -------------------- |
| `loop_retry: false`        | 所有渠道耗尽后立即返回错误                                 | 调试环境             |
| `loop_retry: true`         | 回到最高优先级渠道继续尝试，最多 `max_loops` 轮            | **生产环境（推荐）** |

#### 错误状态码

**反代不自造状态码。** 上游返回过响应时，原样透传提供商的状态码；只有在上游从未响应（纯本地失败）时，才按 HTTP 标准语义映射：

| 状态码        | 含义                  | 触发场景                                       |
| ------------- | --------------------- | ---------------------------------------------- |
| *透传*        | 提供商原始状态码      | 上游返回过响应（含 429 限流、5xx 等）          |
| **503** | Service Unavailable   | 上游原样透传，或代理当前无可用端点                     |
| **502** | Bad Gateway           | 所有渠道转移完毕仍失败，且上游从未响应         |
| **504** | Gateway Timeout       | 挂起等待端点恢复超时；全局重试预算耗尽         |
| **499** | Client Closed Request | 用户主动取消请求                               |
| **403** | Forbidden             | 本地隐私策略 fail-closed 拒发                  |
| **400** | Bad Request           | 请求体无法读取或解析                           |

> 历史版本曾用 422 阻止 Claude Code 自动重试。该做法已移除——提供商从不返回 422，伪造它会让请求追踪里的状态码失去诊断价值。

## 技术架构

```
┌──────────────────────────────────────────────────────────────┐
│                    AI Relay Forwarder                        │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────────────┐      ┌─────────────────────────┐    │
│  │   Frontend (React)  │      │     Backend (Go)        │    │
│  │                     │      │                         │    │
│  │  ├─ 概览仪表板        │◄────►│  ├─ HTTP 代理服务         │    │
│  │  ├─ Claude 管理       │ Wails │  ├─ 端点管理器            │    │
│  │  ├─ Codex 管理        │ IPC   │  ├─ 请求生命周期管理       │    │
│  │  ├─ 请求追踪         │       │  ├─ HotPool 使用追踪      │    │
│  │  └─ 设置页面         │       │  └─ Wails 事件桥          │    │
│  └─────────────────────┘      └─────────────────────────┘    │
│                                          │                   │
│                                          ▼                   │
│                               ┌─────────────────────┐        │
│                               │       SQLite        │        │
│                               │   ├─ 端点配置         │        │
│                               │   ├─ 请求日志         │        │
│                               │   ├─ 使用统计         │        │
│                               │   ├─ 模型定价         │        │
│                               │   ├─ 系统设置         │        │
│                               │   └─ Schema 版本     │        │
│                               └─────────────────────┘        │
└──────────────────────────────────────────────────────────────┘

请求处理流程：
┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐
│ Client │───►│ Proxy  │───►│Endpoint│───►│ Claude │───►│Response│
│Request │    │ Server │    │ Select │    │  API   │    │ Stream │
└────────┘    └────────┘    └────────┘    └────────┘    └────────┘
                  │              │                           │
                  ▼              ▼                           ▼
             ┌────────┐    ┌────────┐                  ┌────────┐
             │HotPool │    │Failover│                  │ Token  │
             │+ Retry │    │ + 节流  │                  │ Parse  │
             │ Budget │    │ Logic  │                  │+计费防护│
             └────────┘    └────────┘                  └────────┘
```

### 核心模块

| 模块     | 路径                   | 职责                           |
| -------- | ---------------------- | ------------------------------ |
| 代理引擎 | `internal/providers/claude/proxy/` | 请求转发、流式处理、错误恢复、重试预算 |
| 端点管理 | `internal/providers/claude/proxy/endpoint/` | 端点调度、健康检查（含节流）、故障转移 |
| 使用追踪 | `internal/tracking/` | 热池缓存、数据库写入、统计查询、归档失败降级 |
| 业务服务 | `internal/service/`  | 设置管理、渠道服务、定价服务 |
| 数据存储 | `internal/store/`    | SQLite 抽象、Settings 持久化 |
| 前端应用 | `frontend/`          | React + Vite + TailwindCSS     |

> 详细的模块架构图、数据流图和状态机请参考 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## 故障转移机制

### 核心配置项

| 配置项                        | 位置   | 作用                               | 默认值    |
| ----------------------------- | ------ | ---------------------------------- | --------- |
| `failover.enabled`          | 全局   | **渠道间故障转移开关**       | `true`  |
| `failover.loop_retry`       | 全局   | 所有渠道耗尽后是否循环重试         | `false` |
| `failover.max_loops`        | 全局   | 最大循环轮数（仅 loop_retry=true） | `3`     |
| `retry.max_attempts`        | 全局   | 单端点最大重试次数                 | `3`     |
| `endpoint.failover_enabled` | 端点级 | 端点是否参与故障转移               | `true`  |

### 渠道内转移（Intra-Channel）

当请求在**同一渠道内**的端点之间切换时：

```
请求到达 → 获取渠道内端点列表（按优先级排序）
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│  端点 1 → 转发请求                                       │
│     │                                                   │
│     ├── 成功 → 返回响应 ✅                               │
│     │                                                   │
│     └── 失败 → 错误决策                                  │
│            │                                            │
│            ├── 可重试错误 → 同端点重试（≤ max_attempts） │
│            │                                            │
│            └── 需切换错误 → 标记失败，切换到端点 2        │
│                                                         │
│  端点 2 → 转发请求 → ...（同上）                         │
│                                                         │
│  端点 N → 所有端点耗尽                                   │
│     │                                                   │
│     ├── failover.enabled = true  → 触发渠道间转移       │
│     └── failover.enabled = false → 返回错误 ❌          │
└─────────────────────────────────────────────────────────┘
```

#### 错误类型决策表

| 错误类型            | 同端点重试  | 切换端点    | 说明           |
| ------------------- | ----------- | ----------- | -------------- |
| 网络错误 / 连接超时 | ✅ (≤ max) | ✅ (达限后) | 可安全重试     |
| 服务器错误 (5xx)    | ✅ (≤ max) | ✅ (达限后) | 服务端临时问题 |
| 限流 (429)          | ✅ (≤ max) | ✅ (达限后) | 延迟更长       |
| EOF / 响应超时      | ❌          | ✅          | 避免重复计费   |
| 流处理错误          | ❌          | ✅          | 数据已部分发送 |
| 认证错误 (401/403)  | ❌          | ✅          | 尝试其他端点   |
| 客户端取消          | ❌          | ❌          | 立即停止       |

### 渠道间转移（Inter-Channel）

当渠道内**所有端点都失败**后，触发跨渠道故障转移：

```
渠道内所有端点耗尽
    │
    ▼
检查 failover.enabled
    │
    ├── 关闭 → 返回错误 ❌ "故障转移未启用"
    │
    └── 开启 ↓
            │
            ▼
    标记失败端点（RequestFailed=true, Healthy=false）
            │
            ▼
    选择下一可用渠道（排除已尝试渠道，按优先级排序）
            │
            ├── 有可用渠道 → 激活新渠道 → 递归重启请求处理
            │
            └── 无可用渠道 ↓
                    │
                    ▼
            检查 loop_retry
                    │
                    ├── 关闭 → 返回错误 ❌ "没有可用的故障转移渠道"
                    │
                    └── 开启 → 重置端点状态 → 开始新一轮（受 max_loops 限制）
```

### 配置场景对照表

| 场景            | failover.enabled | loop_retry | 行为描述                     |
| --------------- | ---------------- | ---------- | ---------------------------- |
| **场景A** | ❌ 关闭          | -          | 仅渠道内转移，耗尽后返回错误 |
| **场景B** | ✅ 开启          | ❌ 关闭    | 遍历所有渠道一轮后返回错误   |
| **场景C** | ✅ 开启          | ✅ 开启    | 循环重试直到 max_loops 轮    |

#### 场景示例

**场景A：关闭渠道间转移**

```
渠道1: 端点1 → 失败 → 端点2 → 失败 → 端点3 → 失败 → 返回错误 ❌
```

**场景B：开启渠道间转移，关闭循环重试**

```
渠道1 (优先级1): 端点1 → 失败 → 端点2 → 失败 → 触发渠道间转移 →
渠道2 (优先级2): 端点3 → 失败 → 端点4 → 失败 → 触发渠道间转移 →
渠道3 (优先级3): 端点5 → 失败 → 所有渠道耗尽 → 返回错误 ❌
```

**场景C：开启循环重试**

```
第1轮: 渠道1 → 渠道2 → 渠道3 → 耗尽 → 重置端点状态 →
第2轮: 渠道1 → 渠道2 → 渠道3 → 耗尽 → loopCount++ →
第3轮: 渠道1 → 成功 ✅ 或 耗尽 → 返回错误 ❌ "共循环3轮"
```

### 端点状态管理

| 状态字段          | 类型 | 含义         | 设置时机                  |
| ----------------- | ---- | ------------ | ------------------------- |
| `Healthy`       | bool | 健康检查状态 | 定时健康检查 / 渠道间转移 |
| `RequestFailed` | bool | 请求转发失败 | 渠道内/间转移时标记       |
| `NeverChecked`  | bool | 从未检测     | 初始化 / 循环重试重置     |

**端点可用性判定**：

```
可用 = (Healthy || NeverChecked) && !RequestFailed
```

### 安全保护机制

| 机制                     | 作用               | 说明                               |
| ------------------------ | ------------------ | ---------------------------------- |
| **重复计费防护**   | 已发送内容禁止重试 | 防止流式响应中断后重试导致重复计费 |
| **最大重启限制**   | 防止无限递归       | 限制单次请求的渠道切换次数         |
| **最大循环轮数**   | 限制 loop_retry    | 默认最多循环 3 轮                  |
| **状态码透传**     | 不伪造响应码       | 上游返回过就原样透传，本地失败按标准语义映射 |

### 渠道内 vs 渠道间对比

| 维度               | 渠道内转移             | 渠道间转移                                 |
| ------------------ | ---------------------- | ------------------------------------------ |
| **触发条件** | 端点失败 + 决策切换    | 渠道内所有端点耗尽                         |
| **配置开关** | 无（始终启用）         | `failover.enabled`                       |
| **端点选择** | 按端点优先级           | 按渠道优先级                               |
| **状态标记** | `RequestFailed=true` | `Healthy=false` + `RequestFailed=true` |
| **循环重试** | 不支持                 | 支持（`loop_retry`）                     |

## 常见问题

<details>
<summary><b>Q: 代理启动后 Claude Code 连接失败？</b></summary>

1. 确认代理端口（默认 9090）没有被占用
2. 检查 Claude Code 的 `apiBaseUrl` 配置是否正确
3. 确认至少配置了一个可用端点

</details>

<details>
<summary><b>Q: 如何配置故障转移行为？</b></summary>

在「设置」→「故障转移」中配置：

- **生产环境**：开启 `loop_retry`，设置 `max_loops=3`（默认）
- **调试环境**：关闭 `loop_retry`，所有渠道耗尽后立即返回错误

**关键概念**：
- **渠道内重试**：单个端点失败后，自动切换到同渠道的下一个端点
- **渠道间转移**：渠道内所有端点耗尽后，切换到下一个渠道（按优先级）
- **循环重试**：所有渠道都耗尽后，是否回到最高优先级渠道继续尝试

> 旧版本的三种恢复策略（auto_wait / loop_retry / fail_fast）已合并简化。

</details>

<details>
<summary><b>Q: 出站代理是什么？跟服务端口有何区别？</b></summary>

- **服务端口**（默认 9090）：客户端（Claude Code 等）连接本应用的端口
- **出站代理**：本应用连接上游服务商（Claude API / Codex 等）使用的代理

如服务商被墙、公司网络要求走代理时启用。在「设置」→「出站代理」中配置。

</details>

<details>
<summary><b>Q: 如何查看请求日志？</b></summary>

应用内「请求追踪」页面可以查看所有请求记录，支持按时间、状态、模型等筛选。详细日志文件位于 `logs/app.log`。

</details>

<details>
<summary><b>Q: 成本统计不准确？</b></summary>

1. 检查「基础定价」页面的模型定价是否正确
2. 如果使用第三方端点，需要设置对应的成本倍率

</details>

<details>
<summary><b>Q: 支持其他 AI API 吗？</b></summary>

目前专为 Claude API 设计和优化。理论上兼容 Claude API 格式的其他服务也可以使用。

</details>

## 已知问题

### Dev 模式退出进程挂起（Wails v3）

**问题描述**：在开发模式下（`wails3 dev`），通过应用内退出按钮或托盘图标退出应用后，终端会挂起在 Wails 赞助提示信息，无法自动返回命令行。

**影响范围**：仅影响开发模式，Release 构建版本不受影响。

**原因**：这是 Wails v3 的已知限制。`wails3 dev` 命令同时启动 Go 应用和 Vite 前端服务器，当 Go 应用退出时，Vite 进程可能不会被正确终止，导致 wails3 dev 进程持续等待。

**相关 Issue**：

- [wailsapp/wails#2421](https://github.com/wailsapp/wails/issues/2421) - OnShutdown not called in dev mode
- [wailsapp/wails#3058](https://github.com/wailsapp/wails/issues/3058) - Process not terminating properly

**临时解决方案**：在终端中按 `Ctrl+C` 强制中断进程。

## 开发计划

- [ ] 多语言支持（English）
- [ ] 请求重放功能
- [ ] 更多统计维度
- [ ] 自动更新

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

[MIT License](LICENSE)

## 致谢

- 本项目最初受 [xinhai-ai/endpoint_forwarder](https://github.com/xinhai-ai/endpoint_forwarder) 启发
- 感谢 [Wails](https://wails.io) 提供优秀的桌面应用框架
- 感谢所有开源库的贡献者

---

<p align="center">
  <sub>Made with ❤️ and mass vibe coding</sub>
</p>
