# Tangentry 架构说明

本文面向贡献者，说明代码如何分层、依赖方向、各目录的职责，以及新增功能时应该把代码放在哪里。

## 总览

```text
          ┌──────────────────────────────┐
          │  src/web/ (React 19 + Vite)  │  ← 浏览器 / 桌面 WebView
          └──────────────┬───────────────┘
                         │ HTTP JSON  /api/*
┌────────────────────────▼─────────────────────────┐
│ src/cmd/tangentry-desktop   src/cmd/tangentry-server │  入口：进程、窗口、密码门
├──────────────────────────────────────────────────┤
│ src/internal/server   src/internal/desktop        │  外壳：运行时、会话、生命周期
├──────────────────────────────────────────────────┤
│ src/internal/transport/http                      │  传输层：路由、校验、错误映射
│        ▲ 只依赖 ports_*.go 中定义的接口             │
├──────────────────────────────────────────────────┤
│ src/internal/app                                 │  组合根：创建适配器并装配服务
├──────────────────────────────────────────────────┤
│ src/internal/{content,notes,subscriptions,...}   │  领域 / 用例：纯业务逻辑
│   ai,transcription,downloads,favoritesync,...}   │
├──────────────────────────────────────────────────┤
│ src/internal/adapters/{sqlite,platforms/*,...}   │  适配器：数据库、平台 API、
│   articles,tools,aichat,filesystem,...}          │  外部命令、文件系统
└──────────────────────────────────────────────────┘
```

前端构建产物（`src/web/dist`）通过 `src/web/assets.go` 的 `go:embed` 打包进 Go 二进制，桌面版和服务端版共享同一套前端与同一套 HTTP API。

## 依赖规则

规则由 [`src/internal/layering/layering_test.go`](../src/internal/layering/layering_test.go) 在 `go test` 中强制执行，违反即测试失败：

| 层 | 可以依赖 | 不可以依赖 |
| --- | --- | --- |
| `src/cmd/*` | `app`、`server`、`desktop`、`jobs` | 领域包、适配器 |
| `src/internal/server` | 领域包 | `app`、`adapters`、`transport` |
| `src/internal/transport` | 领域包（类型与接口） | `app`、`server`、`adapters` |
| `src/internal/app` | 一切内层与适配器 | `server` |
| `src/internal/adapters/*` | 领域包 | `app`、`server`、`transport` |
| 领域包 | 其他领域包、标准库 | 任何外层或具体实现 |

一句话：**依赖只能向内**。领域代码不知道 SQLite、HTTP 或某个平台的存在。

## 目录职责

### 后端

| 目录 | 职责 |
| --- | --- |
| `src/cmd/tangentry-desktop` | MyGo 原生窗口、菜单、对话框、导航拦截、生命周期 |
| `src/cmd/tangentry-server` | 无界面服务，读取环境变量、生成密码、健康检查 |
| `src/internal/app` | 组合根：`wire_services.go` 装配服务，`http_*_adapters.go` 将服务适配为传输层端口 |
| `src/internal/transport/http` | `routes_*.go` 注册路由，按功能拆分处理器；`ports_*.go` 定义它需要的接口 |
| `src/internal/server` | 密码门、桌面/服务端共享的外壳逻辑 |
| `src/internal/content` | 内容条目模型、ID 规则与校验，是全局共享的核心类型 |
| `src/internal/notes` | 笔记创建、搜索、标签、导出（Markdown/ZIP） |
| `src/internal/subscriptions` / `discovery` / `favoritesync` | 订阅、推荐与搜索、收藏同步的用例 |
| `src/internal/ai` / `transcription` / `subtitles` | AI 摘要与翻译、转录、字幕处理 |
| `src/internal/downloads` | 下载队列、准入、工作者、产物管理 |
| `src/internal/netpolicy` | 公网抓取的 SSRF 防护 HTTP 客户端 |
| `src/internal/credentials` | 平台 Cookie / 会话的存储格式与传输 |
| `src/internal/adapters/sqlite` | SQLite 存储与迁移（`migrations/NNN_name.sql`） |
| `src/internal/adapters/platforms/*` | 每个平台一个包：bilibili、youtube、xiaohongshu、douyin、xiaoyuzhou、discourse |
| `src/internal/adapters/tools` | yt-dlp、FFmpeg、Whisper 等外部工具与云端转录 |

### 前端

| 目录 | 职责 |
| --- | --- |
| `src/web/src/app` | 应用外壳、导航、阅读工作区、全局 hooks |
| `src/web/src/features/<feature>` | 按功能聚合的页面、组件、状态与纯函数 |
| `src/web/src/shared` | 跨功能复用：`api`（请求与类型）、`ui`、`hooks`、`media`、`text` |
| `src/web/tests` | Playwright 关键流程测试与 E2E 夹具服务 |

## 测试策略

> **单元测试覆盖细节，少量 E2E 覆盖最重要的用户流程。**

| 层级 | 位置 | 运行 | 写什么 |
| --- | --- | --- | --- |
| Go 单元/集成 | `src/internal/**/*_test.go` | `cd src && go test ./...` | 解析、校验、迁移、服务逻辑、HTTP 处理器（`httptest`） |
| 前端单元 | `src/web/src/**/*.test.ts`（与源码同目录） | `cd src/web && npm test` | 纯函数：Markdown 导出、翻译分段、排序、校验等 |
| 端到端 | `src/web/tests/*.spec.ts` | `cd src/web && npm run test:e2e` | 只放核心用户旅程 |

当前保留的 E2E 关键流程：

| 文件 | 覆盖的用户旅程 |
| --- | --- |
| `app.spec.ts` | 应用外壳与导航、笔记增删改、收藏、下载、字幕加载与搜索、阅读页直链 |
| `search-pagination.spec.ts` | 添加内容 / 搜索与分页 |
| `subscriptions.spec.ts` | 添加订阅、刷新、失败保留 |
| `player.spec.ts` | 本地播放、字幕跟随、跳转与笔记时间 |
| `video-transcription.spec.ts` | 手动转录与失败重试 |
| `ai-translation.spec.ts` | AI 设置与字幕翻译 |
| `note-export.spec.ts` | 笔记筛选与导出 |
| `bilibili-login.spec.ts` | 扫码登录 |

**新增测试时的判断标准：**

1. 能用纯函数表达的逻辑 → 抽成函数，写单元测试；
2. 后端行为 → Go 测试，HTTP 层用 `httptest`；
3. 只有当一个改动会破坏上表中的某条用户旅程时，才扩展对应的 E2E，而不是新增 spec 文件。

E2E 使用 [`src/web/tests/server`](../src/web/tests/server/main.go)：真实服务 + 隔离的 SQLite + 显式的平台夹具，不访问外网。

## 新增一个功能：放在哪里

以“新增一个内容平台”为例：

1. `src/internal/adapters/platforms/<name>/`：HTTP 客户端与响应解析，配套解析测试；
2. `src/internal/content/identity.go`：注册 `<name>:<id>` 的 ID 规则；
3. `src/internal/discovery` 或 `src/internal/subscriptions`：在用例层接入；
4. `src/internal/app`：在组合根装配适配器；
5. `src/internal/transport/http/routes_*.go`：如需新接口，先在 `ports_*.go` 声明接口；
6. `src/web/src/features/<feature>/`：前端界面，纯逻辑配单元测试。

## 数据库迁移

- 新增文件 `src/internal/adapters/sqlite/migrations/NNN_description.sql`，编号递增且不可复用；
- 每个版本在事务中执行并记录到 `schema_version`，中断后可续；
- 需要 Go 代码改写数据时，在 `migrations.go` 的 `migrationSteps` 中注册；
- **已发布的迁移文件不要修改**，只追加新版本。
