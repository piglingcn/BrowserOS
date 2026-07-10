# BrowserOS 前端技术栈

> 基于 `packages/browseros-agent/apps/app/` 的依赖分析

## 框架与核心

| 技术 | 用途 |
|------|------|
| **React 19** | UI 框架 |
| **TypeScript** | 语言 |
| **WXT** (v0.20) | Chrome 扩展框架（基于 Vite） |
| **Vite** | 构建工具 |
| **Bun** | 包管理器 & 运行时 / 脚本执行 |

## UI / 样式

| 技术 | 用途 |
|------|------|
| **Tailwind CSS v4** | CSS 框架（通过 `@tailwindcss/vite` 插件集成） |
| **Radix UI** (15+ 个组件) | 无样式可访问性 UI 组件（Dialog、Dropdown、Popover、Tabs、Switch、Select 等） |
| **Lucide React** | 图标库 |
| **Motion** (原 Framer Motion) | 动画库 |
| **Sonner** | Toast 通知 |
| **CMDK** | 命令面板 |
| **react-resizable-panels** | 可拖拽面板布局 |
| **Embla Carousel** | 轮播组件 |
| **Next Themes** | 主题切换 |
| **Shiki** | 代码语法高亮 |
| **class-variance-authority + clsx + tailwind-merge** | 组件样式组合工具（shadcn/ui 模式） |
| **tw-animate-css** | Tailwind 动画工具 |
| **tailwind-scrollbar-hide** | 滚动条隐藏工具 |

## 状态管理 & 数据流

| 技术 | 用途 |
|------|------|
| **TanStack React Query v5** | 服务端状态管理 / 数据请求 |
| **XState Store** | 轻量级状态管理（带 persist/react/undo 扩展） |
| **SWR** | 部分数据请求场景 |
| **TanStack Query Async Storage Persister** | 异步持久化 |
| **WXT Storage** | 扩展存储 API |
| **idb-keyval** | IndexedDB 键值存取 |
| **@webext-core/messaging** | 扩展消息通信 |

## 路由

| 技术 | 用途 |
|------|------|
| **React Router v7** | 客户端路由（HashRouter） |

## 表单

| 技术 | 用途 |
|------|------|
| **React Hook Form** | 表单管理 |
| **Zod v4** | 表单校验 schema |
| **@hookform/resolvers** | 桥接 RHF 与 Zod |

## API 请求 & GraphQL

| 技术 | 用途 |
|------|------|
| **GraphQL** | 主力 API 请求 |
| **@graphql-codegen/cli + client-preset** | 根据 GraphQL Schema 自动生成类型和 hooks |
| **Hono** | 本地服务端路由（后台通信） |
| **eventsource-parser** | SSE 流事件解析 |
| **Fuse.js** | 本地模糊搜索 |

## AI / 智能体

| 技术 | 用途 |
|------|------|
| **Vercel AI SDK** (`ai`) | AI 流式对话，v6 |
| **@ai-sdk/react** | React 集成 hooks |
| **StreamDown** | AI 流式内容渲染 |
| **TokenLens** | Token 计数与可视化 |
| **@ricky0123/vad-web** | 语音活动检测（Web） |
| **onnxruntime-web** | 浏览器端 ONNX 推理 |
| **@lobehub/icons** | AI 模型图标集 |
| **acp-probe** | Agent-Computer-Protocol 探测 |

## 监控 & 分析

| 技术 | 用途 |
|------|------|
| **Sentry** (React + Vite 插件) | 错误监控 |
| **PostHog** (js + React) | 产品分析 / Feature flag / 调查 |
| **@posthog/react** | React 集成 |

## 工具库

| 技术 | 用途 |
|------|------|
| **dayjs** | 日期处理 |
| **nanoid** | 唯一 ID 生成 |
| **es-toolkit** | Lodash 替代工具集 |
| **use-deep-compare-effect** | 深比较 useEffect |
| **use-stick-to-bottom** | 粘性滚动到底部 |
| **canvas-confetti** | 🎉 撒花特效 |
| **better-auth** | 认证库 |
| **MDX Editor** | Markdown/MDX 富文本编辑器 |
| **XYFlow** (React Flow) | 流程图 / 节点编辑器 |

## 开发工具

| 技术 | 用途 |
|------|------|
| **Biome** | Lint + 格式化 |
| **TypeScript** (v5.9) | 类型检查 |
| **shiki** | 代码高亮（开发文档） |
| **Lefthook** | Git hooks 管理器 |
| **GraphQLSP** | IDE 中 GraphQL 类型支持 |

---

## 架构模式要点

1. **浏览器扩展 MV3** — Chrome Extension Manifest V3，使用 WXT 框架管理多入口点：sidepanel（侧边栏）、app（设置页）、newtab（新标签页）、onboarding（引导页）、background（后台）、content script（内容脚本）

2. **GraphQL + Codegen** — 通过 `@graphql-codegen` 从 `.graphql` 文档自动生成类型安全的查询 hooks，避免手写网络请求代码

3. **服务端状态管理** — 统一走 TanStack Query，分两路：
   - **GraphQL 路**：`useGraphqlQuery` / `useGraphqlMutation` 封装
   - **REST 路**：通过 `useQuery` / `useMutation` 原生 TanStack Query

4. **shadcn/ui 风格** — Radix UI 组件 + CVA + tailwind-merge 组合模式，生成的基础 UI 组件不可手改

5. **表单模式** — react-hook-form + Zod schema + shadcn Form 组件，`zod/v3` 导入（Zod v4 兼容入口）
