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

## 会话 / 消息接口（主窗口使用）

参考文档：`src/api/v1/conversation.md`、`src/api/v1/msg.md`、`src/full.proto`
（`ConversationListRequest/Response`、`ListMsgRequest/Response`、`SendMsgRequest`）。

### POST /v1/conversation/list —— 获取会话列表

- 请求头 `token`，请求体为 protobuf `ConversationListRequest { string md5 = 2; }`
  （客户端首次请求发空 md5 即可；md5 与服务端一致时服务端返回空列表）。
- 响应为 protobuf `ConversationListResponse`：
  - `status = 1`，`data = 2`（repeated），`total = 3`，`md5 = 4`
  - Data 字段：`chat_id=1`、`chat_type=2`（1 用户 / 2 群聊 / 3 机器人）、
    `remark=3`、`chat_content=4`（最新消息，通常为 JSON）、`timestamp_ms=5`、
    `unread_msg=6`、`at=7`、`avatar_id=8`、`avatar_url=9`、`do_not_disturb=11`、
    `send_timestamp=12`（秒）、`is_vip=13`、`at_data=14`、`name=15`、`certification_level=16`。
- 实测（无效 token）：HTTP 200 + `application/x-protobuf`，内容为
  `Status{code=2, msg="未登录，请登录"}`；不带 token 请求头时返回 JSON
  `{"code":-101,"msg":"未登录"}`。请求的 `Content-Type` 服务端未严格校验，
  客户端使用 `application/x-protobuf`。

### POST /v1/msg/list-message —— 列出消息

- 请求体 protobuf `ListMsgRequest { int32 msg_count = 2; string msg_id = 3;
  ChatType chat_type = 4; string chat_id = 5; }`（`msg_id` 为空时取最近消息）。
- 响应 protobuf `ListMsgResponse`：`status=1`、`data=2`（repeated MsgData）、`total=3`。
  MsgData 字段：`msg_id=1`、`sender=2`、`direction=3`（left/right）、`content_type=4`、
  `content=5`（MsgContent）、`send_timestamp_ms=6`、`cmd=7`、`recall_timestamp=8`、
  `quote_msg_id=9`、`msg_seq=10`、`edit_timestamp=12`。
  Sender 字段：`chat_id=1`、`chat_type=2`、`name=3`、`avatar_url=4`、`is_vip=5`。
- 实测（无效 token）同上，返回 protobuf 未登录状态。客户端按 `msg_seq` 升序排序后渲染。

### POST /v1/msg/send-message —— 发送消息

- 请求体 protobuf `SendMsgRequest { string msg_id = 2; string chat_id = 3;
  ChatType chat_type = 4; MsgContent content = 5; ContentType content_type = 6; ... }`，
  文本消息：`content.text=1`、`content_type=1`，`msg_id` 使用 32 位 hex（等价
  `uuid4().hex`）。
- 响应 protobuf `StatusResponse { Status status = 1; }`，`status.code==1` 为成功。
- 实测（无效 token）返回 protobuf 未登录状态，说明请求类型解析正确。

### POST /v1/conversation/dismiss-notification —— 会话标记已读

- 请求头 `token`，JSON 请求体 `{"chatId":"..."}`，响应 JSON `{"code":1,"msg":"success"}`。

### WebSocket（尚未接入，下一阶段）

`wss://chat-ws-go.jwzhd.com/ws`：连接后发送 JSON
`{"seq":"...","cmd":"login","data":{"userId":"...","token":"...","platform":"windows","deviceId":"..."}}`，
随后周期性发送 `heartbeat`。服务端推送为 protobuf `WsWrapper`：
`info=1`（`WsInfo{seq=1,cmd=2}`）+ `data=2`（`google.protobuf.Any`）；按 `info.cmd`
（`push_message` / `edit_message` / `recalled` / `heartbeat_ack` …）解析 Any 的
`value` 字段即可（`type_url` 不可靠，建议按 cmd 分发）。

## 通讯录（好友 / 群聊 / 机器人）

### POST /v1/friend/address-book-list —— 获取通讯录

- 请求头 `token`；请求体 protobuf `AddressBookListRequest { string md5 = 2; }`（空 md5 也要编码，服务端返回全量）。
- 响应 protobuf `AddressBookListResponse`：`status=1`、`data=2`（repeated Data）、`md5=3`。
  - Data 字段：`list_name=1`（"用户"/"我加入的群聊"/"机器人"）、`data=2`（repeated DataList）、`chat_type=3`。
  - DataList 字段：`chat_id=1`、`remark=2`、`avatar_url=3`、`permisson_level=4`（群主 100 / 管理员 2，少一个 i 是上游拼写）、
    `no_disturb=5`、`name=8`。
- 实测（无效 token）：HTTP 200 protobuf `Status{code=2,...}`；不带 token 头为 JSON `{"code":-101}`。

### POST /v1/friend/request-list —— 申请 / 邀请列表

- 请求头 `token`，无请求字段（发送空 protobuf body）。
- 响应 protobuf `RequestListResponse`：`status=1`、`requests=2`（repeated Request）、`total=3`、`pending=4`。
  Request 字段：`receiver_name=1`、`receiver_avatar=2`、`name=3`、`avatar=4`、`group_name=5`、`group_avatar=6`、
  `inviter_id=7`（文档称 8 同义，实测未知）、`source_type=9`、`target_type=10`、`target_id=11`、`receiver_id=12`、
  `result=13`、`processed_at=14`、`invite_at=16`、`invite_at_str=17`、`request_id=18`、`bot_name=19`、`bot_avatar=20`、
  `processor_name=22`、`note=23`、`is_vip=24`。

## 社区（分区 / 文章）—— JSON 接口

以下接口均为 `POST` + 请求头 `token`，请求/响应 JSON，统一 `{code,msg,data}`；`code!=1` 时按 APIError 处理。

- **分区列表** `POST /v1/community/ba/following-ba-list`：请求 `{typ,size,page}`（typ：1 关注 / 2 热门 / 3 我的 / 4 全部）；响应 `data.ba[]`（id/name/avatar/memberNum/postNum/groupNum/createTimeText/isFollowed）与 `data.total`。
- **分区信息** `POST /v1/community/ba/info`：请求 `{id}`；响应 `data.ba`（同上，`isFollowed` 为 "1"/"0"）。
- **文章列表** `POST /v1/community/posts/post-list`：请求 `{typ:1, baId, size, page}`；响应 `data.posts[]`（id/baId/senderId/title/contentType/Content/createTime/…/isSticky/isLiked）与 `data.total`。
- **文章详情** `POST /v1/community/posts/post-detail`：请求 `{id}`；响应 `data.ba`、`data.isAdmin`、`data.post`（含 content/作者/点赞/评论/收藏等）。
- **评论列表** `POST /v1/community/comment/comment-list`：请求 `{postId,size,page}`；响应 `data.comments[]` 与 `data.total`。
- 注意：`isLiked`/`isFollowed` 在文档中同时出现字符串与数字两种形态，客户端解码做了兼容。

## 个人资料 / 账号操作

- **修改昵称** `POST /v1/user/edit-nickname`：protobuf `EditNicknameRequest{name=3}` → `StatusResponse`。
- **修改头像** `POST /v1/user/edit-avatar`：protobuf `EditAvatarRequest{url=2}` → `StatusResponse`。
- **获取个人资料** `POST /v1/user/get-user-data`：JSON，响应 `data.data`（userId/introduction/gender/birthday/province/city/district/locationCode/lastLoginTime/update_time）。
- **保存个人资料** `POST /v1/user/save-user-data`：JSON（当前 TUI 只读展示，接口已封装）。
- **添加好友/群/机器人** `POST /v1/friend/apply`：JSON `{chatId,chatType,remark}`（chatType 1/2/3）。
