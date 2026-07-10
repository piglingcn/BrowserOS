# BrowserOS 项目架构文档

## 项目概述

BrowserOS 是一个开源的 Chromium 分支浏览器，核心定位是让浏览器原生运行 AI Agent。它包含两个主要部分：

- **`BrowserOS 浏览器`**：基于 Chromium 146 的定制浏览器，带 AI Agent 侧边栏和新标签页
- **`BrowserOS Agent`**：Agent 平台，包含 MCP 服务器、Agent UI、CLI 和评估框架

## 项目结构

```
browser-os/
├── packages/
│   ├── browseros/               # Chromium 浏览器构建系统 (Python)
│   │   ├── bos_build/           # 构建系统 (Python CLI)
│   │   ├── chromium_patches/    # BrowserOS 对 Chromium 的补丁
│   │   ├── chromium_files/      # 新增文件（非补丁）
│   │   ├── series_patches/      # 有序补丁系列
│   │   ├── resources/           # 图标、权限、签名资源
│   │   └── tools/               # 补丁管理工具
│   │
│   └── browseros-agent/         # Agent 平台 (Bun monorepo)
│       ├── apps/
│       │   ├── server/          # Bun HTTP 服务器 - MCP + Agent 循环
│       │   ├── app/             # Chrome 扩展 UI (WXT + React)
│       │   ├── cli/             # Go CLI 终端控制工具
│       │   ├── eval/            # 评估框架 (Voyager, Mind2Web)
│       │   ├── claw-server/     # Claw 服务器 (Node)
│       │   ├── claw-server-rust/# Claw 服务器 (Rust)
│       │   └── claw-app/        # Claw UI (React)
│       │
│       ├── packages/
│       │   ├── cdp-protocol/    # CDP 类型绑定 (auto-generated)
│       │   ├── shared/          # 共享常量 (ports, timeouts, limits)
│       │   └── browseros-core/  # Agent 核心 (Rust)
│       │
│       ├── crates/
│       │   ├── browseros-core/  # CDP 核心逻辑
│       │   ├── browseros-mcp/   # MCP 协议实现
│       │   └── browseros-cdp/   # Chrome DevTools Protocol
│       │
│       └── docs/                # 文档 (Mintlify MDX)
```

## 技术栈

### BrowserOS 浏览器

| 技术 | 用途 |
|------|------|
| Chromium 146 | 浏览器内核 (C++) |
| Python 3.12+ | 构建系统 (`bos_build`) |
| GN + Ninja | Chromium 编译 |
| WinSparkle / Sparkle | 自动更新框架 |

### BrowserOS Agent

| 技术 | 用途 |
|------|------|
| **Bun** | 运行时 + 包管理 (替代 Node.js) |
| **TypeScript** | 主要开发语言 |
| **React** | UI 框架 |
| **WXT** | Chrome 扩展构建 (替代 web-ext) |
| **Hono** | HTTP 服务器框架 |
| **Tailwind CSS** | 样式 |
| **Biome** | 代码格式化 + 检查 |
| **Go** | CLI 工具 |
| **Rust** | 核心 CDP/MCP 引擎 |

## 架构图

```
┌──────────────────────────────────────────────────────────────┐
│                        BrowserOS 浏览器                        │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  扩展 UI (WXT React)                                   │  │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌─────────┐  │  │
│  │  │ 侧边栏   │ │ 新标签页 │ │ 设置页   │ │ 新手引导│  │  │
│  │  │ Chat UI  │ │ New Tab  │ │ Settings │ │Onboarding│  │  │
│  │  └──────────┘ └──────────┘ └──────────┘ └─────────┘  │  │
│  └────────────────────────────────────────────────────────┘  │
│                            │ HTTP                             │
│                            ▼                                  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  BrowserOS Server (Bun, port 9100, localhost)           │  │
│  │  ┌──────────┐ ┌──────┐ ┌──────┐ ┌──────────┐          │  │
│  │  │ /chat     │ │ /mcp │ │/health│ │ /provider │          │  │
│  │  │ Agent循环 │ │ MCP  │ │ 健康  │ │ Provider  │          │  │
│  │  │ streaming │ │ 工具 │ │ 检查  │ │ 测试     │          │  │
│  │  └──────────┘ └──────┘ └──────┘ └──────────┘          │  │
│  └────────────────────────────────────────────────────────┘  │
│                            │ CDP                              │
│                            ▼                                  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Chromium CDP (port 9000)                              │  │
│  │  BrowserOS Server 作为客户端连接                         │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
                            │ MCP
                            ▼
┌──────────────────────────────────────────────────────────────┐
│  外部 MCP 客户端                                              │
│  (Claude Code, Gemini CLI)                                   │
└──────────────────────────────────────────────────────────────┘
```

## 扩展 UI（apps/app）

### 技术栈

| 技术 | 版本/说明 |
|------|----------|
| WXT | Chrome 扩展构建工具 |
| React 19 | UI 框架 |
| TypeScript | 开发语言 |
| Tailwind CSS | 样式 (v4, `@tailwindcss/vite`) |
| React Router v7 | HashRouter 路由 |
| TanStack Query | 服务端状态管理 |
| Zod | 表单验证 |
| react-hook-form | 表单处理 |
| dayjs | 日期处理 |
| ai-sdk/react | AI chat 集成 |
| shadcn/ui | UI 组件库 (生成在 components/ui/) |
| GraphQL Codegen | GraphQL 类型生成 |
| @lobehub/icons | Provider 图标 |

### 目录结构

```
apps/app/
├── entrypoints/             # WXT 入口点
│   ├── background/          # Service Worker
│   ├── sidepanel/           # 侧边栏聊天 UI
│   ├── app/                 # 应用/设置页面
│   ├── newtab/              # 新标签页
│   ├── onboarding/          # 新手引导
│   └── *.content.{ts,tsx}   # 内容脚本
│
├── components/
│   ├── ui/                  # shadcn UI 组件 (生成，不手改)
│   ├── ai-elements/         # AI 组件 (生成，不手改)
│   └── ...                  # 功能组件
│
├── modules/                 # 业务模块
│   └── chat/               # 聊天会话管理
│       ├── chat-session.hooks.ts  # 核心聊天 hook
│       ├── chat-refs.hooks.ts     # 引用管理
│       ├── chat-session-request.ts # 请求构建
│       └── ...
│
├── lib/                     # 共享工具库
│   ├── llm-providers/       # Provider 配置
│   ├── conversations/       # 对话历史
│   ├── messaging/           # 扩展间通信
│   ├── search-actions/      # 搜索/发送动作
│   └── execution-history/   # 执行历史
│
├── screens/                 # 页面组件
│   ├── sidepanel/           # 侧边栏页面
│   ├── agent-command/       # Agent 命令页面
│   ├── ai-settings/         # AI 设置
│   └── ...
│
├── generated/graphql/       # GraphQL 代码生成输出
├── schema/                  # GraphQL schema 文件
└── public/                  # 静态资源 (icons, fonts, WASM)
```

### 构建

```bash
cd apps/app
bun install
cp .env.example .env.development
bun run dev           # 开发模式 (热更新)
bun run build         # 生产构建 → dist/chrome-mv3/
bun run zip           # 打包 zip
```

### 关键入口点

| 入口 | 文件 | 说明 |
|------|------|------|
| Service Worker | `entrypoints/background/index.ts` | 扩展生命周期、消息路由、storage 操作 |
| 侧边栏 | `entrypoints/sidepanel/main.tsx` | AI 聊天界面 |
| 设置页 | `entrypoints/app/main.tsx` | Provider 配置、MCP 设置等 |
| 新标签页 | `entrypoints/newtab/` | 统一的搜索和 Agent 首页 |

### 消息通信

扩展组件之间使用 `@webext-core/messaging` 定义类型安全的通信协议：

```
RuntimeMessagesProtocol
├── runtime.getTabId           → 获取当前标签页 ID
├── runtime.authSuccess        → 登录成功
├── runtime.stopAgent          → 停止 Agent
├── runtime.sidePanelScopeChanged → 侧边栏作用域变更
└── runtime.sendAgentQuery     → 发送 Agent 查询
```

### 数据流 (点击"开始生成")

```
DemoImagePage 点击
    ↓ openSidePanelWithSearch
背景脚本收到
    ↓ openSidePanel
打开侧边栏
    ↓ setValue → storage
侧边栏 Chat 组件 watch 触发
    ↓ sendMessage
AI SDK 发起 chat 请求 → 后端 Server
```

## 后端 Server（apps/server）

### 技术栈

| 技术 | 说明 |
|------|------|
| **Bun** | 运行时 |
| **Hono** | HTTP 框架 |
| **AI SDK** | Agent 循环和工具调用 |
| **CDP** | Chrome DevTools Protocol 控制浏览器 |

### 核心模块

| 模块 | 说明 |
|------|------|
| `src/api/` | Hono 路由 - /chat, /mcp, /health |
| `src/agent/` | Agent 循环、Provider 工厂、提示词管理 |
| `src/browser/` | CDP 浏览器控制 |
| `src/tools/` | MCP 工具实现 |
| `src/lib/` | DB、OAuth、日志、监控 |

## CLI（apps/cli）

Go 语言实现的命令行工具，用于从终端控制 BrowserOS。支持通过 MCP 协议与 Claude Code 等 AI 工具集成。

## Chromium 构建（packages/browseros）

### 构建系统 (bos_build)

基于 Python 的构建系统，使用 GN + Ninja 编译 Chromium。

```bash
browseros setup          # 拉取 Chromium 源码 (~40GB)
browseros apply          # 打 BrowserOS 补丁
browseros build          # 编译
browseros package        # 打包安装程序
browseros sign           # 签名
```

### 环境要求

| 平台 | 要求 |
|------|------|
| Windows | VS 2022 + Windows SDK |
| macOS | Xcode + Command Line Tools |
| Linux | build-essential, clang, lld |
| 通用 | Python 3.12+, ~100GB 硬盘 |

### 补丁系统

`chromium_patches/` 目录按 Chromium 源码路径组织，包含 AI Agent 集成、隐私增强、品牌替换等修改。通过 `features.yaml` 管理功能标志。

## 开发指南

### 快速开始（只改 UI，不需要编译 Chromium）

```bash
# 1. 进入扩展 UI 目录
cd packages/browseros-agent/apps/app

# 2. 安装依赖
bun install
cp .env.example .env.development

# 3. 构建
GRAPHQL_SCHEMA_PATH=schema/schema.graphql bun run build

# 4. 加载到 BrowserOS/Chrome
# chrome://extensions → 开发者模式 → 加载已解压扩展 → 选 dist/chrome-mv3/
```

### 完整开发环境（需要后端）

```bash
# monorepo 根目录
cd packages/browseros-agent

# 复制环境配置
cp apps/server/.env.example apps/server/.env.development
cp apps/app/.env.example apps/app/.env.development

# 安装依赖
bun install

# 启动完整开发环境（Server + App）
bun run dev:watch
```

## CI/CD

GitHub Actions 工作流：

| 工作流 | 触发 | 说明 |
|--------|------|------|
| `build-browseros.yml` | 调用 | Chromium 浏览器构建 (可复用) |
| `release-windows.yml` | 手动 | Windows 发布构建 |
| `release-macos.yml` | 手动 | macOS 发布构建 |
| `release-linux.yml` | 手动 | Linux 发布构建 |
| `nightly-browseros.yml` | 定时/手动 | macOS 每日构建 |
| `release-agent-extension.yml` | 手动/标签 | 扩展发布 |
| `release-server.yml` | 手动 | Server 发布 |
