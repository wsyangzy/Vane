# Claude Request Forwarder Desktop

<p align="center">
  <img src="build/appicon.png" width="128" height="128" alt="Claude Request Forwarder Desktop">
</p>

<p align="center">
  <strong>Claude API 智能转发代理</strong><br>
  多端点负载均衡 · 自动故障转移 · 实时使用统计
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-6.0.7-blue.svg" alt="Version">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey.svg" alt="Platform">
  <img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8.svg" alt="Go">
  <img src="https://img.shields.io/badge/React-18-61DAFB.svg" alt="React">
</p>

---

## 概述

Claude Request Forwarder Desktop 是一款基于 [Wails](https://wails.io) 构建的跨平台桌面应用，专为 Claude API 用户设计。它作为本地代理运行，提供智能请求转发、多端点管理、自动故障恢复等企业级功能，同时记录完整的使用统计和成本数据。

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
- **智能错误恢复** (v6.0.7+) - 三种恢复策略可选
  - **智能等待** (`auto_wait`) - 所有端点耗尽后挂起请求，等待端点自愈（推荐生产环境）
  - **循环重试** (`loop_retry`) - 回到最高优先级端点继续尝试（仅测试环境）
  - **快速失败** (`fail_fast`) - 立即返回错误，不等待恢复（调试用）
- **端点自愈** - 持续监测故障端点，恢复后自动重新启用
- **流式传输** - 完整支持 SSE 流式响应，零延迟透传
- **计费感知重试** (v6.0.5+) - 基于 Token 计费智能判断错误是否可重试，避免重复计费

### 📊 使用统计

- **实时监控** - Token 用量、请求成功率、响应时间一目了然
- **成本追踪** - 自动计算费用，支持按端点设置成本倍率
- **历史记录** - 完整的请求日志，支持筛选和导出
- **可视化图表** - 请求趋势图，直观展示使用情况

### 🎛️ 端点管理

- **可视化配置** - 图形界面管理端点，无需编辑配置文件
- **渠道分组** - 按渠道标签组织端点，清晰分类
- **状态监控** - 实时显示端点健康状态和响应延迟
- **灵活配置** - 支持自定义请求头、超时时间、成本倍率等

### 🔧 其他特性

- **请求生命周期追踪** - 从接收到完成的全程状态管理
- **热池架构** - 内存缓存 + 异步写入，高性能低延迟
- **本地存储** - SQLite 数据库，无需额外依赖
- **跨平台** - macOS、Windows、Linux 全平台支持
- **可视化设置** (v6.0.7+) - 设置页面支持枚举类型下拉选择、变更追踪、实时生效
- **日志复制** (v6.0.5+) - 日志查看器支持一键复制，便于问题排查

## 截图

| 概览 | 端点管理 |
|:---:|:---:|
| ![overview](images/overview.png) | ![endpoints](images/endpoints.png) |

| 请求追踪 | 设置 |
|:---:|:---:|
| ![requests](images/requests.png) | ![settings](images/settings.png) |

## 快速开始

### 方式一：下载安装包

从 [Releases](https://github.com/xiaozhaodong/claude-request-forwarder-desktop/releases) 页面下载：

| 平台 | 文件 |
|------|------|
| macOS (Intel) | `Claude-Request-Forwarder-darwin-amd64.zip` |
| macOS (Apple Silicon) | `Claude-Request-Forwarder-darwin-arm64.zip` |
| Windows | `Claude-Request-Forwarder-windows-amd64.zip` |
| Linux | `Claude-Request-Forwarder-linux-amd64.tar.gz` |

### 方式二：从源码构建

```bash
# 1. 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 2. 克隆项目
git clone https://github.com/xiaozhaodong/claude-request-forwarder-desktop.git
cd claude-request-forwarder-desktop

# 3. 安装前端依赖
cd frontend && npm install && cd ..

# 4. 开发模式运行
wails dev

# 5. 构建生产版本
wails build
```

### 配置 Claude Code

启动应用后，在 Claude Code 中设置代理地址：

```bash
# 设置 API 代理地址（默认端口 9090）
claude config set --global apiBaseUrl http://127.0.0.1:9090
```

## 配置说明

### 端点配置

端点通过应用内「端点管理」页面进行管理，支持以下配置项：

| 配置项 | 说明 | 示例 |
|--------|------|------|
| 渠道 | 分组标签 | `官方`、`第三方` |
| 名称 | 唯一标识（不可修改） | `claude-primary` |
| URL | API 端点地址 | `https://api.anthropic.com` |
| Token | Bearer Token | `sk-ant-xxx` |
| 优先级 | 数字越小优先级越高 | `1` |
| 故障转移 | 是否参与自动切换 | `启用` |
| 成本倍率 | 费用计算倍率 | `1.0` |

### 全局配置

编辑 `config/config.yaml` 配置全局选项：

```yaml
# 时区设置
timezone: "Asia/Shanghai"

# 服务器配置
server:
  host: "127.0.0.1"
  port: 9090

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

# 日志配置
logging:
  level: "info"              # debug, info, warn, error
  file_enabled: true
  file_path: "logs/app.log"
  max_file_size: "50MB"
  max_files: 10

# 流式传输
streaming:
  heartbeat_interval: "30s"
  read_timeout: "10s"
  response_header_timeout: "90s"  # Claude API 可能需要较长等待

# 使用统计
usage_tracking:
  enabled: true
  hot_pool:
    enabled: true             # 内存热池，提升写入性能
    max_age: "30m"
    max_size: 10000
```

#### 恢复策略详解

当所有端点都不可用时，系统提供三种恢复策略：

| 策略 | 行为 | 适用场景 | 优缺点 |
|------|------|----------|--------|
| **auto_wait**<br/>（智能等待） | 挂起请求，等待端点自动恢复 | **生产环境（推荐）** | ✅ 避免循环重试<br/>✅ 减少无效请求<br/>⚠️ 可能等待较长时间 |
| **loop_retry**<br/>（循环重试） | 回到最高优先级端点继续尝试 | 测试环境或端点极少场景 | ✅ 持续尝试，不会超时<br/>❌ 可能循环重试浪费资源 |
| **fail_fast**<br/>（快速失败） | 立即返回错误 | 调试环境 | ✅ 快速反馈错误<br/>❌ 可能错过即将恢复的端点 |

#### 错误状态码与重试机制

当端点请求失败时，系统使用不同的 HTTP 状态码来控制客户端（如 Claude Code）的重试行为：

| 状态码 | 含义 | 客户端行为 | 触发场景 |
|--------|------|-----------|----------|
| **422** | Unprocessable Entity | 阻止自动重试 | 所有端点耗尽且关闭「端点耗尽后允许重试」 |
| **503** | Service Unavailable | 允许自动重试 | 所有端点耗尽且开启「端点耗尽后允许重试」 |
| **429** | Too Many Requests | 根据 Retry-After 重试 | 上游端点返回限流 |
| **499** | Client Closed Request | 不重试 | 用户主动取消请求 |

**「端点耗尽后允许重试」开关**（设置 → 请求控制）：

| 开关状态 | 返回状态码 | 行为说明 |
|----------|-----------|----------|
| **开启**（默认） | 503 | 允许 Claude Code 等客户端自动重试，适合希望客户端持续尝试的场景 |
| **关闭** | 422 | 阻止客户端自动重试，适合希望立即得到明确错误反馈的场景 |

> **提示**：如果你发现 Claude Code 在所有端点都不可用时持续重试，可以关闭此开关来立即中断。

## 技术架构

```
┌──────────────────────────────────────────────────────────────┐
│                 Claude Request Forwarder Desktop             │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────────────┐      ┌─────────────────────────┐    │
│  │   Frontend (React)  │      │     Backend (Go)        │    │
│  │                     │      │                         │    │
│  │  ├─ 概览仪表板        │◄────►│  ├─ HTTP 代理服务         │    │
│  │  ├─ 端点管理         │ Wails │  ├─ 端点管理器            │    │
│  │  ├─ 请求追踪         │ IPC   │  ├─ 请求生命周期管理       │    │
│  │  ├─ 系统日志         │       │  ├─ 使用量追踪            │    │
│  │  └─ 设置页面         │       │  └─ 事件推送系统          │    │
│  └─────────────────────┘      └─────────────────────────┘    │
│                                          │                   │
│                                          ▼                   │
│                               ┌─────────────────────┐        │
│                               │       SQLite        │        │
│                               │   ├─ 端点配置         │        │
│                               │   ├─ 请求日志         │        │
│                               │   ├─ 使用统计         │        │
│                               │   └─ 模型定价         │        │
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
             │Tracking│    │Failover│                  │ Token  │
             │ Record │    │ Logic  │                  │ Parse  │
             └────────┘    └────────┘                  └────────┘
```

### 核心模块

| 模块 | 路径 | 职责 |
|------|------|------|
| 代理引擎 | `internal/proxy/` | 请求转发、流式处理、错误恢复 |
| 端点管理 | `internal/endpoint/` | 端点调度、健康检查、故障转移 |
| 使用追踪 | `internal/tracking/` | 热池缓存、数据库写入、统计查询 |
| 事件系统 | `internal/events/` | SSE 推送、状态同步 |
| 前端应用 | `frontend/` | React + Vite + TailwindCSS |

## 常见问题

<details>
<summary><b>Q: 代理启动后 Claude Code 连接失败？</b></summary>

1. 确认代理端口（默认 9090）没有被占用
2. 检查 Claude Code 的 `apiBaseUrl` 配置是否正确
3. 确认至少配置了一个可用端点

</details>

<details>
<summary><b>Q: 如何选择合适的恢复策略？</b></summary>

**推荐配置**（v6.0.7+）：
- **生产环境**: `auto_wait`（智能等待）- 避免无效重试，减少资源浪费
- **测试环境**: `loop_retry`（循环重试）- 适合端点数量少且需要持续尝试的场景
- **调试环境**: `fail_fast`（快速失败）- 快速反馈错误，便于问题定位

在应用内「设置」页面可以实时切换策略，提供详细的对比说明。

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
