# 云湖 API 探索笔记（本项目实际调用部分）

参考文档：`C:\Users\Admin\Desktop\Codes\yhchatAPI\`（`src/api/v1/user.md`、
`src/api/v1/verification.md`、`src/full.proto`）。

所有请求域名为 `https://chat-go.jwzhd.com`，JSON 接口需要
`Content-Type: application/json`；带 token 的接口在请求头中放 `token: <token>`。

## 已实测确认（2026-11-14，匿名接口）

### POST /v1/user/captcha —— 获取图文验证码（无需 token）

响应 `application/json`：

```json
{"code":1,"data":{"b64s":"data:image/png;base64,iVBORw0KGgo...","id":"gENc72ATyoSKq8DDMu3g"},"msg":"success"}
```

- 图片为 180x40 PNG，内容为 4 个英文字母/数字（人眼可读，可在终端用半块字符渲染）。
- `id` 会随提交短信验证码接口一起发送。

### POST /v1/verification/get-verification-code —— 发送短信验证码

请求体：

```json
{"mobile":"13800000000","code":"图像验证码","captcha_type":"","id":"验证码ID","platform":"windows"}
```

- 实测错误信息：`{"code":-1,"msg":"图文验证码验证失败"}`（code 为图像验证码内容错误）。
- `captcha_type` 留空即使用图文验证码；`slide` 表示滑动验证码（本项目 TUI 不易交互，未实现）。

### POST /v1/user/verification-login —— 短信验证码登录

请求体：

```json
{"mobile":"13800000000","captcha":"短信验证码","deviceId":"<设备ID>","platform":"windows"}
```

响应成功：`{"code":1,"data":{"token":"f87TJHF9-..."},"msg":"success"}`

### POST /v1/user/email-login —— 邮箱密码登录（本项目主推方式）

请求体：

```json
{"email":"a@b.com","password":"******","deviceId":"<设备ID>","platform":"windows"}
```

响应成功：`{"code":1,"data":{"token":"..."},"msg":"success"}`  
实测错误信息：`{"code":-1,"msg":"密码错误"}`

### GET /v1/user/info —— 获取自身信息（需要 token）

- 请求头携带 `token`。
- 携带 token 时响应为 **protobuf**：`Content-Type: application/x-protobuf`，
  消息结构 `UserInfo`（见 `src/full.proto`）：

```protobuf
message UserInfo {
  Status status = 1;
  message Data {
    string id = 1;
    string name = 2;
    int32 field3 = 3;
    string avatar_url = 4;
    int64 avatar_id = 5;
    string phone = 6;
    string email = 7;
    double coin = 8;
    bool is_vip = 9;
    int64 vip_expired_timestamp = 10;
    VipStatus vip_status = 11;
    string invitation_code = 12;
  }
  Data data = 2;
}
message Status { int64 request_id = 1; int32 code = 2; string msg = 3; }
```

- 实测无效 token 返回体（protobuf，hex）：

```
0a2308d0afd1f7bc9ca6cf4910021a15e69caae799bbe5bd95efbc8ce8afb7e799bbe5bd95
```

  解码：`status.code = 2`，`status.msg = "未登录，请登录"`（byte 0a=field1 len, 08=request_id,
  10=code, 1a=msg）。

- **完全不带 token 请求头**时返回 JSON：`{"code":-101,"msg":"未登录"}`。
  因此客户端必须同时处理 JSON 与 protobuf 两种响应。

### POST /v1/user/logout —— 退出登录

请求头 `token`，请求体 `{"device-id":"<设备ID>"}`，成功 `{"code":1,"msg":"success"}`。

### 通用响应约定

- JSON 响应中 `code == 1` 表示成功，其余为错误，提示信息在 `msg`。
- 未登录时 code 为 `-101`（JSON）或 `2`（protobuf status）。

## WS 登录（后续聊天功能会用到，本次未实现）

`wss://chat-ws-go.jwzhd.com/ws`，先发：

```json
{"seq":"...","cmd":"login","data":{"userId":"...","token":"...","platform":"windows","deviceId":"..."}}
```

允许的 platform：`windows`,`macos`,`android`,`linux`,`ios`,`fuchsia`,`Web`（大小写敏感）。
