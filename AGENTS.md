# AGENTS.md

本文件给在本仓库工作的编码 Agent（以及人类贡献者）提供约定与上下文。改动约定时请同步更新本文件。

## 项目概览

`yhchat-tui` 是云湖（Yunhu）社交平台的终端客户端，Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) 实现，支持 Windows / Linux。

当前里程碑：

- **已完成**：登录（邮箱密码 / 短信验证码 + 终端内图片验证码渲染）、自动登录、主窗口（侧边栏 + 会话搜索 + 会话列表 + 消息历史 + 发送文本）、账号信息页、退出登录、全量构建脚本。
- **下一阶段**：WebSocket 实时消息（`wss://chat-ws-go.jwzhd.com/ws`）、更多消息类型、历史分页。

API 依据公开文档仓库 [`../yhchatAPI`](../yhchatAPI) 实现；本仓库的实际调用/实测记录见 [`docs/API_NOTES.md`](docs/API_NOTES.md)。

## 环境与常用命令

- Go **1.24+**（开发环境 go1.27.4）；依赖已 vendored，**可离线构建**，不要依赖网络下载模块。
- Windows 沙箱会话先加载环境变量（Go 不一定在 PATH 中）：

```powershell
. .\scripts\env.ps1
go build ./...
go vet ./...
go test ./... -count=1
```

- 单一二进制构建：`go build -o yhchat-tui .`
- 全量构建（检查 + 测试 + 9 平台交叉编译，只产出可执行文件）：

```powershell
pwsh -File scripts\build.ps1          # Windows
./scripts/build.sh                    # Linux / macOS / Git Bash
```

- UI 快照测试更新（修改界面后必须执行并提交变更的快照）：

```powershell
$env:UPDATE_SNAPSHOTS="1"; go test ./internal/tui -run TestViewSnapshots -count=1; Remove-Item Env:UPDATE_SNAPSHOTS
```


## 目录结构

```
main.go                      入口：flag 解析 + tea 程序启动
internal/api/                HTTP 客户端：登录、会话列表、消息历史、发送（protobuf）
internal/config/             token / 设备 ID 持久化（`~/.yhchat-tui/config.json`）
internal/tui/                所有界面：app 根模型、登录页、主窗口、账号页、样式、快照测试
internal/captchaimg/         图形验证码 → ANSI 半块字符渲染（含真实图片 fixture）
tools/build/                 跨平台全量构建器（被 scripts/build.* 调用）
docs/API_NOTES.md            实际调用的 API 实测记录（协议字段、实测响应）
docs/VERIFICATION.md         独立验收报告（登录里程碑）
vendor/                      依赖（提交进仓库，离线构建）
```

## 代码约定

- 代码风格：`gofmt` 必须干净；`go vet` 必须通过。注释中英文均可，用户可见文案使用中文。
- **不要随意新增第三方依赖**。确需新增时：`go get` 后执行 `go mod tidy && go mod vendor`，并提交
  `go.mod`、`go.sum`、`vendor/`。
- UI 统一使用全终端布局：`internal/tui/layout.go` 的 `screenChrome*`（顶部标题栏 + 分隔线 + 内容 + 底部快捷键提示栏）。
  所有行宽高必须铺满终端；新增界面需在 `TestViewFitsTerminal` 的尺寸集合下通过。
- 样式集中在 `internal/tui/styles.go`，不要在 View 里重复内联颜色。
- **Bubble Tea textinput 的 placeholder 必须是 ASCII**：CJK placeholder 会触发 upstream 渲染 NUL 字节的 bug。
- 时间格式必须使用 `timeNow` / `timeLocation`（`internal/tui/mainwindow.go`），测试会冻结时间保证快照确定性。
- 新增/修改 API 调用时：同步更新 `docs/API_NOTES.md`，并在 `internal/api/*_test.go` 加 protobuf fixture 测试。
- 不要修改 `internal/api/proto.go` 中已验证的 UserInfo 解码语义（字段号/类型以 `../yhchatAPI/src/full.proto` 为准）。

## 测试约定

- 全部离线：使用 `internal/tui/helpers_test.go` 的 httptest mock server；**禁止在测试中访问真实服务**。
- 真实账号 token / 密码 / 短信码绝对不能出现在测试、日志、提交里；测试使用假 token（如 `test-token-1`）。
- 界面变更必须同步 `internal/tui/testdata/snapshots/*.txt`（纯文本快照，`TestViewSnapshots` 逐字节比较）。
- 提交前必须全绿：`gofmt -l main.go internal tools` 为空、`go vet ./...`、`go test ./... -count=1`。

## 安全

- 不提交 token/密码/个人配置；`dist/`、`.gopath/`、`.gocache/`、`.gomodcache/`、`_tools/` 已在 `.gitignore`。
- 运行时 token 只保存在 `~/.yhchat-tui/config.json`（0600；Windows 为 `%USERPROFILE%\.yhchat-tui\config.json`），不打印到 UI/日志。
- 不要把用户的真实 token 硬编码进源码或测试。

## Git 约定

- 每个可工作的里程碑提交一次，提交信息使用英文 Conventional Commits：`feat:` / `fix:` / `docs:` / `test:` / `build:` / `chore:`。
- 提交前确认 `git status` 中只包含本次改动的文件；不要提交构建产物、临时探针、截图中间文件。
- 不强制 GPG 签名；在本仓库可用 `git -c commit.gpgsign=false commit --no-gpg-sign`。

## 参考

- 上游 API 文档：`../yhchatAPI/src/api/v1/*.md`、`../yhchatAPI/src/full.proto`、`../yhchatAPI/src/api/wss/`
- 本项目 API 实测记录：[`docs/API_NOTES.md`](docs/API_NOTES.md)
- 登录里程碑独立验收报告：[`docs/VERIFICATION.md`](docs/VERIFICATION.md)


