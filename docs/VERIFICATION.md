# 独立验收报告 — yhchat-tui 登录里程碑

> 历史报告：本报告记录的是 `5108d5f` 时的状态。之后项目已把配置路径改为
> `~/.yhchat-tui/config.json`（旧位置自动迁移）并移除了 `tools/mockapi` 演示工具；
> 报告中相关的路径/mock 描述仅反映当时版本，其余结论仍然适用。

- 角色：独立验证者（verifier，未参与代码编写），对抗性验收
- 日期：2026-11-14（Asia/Shanghai），约 10:11–10:29
- 验收目标：`main` @ **`5108d5f`**（`5e1ae33` 为登录功能实现；`1b69911` 仅新增 README 截图/文档；`5108d5f` 为验证过程中发现的测试/边界问题修复）
- 环境：Windows（`windows/amd64`），Go **1.27.4**（未在 PATH，使用 `. .\scripts\env.ps1`）；DSH 沙箱；无外网（HTTPS 被沙箱拦截），未调用真实云湖 API
- 结论：**PASS**。构建 / vet / 测试 / 4 平台交叉编译 / 离线 vendored 构建全部通过；API 契约、protobuf 解码、安全隐私、TUI 行为与文档一致。未发现 critical / high 级产品缺陷；验证中发现的 2 个 medium（均为测试基础设施）与 1 个 low 边界问题已在 `5108d5f` 修复并复验，1 个 low 已文档化。

## 1. 验证方法与免干扰措施

1. 源码对验证者冻结，本报告只写 `docs/VERIFICATION.md`，未修改任何被测源码。
2. 为防止并行写入与 Git 行尾转换（本机 Git 系统配置 `core.autocrlf=true`）污染结果，权威构建/测试在**全新解包副本**中进行：
   `git -c core.autocrlf=false archive HEAD | tar -x -C <tmp>`，另在 workspace 原地复核（最终 workspace `git status` 干净、HEAD=`5108d5f`）。
3. 现有测试未覆盖的对抗性用例通过 **`go test -overlay=<json>`** 注入临时 `_test.go` 文件（只存在于临时目录，不写入仓库），覆盖：精确 HTTP 契约、错误/超时/非 200、protobuf 截断与随机输入、配置持久化、TUI 布局扫描与安全断言。overlay 测试全部通过（api 3 个、config 2 个、tui 9 个测试函数）。
4. 全部命令使用 `GOPROXY=off` + `-mod=vendor`，证明**无网络**可构建/测试；未执行 `go get` / `go mod tidy`。

## 2. 命令与结果

| # | 命令（在解包副本中执行，除特别说明） | 结果 |
| --- | --- | --- |
| 1 | `go version` | `go version go1.27.4 windows/amd64` ✅ |
| 2 | `go build ./...` | exit 0 ✅ |
| 3 | `go vet ./...` | exit 0 ✅ |
| 4 | `go test ./... -count=1` | exit 0，`api`/`captchaimg`/`config`/`tui` 全部 `ok`；主包与 `tools/mockapi` 无测试 ✅ |
| 5 | `GOOS=windows\|linux GOARCH=amd64\|arm64 CGO_ENABLED=0 go build -trimpath -mod=vendor -o <tmp>/... .` | 4/4 exit 0（产物见下） ✅ |
| 6 | `pwsh -File scripts/build.ps1 -OutDir <tmp>` | exit 0，生成 4 个产物 ✅ |
| 7 | `GOOS=linux GOARCH=amd64\|arm64 go vet ./...` | exit 0 ✅ |
| 8 | 空 `GOMODCACHE` + 空 `GOPATH` + `GOPROXY=off` 下 `go build ./...`、`go test ./internal/api` | 通过；构建后 module cache 文件数 = 0 → **vendor 完整、真正离线** ✅ |
| 9 | `go test -race ./...` | exit 2：`-race requires cgo`（本机 `CGO_ENABLED=0` 且无 gcc）→ 竞态检测**不可用**，按约定仅作信息记录 ⚠️ |
| 10 | `gofmt -l main.go internal tools` | 输出为空（`5108d5f` 修复了此前 `styles.go`、`helpers_test.go` 的格式问题） ✅ |
| 11 | `go run . -version` / `-h` | `yhchat-tui 0.1.0`；`-config`/`-server`/`-version` 用法正确 ✅ |
| 12 | 在 `5108d5f` 上新建真实 `git worktree add --detach HEAD` 并检查行尾 | 6 个 snapshot 文件 0 个含 CR；34 个非 vendor 文本文件 0 个含 CR；该 worktree 内 build/vet/test 全通过（且 `NO_COLOR`/`TERM` 未设置） ✅ |
| 13 | 将 snapshot 文件强制转成 CRLF 后运行 `TestViewSnapshots` | 通过（`snapshot_test.go` 的比较逻辑已做 CRLF 归一化） ✅ |
| 14 | 在 `NO_COLOR` 与 `TERM` 都不设置的干净环境下 `go test ./... -count=1` | 通过（`5108d5f` 的 `TestMain` 已固定灰度测试环境） ✅ |
| 15 | 将配置原子写入的 `config.json.tmp` 路径占为目录制造写失败后执行退出登录 | UI 现在提示 `已退出登录，但清除本地登录状态失败: …`（错误已上浮） ✅ |

交叉编译产物（`5108d5f`，均使用 `-mod=vendor`、`CGO_ENABLED=0`）：

| 目标 | 大小 |
| --- | --- |
| windows/amd64 `yhchat-tui-windows-amd64.exe` | 8,942,080 B |
| windows/arm64 `yhchat-tui-windows-arm64.exe` | 8,179,200 B |
| linux/amd64 `yhchat-tui-linux-amd64` | 8,720,544 B |
| linux/arm64 `yhchat-tui-linux-arm64` | 8,126,624 B |

## 3. 检查清单（Pass / Fail + 证据）

### 3.1 构建与测试

| 项 | 结果 | 证据 |
| --- | --- | --- |
| `go build ./...` / `go vet ./...` / `go test ./...` | ✅ | 第 2 节 #2–#4；workspace 原地复跑同样通过 |
| Windows / Linux × amd64 / arm64 交叉编译（CGO=0） | ✅ | 第 2 节 #5–#6、产物表 |
| 离线 vendored 构建 | ✅ | 第 2 节 #8，空 module cache 构建后仍为 0 文件 |
| race detector | ⚠️ 不可用（非失败） | 第 2 节 #9 |
| 测试确定性（测试套件 hermetic） | ✅（修复后） | 第 2 节 #12–#14；发现 F1/F2 并已复验修复 |

### 3.2 API 契约（对照 `yhchatAPI/src/api/v1/*.md`、`src/full.proto`、`docs/API_NOTES.md`）

| Endpoint | 实现 | 验证结果 |
| --- | --- | --- |
| `POST /v1/user/captcha`（匿名） | `internal/api/client.go:85` | ✅ 方法/路径正确；无 `token` 头、无请求体；响应 `data.b64s` + `data.id` 解析正确 |
| `POST /v1/user/email-login` | `client.go:104` | ✅ Body 字段**精确**为 `{email,password,deviceId,platform}`，无多余字段；不带 token 头；`data.token` 提取 |
| `POST /v1/verification/get-verification-code` | `client.go:119` | ✅ Body 精确为 `{mobile,code,captcha_type,id,platform}`，`captcha_type=""`（图文验证码，与 API_NOTES 实测一致） |
| `POST /v1/user/verification-login` | `client.go:131` | ✅ Body 精确为 `{mobile,captcha,deviceId,platform}` |
| `GET /v1/user/info` | `client.go:147` | ✅ 请求头 `token` 正确；同时处理 `application/json`（`code=-101` 未登录）与 `application/x-protobuf`（`Status.code=2` 未登录）两种响应；JSON 成功体/空 data 报“用户信息为空”不误判 |
| `POST /v1/user/logout` | `client.go:182` | ✅ 带 `token` 头；Body 精确为 `{"device-id": …}`（连字符，符合文档） |
| JSON 信封语义 | `client.go:192-285`、`errors.go` | ✅ 仅 `code==1` 为成功；`-101`/code 2 → `IsSessionExpired`；HTTP 非 200 保留 `HTTPStatus` 并给友好提示；响应不是 JSON 时给出可读错误 |
| Content-Type / timeout / 非 200 | `client.go:207-257` | ✅ 请求 `Content-Type: application/json`；`http.Client.Timeout=30s` + 上下文超时；非 200（含 JSON/HTML 体）不 panic、错误可读 |
| protobuf `UserInfo` 结构 | `internal/api/proto.go:89-273`、`types.go` | ✅ `status=1`, `data=2`；Data 字段 1–12 编号/类型逐一对齐 `full.proto`：`coin=8` 按 **fixed64/double**、`vip_status=11` 按 **enum varint → int32**、`is_vip=9` bool、`avatar_id/vip_expired_timestamp` int64 |
| 未知字段 / 线型不符 / 截断 / 随机输入 | `proto.go:318-382` | ✅ 未知字段（含 group）跳过；错误线型按 proto 语义跳过；合法报文逐字节截断、单 bit 翻转、3000 组随机缓冲均不 panic；空 body 返回错误 |
| 手工解码与 protobuf 语义差异 | `proto.go:124-137` | ℹ️ 同一嵌套消息字段出现多次时，本实现取“最后一次”整体替换，而 protobuf 规范是 merge。真实接口不会拆分 `Status`/`Data`，不构成缺陷；仅记录（见 F5） |

### 3.3 安全与隐私

| 项 | 结果 | 证据 |
| --- | --- | --- |
| 密码不落盘 | ✅ | `config.Config` 字段中无密码（`internal/config/config.go:26-34`）；登录成功后读取配置文件，内容不含密码/`password` 键（overlay 测试断言） |
| 密码不输出/不写日志 | ✅ | 密码仅进入 email-login 请求体；输入框 `EchoPassword`（`login.go:119-132`）；应用代码无日志输出（`main.go` 仅版本/配置提示/运行错误）；失败/成功视图均不含密码（overlay 断言） |
| token 不输出 | ✅ | 所有视图（登录/错误/主页/splash）均不含 token（overlay 断言）；`token` 仅在 HTTP 头、内存、配置文件出现 |
| token 持久化符合文档 | ✅ | 登录成功后写入配置；`Save()` 原子写（tmp + rename） |
| 配置权限 0600（Unix） | ✅（静态代码） | `config.go:107` 目录 `0o700`、`config.go:115`/`config.go:77` 文件 `0o600`；Windows 下权限位无意义（README 已说明由 ACL 保护）。本机无法运行 Linux，未做 Unix 运行时验证 |
| 登出清除 token | ✅（主路径） | 内存 + 磁盘均验证：`l` 后配置文件不再含 token（overlay）；服务器注销失败时也本地清除 |
| 过期清除 token | ✅（主路径） | 自动登录/主页刷新遇到 `-101`/code 2：清 token、回登录页，并验证磁盘文件不再含 token（overlay） |
| 自动登录网络错误不误删 token | ✅ | 500/网络错误时保留 token 并提示“无法验证登录状态”（overlay） |
| 错误信息不含敏感信息 | ✅ | `FriendlyMessage`/`APIError` 只含服务端 msg 或 HTTP 状态；不含密码/token |
| 清除 token 失败 | ✅（已修复提示）/ℹ️ 残留 | 极端情况下磁盘不可写：旧版静默失败，`5108d5f` 起提示“清除本地登录状态失败”（`app.go:166/211/221`）。token 仍可能留在磁盘，但用户已被告知（见 F4） |
| mock 工具日志 | ℹ️ | `tools/mockapi/main.go:201` 打印 token 前 12 字符；仅本地联调工具，非产品代码 |

### 3.4 TUI 行为

| 项 | 结果 | 证据 |
| --- | --- | --- |
| 表单校验（邮箱/密码/手机号/图形码/短信码） | ✅ | `login.go:936-961`；仓库测试 `tui_test.go:86-107,137-166` 断言非法输入不发起请求 |
| 焦点遍历（Tab/Shift+Tab/↑↓/Ctrl+T/←→） | ✅ | overlay 断言 email ring：`email→password→submit→tabs→email`；SMS 验证码阶段 ring：`imageCode→submit→tabs→phone`；仓库 `tui_test.go:366-396` |
| 分阶段 Enter 动作 | ✅ | `login.go:396-416`：email→登录；SMS phone→取图形码；captcha→发短信；code→登录；仓库集成测试 `tui_test.go:26-135,109-135` |
| Esc 返回 / 清除提示 | ✅ | `login.go:353-375`；仓库 `tui_test.go:168-194`；overlay 验证先清 notice/error 再回退阶段 |
| `Ctrl+R` 刷新图形验证码 | ✅ | 仅 SMS 生效（`login.go:320-327,431-437`）；仓库 `tui_test.go:196-231`；email 下为 no-op（overlay） |
| `Ctrl+S` 保存 PNG 到临时文件 | ✅ | `login.go:559-582`；仓库测试校验文件存在且非空；无图片时给出错误提示（overlay） |
| 自动登录成功 / 失败 / 过期 | ✅ | `app.go:61-81,152-170`；仓库 `tui_test.go:233-288`；overlay 覆盖“网络错误保留 token”“刷新时过期清 token 并回登录页” |
| 退出登录（含服务器失败） | ✅ | `home.go:96-100`、`app.go:206-217`；仓库 `tui_test.go:290-318`；overlay 覆盖服务器 500 时仍本地清除并提示 |
| 退出快捷键 | ✅ | 任意界面 `Ctrl+C`（`app.go:92-96`）；主页 `q`（`home.go:98-99`）；仓库 `tui_test.go:339-364` |
| 小终端布局自适应 | ⚠️ 已文档化 | 仓库布局测试仅覆盖高度 ≥20（`tui_test.go:400-436`）；overlay 扫描 6 宽度 × 13 高度发现：登录页最小 12 行、SMS 图形码 18 行、SMS 短信码 22 行、主页 14 行才完全不溢出（80×8 时分别 11/18/21/14 行）。不崩溃，超小终端下 alt-screen 滚动；`5108d5f` 已在 README:17/150 明确“建议 ≥24 行”（见 F3） |
| 快照测试有效性 | ✅（修复后） | 快照覆盖登录/错误/SMS 两阶段/主页/splash 共 6 屏；修复后与终端环境/行尾无关。仍保留“快照文件丢失时自动重建并跳过”的逻辑（首次运行便利，但会掩盖误删，见 F5） |

### 3.5 跨平台与离线

| 项 | 结果 | 证据 |
| --- | --- | --- |
| 平台参数映射 | ✅ | `client.go:57-69`：windows→`windows`，linux→`linux`，darwin→`macos` |
| 配置路径 | ✅ | `config.go:42-52` 使用 `os.UserConfigDir()`：Windows 实测默认 `C:\Users\<user>\AppData\Roaming\yhchat-tui\config.json`；Linux 语义为 `$XDG_CONFIG_HOME/yhchat-tui/config.json`（静态代码，runtime 未在 Linux 执行） |
| 无 OS 特定 syscall/hack | ✅ | 项目代码只使用 `runtime.GOOS` 与 stdlib `os`/`net/http`；平台相关实现全部来自 vendored 标准依赖 |
| 无 CGO | ✅ | 4 目标均在 `CGO_ENABLED=0` 下编译 |
| vendor 完整、可离线 | ✅ | 第 2 节 #8 |
| 换行符/Windows 检出 | ✅（修复后） | `.gitattributes`（`* text=auto eol=lf` + 二进制规则）与快照 CRLF 归一化；真实 worktree 检出 0 CRLF 并通过测试（见 F2） |

## 4. 发现的问题（含处置状态）

| ID | 级别 | 位置（发现时） | 描述 | 最小复现 | 状态 |
| --- | --- | --- | --- | --- | --- |
| F1 | **Medium** | `internal/tui/login.go:135-143`（`colorEnabled` 读环境变量）、`internal/tui/snapshot_test.go`（原精确字节比较）、`helpers_test.go`（原 `TestMain` 未固定环境） | 快照测试不 hermetic：图形验证码是否输出 ANSI 颜色取决于运行者的 `NO_COLOR`/`TERM`。在普通彩色终端（`NO_COLOR` 未设置、`TERM` 非 dumb）下 `go test ./internal/tui` 失败：`login_sms_captcha`、`login_sms_code` 两个快照不匹配（彩色 ANSI vs 灰度快照）。本沙箱全局 `NO_COLOR=1` 恰好掩盖了此问题 | `Remove-Item Env:NO_COLOR,Env:TERM; go test ./internal/tui -run TestViewSnapshots` → FAIL | ✅ 已修复（`5108d5f`）：`helpers_test.go:38-41` 在 `TestMain` 固定 `NO_COLOR=1`、`TERM=dumb`；复验：两变量均未设置时全量测试通过 |
| F2 | **Medium** | 仓库根（缺 `.gitattributes`）、`internal/tui/snapshot_test.go`（原比较未归一化） | Windows 上 Git for Windows 默认 `core.autocrlf=true`（本机系统配置即如此）会在检出时把 `.txt` 快照转成 CRLF，导致 `TestViewSnapshots` 在新检出中全部失败。workspace 文件因为是工具写入的 LF 才通过 | 在 `1b69911` 上 `git worktree add --detach <tmp> HEAD` → 快照 4200→4232 字节且含 CR → `go test ./internal/tui -run TestViewSnapshots` FAIL；`git archive HEAD`（默认配置）同样得到 CRLF | ✅ 已修复（`5108d5f`）：新增 `.gitattributes`（`* text=auto eol=lf`）且 `snapshot_test.go:52` 归一化 CRLF；复验真实 worktree：0 个 CRLF 文件、build/vet/test 通过；强制 CRLF 快照也能通过比较 |
| F3 | **Low** | `internal/tui/login.go:602-659,664-678`、`internal/tui/home.go:123-173`；测试 `internal/tui/tui_test.go:400-436` | 极短终端下视图会超出屏幕高度（不崩溃，仅 alt-screen 滚动）：实测最小可完整显示高度为 登录 12、SMS 图形码 18、SMS 短信码 22、主页 14；仓库布局测试只覆盖高度 ≥20，README 原“自适应小终端”表述偏强 | overlay 布局扫描 40–160 宽 × 8–50 高：80×8 时各屏实际行数 11/18/21/14 > 8 | ✅ 已文档化（`5108d5f`）：README:17/150 增加“建议终端 ≥24 行；更小窗口可能滚动、不会崩溃”。行为未改，符合“文档与行为一致” |
| F4 | **Low** | `internal/tui/app.go`（原 `handleAutoLogin`/`handleLogoutDone`/`handleExpired` 中 `_ = a.store.ClearToken()`） | 清除 token 的磁盘写入失败被静默忽略：配置目录不可写时，UI 显示“已退出登录”，但旧 token 仍留在 `config.json`，且用户无感知 | 登录产生 token 后，把 `config.json.tmp` 占为目录制造原子写失败 → 按 `l` 退出 → UI 提示“已退出登录”，但 `config.json` 中 token 仍在（overlay 动态复现） | ✅ 已修复提示（`5108d5f`）：`app.go:166/211/221` 在 notice 中给出“清除本地登录状态失败”；配置 `Load` 在设备 ID 保存失败时也返回可用内存 store + 警告（`config.go:89-100`）。残留：该极端场景下 token 仍可能留在磁盘，但用户已被告知 |
| F5 | ℹ️ Info | 多处 | 非缺陷信息：① `gofmt` 曾对 `styles.go`/`helpers_test.go` 报格式问题——`5108d5f` 已格式化，现 `gofmt -l` 为空；② race detector 因无 cgo/gcc 不可用；③ `proto.go` 对重复嵌套消息字段采用“最后值覆盖”而非 protobuf merge；真实接口不会触发；④ `tools/mockapi/main.go:201` 会打印 token 前 12 字符（仅本地 mock 工具）；⑤ `snapshot_test.go` 在快照文件缺失时会自动创建而不失败，CI 中可能掩盖误删 | — | 记录，不影响验收 |

> 说明：F1/F2 是验证过程中发现的测试基础设施问题，产品二进制与登录主流程行为不受影响；lead 在 `5108d5f` 修复后，本报告已按新 HEAD 重新执行全部关键验证（含真实 worktree 检出与无 `NO_COLOR` 环境）。

## 5. 未能验证 / 限制

1. **真实 API 联调**：沙箱禁止外网，未调用 `chat-go.jwzhd.com`。契约按 `yhchatAPI` 文档与 `docs/API_NOTES.md` 的实测记录核对；例如服务端若按 `Accept` 头做内容协商（客户端发送 `Accept: application/json, application/x-protobuf`）时 `/v1/user/info` 的实际响应类型，无法离线验证。
2. **Linux 运行时**：本机 WSL 无已安装发行版，只做了 `linux/amd64`、`linux/arm64` 交叉编译与 `GOOS=linux go vet`；Unix 下配置文件 0600/0700 仅静态代码验证，未做 Linux 运行时 `stat`。
3. **race detector**：本机无 cgo/gcc，`go test -race` 不可用（信息记录）。
4. **真实终端交互**：未在真实 TTY 中人工操作 24-bit 彩色渲染、Windows 旧版控制台（cmd.exe）按键行为、终端 resize 等；相关行为由仓库测试（强制 Ascii profile）与 overlay 布局扫描覆盖。
5. 验证期间 workspace 由其他成员并行提交（`1b69911`、`5108d5f`）；本报告结论均基于 **`5108d5f`** 重新解包副本与真实 worktree 复核，最终 workspace `git status` 干净。

## 6. 总体判定

**PASS（建议接收）**

- 构建、`go vet`、单元/集成测试、4 平台交叉编译、离线 vendored 构建全部通过；无 CGO；vendor 自洽。
- 登录主路径（邮箱登录、短信登录 + 终端图形验证码、自动登录、过期、退出登录）与官方 API 文档/实测记录一致；protobuf 解码字段与类型正确，畸形输入不 panic。
- 未发现密码/token 泄露到界面、日志或配置文件（token 按设计持久化并在登出/过期时清除）。
- 验证期间发现的 2 个 medium 测试基础设施问题（快照环境依赖、Windows CRLF 检出）已修复并复验；1 个 low（小终端溢出）已文档化；1 个 low（清 token 写盘失败）已实现错误提示。无阻断性问题。
