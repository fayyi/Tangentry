# 贡献指南

感谢你愿意改进 Tangentry！这份文档说明如何搭建环境、提交改动，以及我们对代码与测试的期望。

## 开始之前

- 修复 bug：直接开 PR，并在描述中说明复现步骤；
- 新功能或较大重构：请先开 [Issue](https://github.com/mu-zi-lee/Tangentry/issues) 讨论，避免白做；
- 安全问题：请按 [SECURITY.md](SECURITY.md) 私下报告，不要公开提交。

## 开发环境

| 依赖 | 版本 |
| --- | --- |
| Go | 1.27+ |
| Node.js | 24+ |
| 可选 | `yt-dlp`、`ffmpeg`、Python 3 |

```bash
git clone https://github.com/mu-zi-lee/Tangentry.git
cd Tangentry
cd src/web && npm ci && npm run build
cd ../.. && (cd src && go test ./...)
make desktop      # 桌面开发模式
```

只改前端时可以 `make server && ./build/tangentry-server -data ./data -addr 127.0.0.1:7380`，另开终端 `cd src/web && npm run dev`（Vite 会把 `/api` 代理到 7380）。

## 代码约定

- 先读 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，遵守分层依赖方向（`cd src && go test ./internal/layering` 会检查）；
- Go 代码通过 `gofmt` 与 `go vet`；前端通过 `npm run build`（包含 `tsc --noEmit`）；
- 用户可见文案使用简体中文；
- 不要提交凭据、Cookie、个人数据或本机绝对路径；测试数据使用明显的夹具值；
- 新增数据库变更只追加迁移文件，不修改已发布的迁移。

## 测试

**原则：单元测试覆盖细节，少量 E2E 覆盖最重要的用户流程。**

```bash
make test-unit    # 前端构建 + Vitest + go test
make test-e2e     # Playwright 关键流程（首次需 npx playwright install chromium）
make check-race   # go vet + 竞态检测
```

- 纯逻辑放进函数并在同目录写 `*.test.ts` / `*_test.go`；
- 不要为单个 UI 细节新建 E2E spec；只有影响核心旅程时才扩展已有的 spec。

## 提交与 PR

- 提交信息使用祈使句，简洁说明“做了什么”，例如 `Add OPML export for subscription groups`；
- 一个 PR 只做一件事，附上测试；
- UI 改动请附截图或录屏；
- PR 提交前确认 `make test-unit` 通过。

## 许可证

提交贡献即表示你同意以 [MIT](LICENSE) 许可证发布你的代码。
