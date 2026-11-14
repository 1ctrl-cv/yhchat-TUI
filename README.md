# 云湖 TUI 客户端（yhchat-tui）

一个用 Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) 编写的云湖（Yunhu）社交平台命令行客户端，支持 **Windows** 和 **Linux**。
当前版本实现了完整的**登录流程**与**账号信息主页**，聊天/消息功能将在后续版本实现。

> 本项目是非官方客户端，API 依据公开文档仓库 [`yhchatAPI`](../yhchatAPI) 实现，仅供学习交流。

## 功能

- **邮箱密码登录**：`POST /v1/user/email-login`
- **短信验证码登录**：获取并**在终端内直接渲染图形验证码**（24 位真彩色半块字符），发送短信、验证登录
  - `Ctrl+R` 更换图形验证码，`Ctrl+S` 把验证码图片保存为 PNG（终端看不清时可用图片查看器打开）
- **登录状态持久化 / 自动登录**：token 保存在本机配置目录，下次启动自动校验并进入主页
- **账号信息主页**：昵称、用户 ID、脱敏手机号/邮箱、金币、VIP 状态、邀请码
- **退出登录**：调用 `POST /v1/user/logout` 并清除本地 token
- **跨平台**：纯 Go、无 CGO；Windows / Linux（amd64、arm64）
- **自适应小终端**：卡片布局会随终端尺寸在完整/紧凑/无边框模式间自动切换（建议终端至少 24 行；更小的窗口会出现内容滚动）

## 界面预览

以下截图由 TUI 实际渲染的 ANSI 输出转换而成（字体/配色会因终端而略有差异）。

| 邮箱密码登录 | 短信验证码登录（终端内渲染图片验证码） |
| --- | --- |
| ![邮箱密码登录](assets/screenshots/login-email.png) | ![短信验证码登录](assets/screenshots/login-sms-captcha.png) |

| 登录失败提示 | 账号信息主页 |
| --- | --- |
| ![登录失败提示](assets/screenshots/login-error.png) | ![账号信息主页](assets/screenshots/home.png) |

## 环境要求

- Go **1.24+**（开发使用 go1.27.4 验证）
- 任意支持 ANSI 转义序列的终端：Windows Terminal、cmd/PowerShell 窗口、Linux 常见终端均可
- 已执行 `go mod vendor`，**离线即可构建**（无需访问 Go 模块代理）

## 构建

```bash
# 当前平台
go build -o yhchat-tui .

# Windows PowerShell 一键交叉编译 4 个目标到 dist\
pwsh -File scripts\build.ps1
```

产物：

| 平台 | 文件 |
| --- | --- |
| Windows amd64 | `dist/yhchat-tui-windows-amd64.exe` |
| Windows arm64 | `dist/yhchat-tui-windows-arm64.exe` |
| Linux amd64 | `dist/yhchat-tui-linux-amd64` |
| Linux arm64 | `dist/yhchat-tui-linux-arm64` |

## 运行

```bash
./yhchat-tui            # Windows: yhchat-tui.exe
```

首次启动进入登录页：

- `Tab` / `↑` / `↓`：切换输入框
- `Ctrl+T`：把焦点移到顶部的登录方式标签（再用 `←` / `→` 切换邮箱登录 / 短信登录）
- `Enter`：执行当前步骤（登录、获取图形验证码、发送短信验证码）
- `Ctrl+C`：退出
- 登录成功后进入账号主页：`r` 刷新账号信息，`l` 退出登录，`q` 退出

### 短信登录流程

1. 切换到「短信验证码」标签，程序自动获取图形验证码
2. 在终端中查看图片并输入其中的字符（`Ctrl+S` 可保存 PNG、`Ctrl+R` 可换一张）
3. `Enter` 发送短信验证码
4. 输入收到的短信验证码，`Enter` 登录

### 命令行参数

```
yhchat-tui [-config FILE] [-server URL] [-version]

  -config FILE   指定配置文件路径（默认使用用户配置目录）
  -server URL    指定 API 服务器地址（默认 https://chat-go.jwzhd.com，调试用）
  -version       打印版本号
```

## 配置文件

| 平台 | 位置 |
| --- | --- |
| Windows | `%AppData%\yhchat-tui\config.json` |
| Linux | `$XDG_CONFIG_HOME/yhchat-tui/config.json`（默认 `~/.config/yhchat-tui/config.json`） |

- 文件权限 `0600`（Windows 由用户目录 ACL 保护）
- 保存登录 token、设备 ID（`deviceId`，登录设备标识）和上次使用的邮箱/手机号
- token 不会出现在界面或日志中；如需退出登录请使用主页的 `l`，或删除该文件

## 无账号体验 / 联调

仓库内含一个本地 mock API，可在没有云湖账号时体验完整登录流程：

```bash
# 终端 1：启动 mock 服务（监听 127.0.0.1:18080）
go run ./tools/mockapi

# 终端 2：让 TUI 指向 mock 服务
go run . -server http://127.0.0.1:18080 -config ./demo-config.json
```

mock 账号：

- 邮箱登录：`demo@yunhu.chat` / `demo1234`
- 短信登录：任意 11 位手机号，短信验证码 `123456`；图形验证码内容任意填写即可

## 测试

```bash
go test ./...        # 单元测试 + 登录流程集成测试（使用本地 mock HTTP 服务）
go vet ./...
```

测试覆盖：

- API 客户端：邮箱/短信登录、图文验证码、protobuf 用户信息解析、未登录判定、错误路径
- protobuf 解码：真实抓包 fixture、未知字段、截断/随机字节不 panic
- 验证码渲染：真实图片 fixture、尺寸预算、彩色/灰度两条路径
- 配置读写：设备 ID 生成、原子写入、损坏配置备份
- TUI：完整邮箱/短信登录流程、自动登录、过期、退出、表单校验、快捷键、多终端尺寸布局
- `internal/tui/testdata/snapshots/` 保存了各页面在纯文本下的渲染快照（`UPDATE_SNAPSHOTS=1 go test ./internal/tui` 可更新）

## 项目结构

```
main.go                    命令行入口（参数解析、tea 程序启动）
internal/api/              云湖 HTTP API 客户端 + protobuf 解码
internal/config/           token / 设备 ID 持久化
internal/tui/              登录页、账号主页、根模型、样式
internal/captchaimg/       图形验证码 → 终端 ANSI 半块字符渲染
internal/captchaimg/testdata/captcha.png   真实验证码测试图
tools/mockapi/             本地 mock API（演示 / 联调 / 测试）
docs/API_NOTES.md          本项目实际调用的 API 实测记录
scripts/build.ps1          交叉编译脚本
scripts/env.ps1            沙箱开发环境变量（仅开发会话需要）
vendor/                    依赖（离线构建）
```

## 已知限制

- 当前仅实现登录与账号信息展示，聊天/消息/群组等功能尚未实现
- 建议终端高度 ≥ 24 行；更小的窗口会退化为无边框紧凑布局，极端尺寸下内容可能超出屏幕（不会崩溃）
- 短信登录使用**图文验证码**；文档中另有滑动验证码（`slide`），TUI 内难以交互，暂未支持
- 终端图片渲染需要 24 位真彩色；`NO_COLOR=1` 或 `TERM=dumb` 时自动降级为灰度字符画，可读性有限（此时建议 `Ctrl+S` 保存图片）
- 登录接口对平台参数只做记录，客户端按运行系统自动填写 `windows` / `linux`
- 账号密码只在本地用于登录请求，不会被保存；token 保存于本机配置目录

## 免责声明

本项目为第三方非官方实现，与云湖官方无关。API 属于公开文档记录的接口，可能随时变更；
请勿将本项目用于违反服务条款的用途，并妥善保管自己的账号凭据与 token。
