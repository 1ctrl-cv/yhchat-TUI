package api

// 消息操作 API：通用发送（引用回复 / @ / 多种内容类型）、消息转发、聊天记录搜索、
// 群聊首页搜索，以及草稿同步所需的 WebSocket 发送与解码辅助。
//
// 相关上游文档（../yhchatAPI）：
//   - src/api/v1/msg.md            POST /v1/msg/send-message、POST /v1/msg/msg-forward
//   - src/api/v1/search.md         POST /v1/search/chat-search、POST /v1/search/home-search
//   - src/api/wss/chat-ws-go...md  草稿同步 inputInfo / draft_input
//   - src/full.proto               SendMsgRequest、MsgContent、MsgInput
//
// 本文件只新增类型与函数，不修改 chat.go / ws.go 中已有的类型和实现；
// TUI 收到 draft_input 事件后直接调用 DecodeDraftInput 解码 MsgInput。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// ---------------------------------------------------------------------------
// 通用发送消息

// SendMessageWithOptions 发送一条内容类型由调用方指定的消息，返回生成的消息 ID
// （32 位十六进制字符串，与官方客户端一致）。
//
// contentType 按 full.proto 的 ContentType 填写：1 文本、2 图片、3 Markdown、
// 4 文件、7 表情、8 HTML 等。quoteMsgID 为空表示不引用消息（引用回复时由
// MsgContent.QuoteMsgText 提供引用文本）。@ 通过 MsgContent.MentionedIDs 填写。
//
// 编码 SendMsgRequest{msg_id=2, chat_id=3, chat_type=4, content=5,
// content_type=6, quote_msg_id=8}，其中 MsgContent 编码
// text/image_url/file_name/file_url/file_size/mentioned_id(可重复)/quote_msg_text，
// 因此文本、Markdown、HTML、图片和文件消息都可通过本方法发送。
//
// Endpoint: POST /v1/msg/send-message (protobuf)。
func (c *Client) SendMessageWithOptions(ctx context.Context, token, chatID string, chatType ChatType, content MessageContent, contentType int32, quoteMsgID string) (string, error) {
	if strings.TrimSpace(chatID) == "" {
		return "", fmt.Errorf("会话 ID 不能为空")
	}
	if chatType == ChatTypeUnknown {
		chatType = ChatTypeUser
	}
	msgID, err := newMessageID()
	if err != nil {
		return "", err
	}
	body := encodeMsgActionSendRequest(msgID, chatID, chatType, content, contentType, quoteMsgID)
	raw, err := c.postProto(ctx, "/v1/msg/send-message", token, body)
	if err != nil {
		return "", err
	}
	status, err := decodeStatusResponse(raw)
	if err != nil {
		return "", fmt.Errorf("解析发送结果失败: %w", err)
	}
	if status.Code != 1 {
		return "", &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return msgID, nil
}

// encodeMsgActionSendRequest 编码 SendMsgRequest{msg_id=2, chat_id=3,
// chat_type=4, content=5, content_type=6, quote_msg_id=8}。
func encodeMsgActionSendRequest(msgID, chatID string, chatType ChatType, content MessageContent, contentType int32, quoteMsgID string) []byte {
	var b []byte
	b = pbAppendString(b, 2, msgID)
	b = pbAppendString(b, 3, chatID)
	b = pbAppendVarint(b, 4, uint64(chatType))
	b = pbAppendBytes(b, 5, encodeMsgActionContent(content))
	b = pbAppendVarint(b, 6, uint64(contentType))
	if quote := strings.TrimSpace(quoteMsgID); quote != "" {
		b = pbAppendString(b, 8, quote)
	}
	return b
}

// encodeMsgActionContent 编码 MsgContent 中发送消息会用到的字段：
// text=1、image_url=3、file_name=4、file_url=5、file_size=18、
// mentioned_id=6（可重复）、quote_msg_text=8。零值字段不编码。
func encodeMsgActionContent(content MessageContent) []byte {
	var b []byte
	if content.Text != "" {
		b = pbAppendString(b, contentTextField, content.Text)
	}
	if content.ImageURL != "" {
		b = pbAppendString(b, contentImageURLField, content.ImageURL)
	}
	if content.FileName != "" {
		b = pbAppendString(b, contentFileNameField, content.FileName)
	}
	if content.FileURL != "" {
		b = pbAppendString(b, contentFileURLField, content.FileURL)
	}
	for _, id := range content.MentionedIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		b = pbAppendString(b, contentMentionedField, id)
	}
	if content.QuoteMsgText != "" {
		b = pbAppendString(b, contentQuoteTextField, content.QuoteMsgText)
	}
	if content.FileSize > 0 {
		b = pbAppendVarint(b, contentFileSizeField, uint64(content.FileSize))
	}
	return b
}

// ---------------------------------------------------------------------------
// 转发消息

// ForwardTarget 是一个转发目标会话。
type ForwardTarget struct {
	ChatID   string
	ChatType ChatType
}

// ForwardMessage 把一条消息转发到多个会话。
//
// msgID 是被转发消息的 ID，fromChatType 是该消息所在会话的类别。
// Endpoint: POST /v1/msg/msg-forward (JSON)：
//
//	{"msgId":"...","chatType":2,"receive":[{"chatId":"...","chatType":1}]}
func (c *Client) ForwardMessage(ctx context.Context, token, msgID string, fromChatType ChatType, targets []ForwardTarget) error {
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return fmt.Errorf("消息 ID 不能为空")
	}
	if len(targets) == 0 {
		return fmt.Errorf("转发目标不能为空")
	}
	if fromChatType == ChatTypeUnknown {
		fromChatType = ChatTypeUser
	}
	type forwardReceive struct {
		ChatID   string   `json:"chatId"`
		ChatType ChatType `json:"chatType"`
	}
	receive := make([]forwardReceive, 0, len(targets))
	for i, target := range targets {
		chatID := strings.TrimSpace(target.ChatID)
		if chatID == "" {
			return fmt.Errorf("第 %d 个转发目标的会话 ID 不能为空", i+1)
		}
		chatType := target.ChatType
		if chatType == ChatTypeUnknown {
			chatType = ChatTypeUser
		}
		receive = append(receive, forwardReceive{ChatID: chatID, ChatType: chatType})
	}
	payload := map[string]any{
		"msgId":    msgID,
		"chatType": fromChatType,
		"receive":  receive,
	}
	_, err := c.postJSON(ctx, "/v1/msg/msg-forward", token, payload)
	return err
}

// ---------------------------------------------------------------------------
// 聊天记录搜索

// SearchResult 是一条聊天记录搜索结果。
type SearchResult struct {
	MsgID        string // 消息 ID（响应中的 id）
	ChatID       string // 消息所在会话 ID
	SenderName   string // 发送者名称
	SenderAvatar string // 发送者头像 URL
	Content      string // 命中的消息文本
	Type         string // 消息类别，1-文本，3-markdown，4-文件，6-文章，7-表情，8-html
	TimeText     string // 发送时间文本（服务器格式化好的字符串）
	Sequence     uint64 // 消息序列（第几条消息）
	Time         int64  // 发送时间戳（毫秒）
}

const (
	// msgSearchAllTime 是聊天记录搜索中表示“不限制时间上限”的时间戳。文档示例
	// 使用 9999999999999（约公元 2286 年），首次搜索（beforeTime <= 0）时用它
	// 取回最新记录。
	msgSearchAllTime int64 = 9999999999999
	// msgSearchDefaultSize 是未指定条数时使用的默认搜索数量。
	msgSearchDefaultSize = 30
	// msgSearchTypeAll 表示搜索全部消息类型。
	msgSearchTypeAll = "all"
)

// SearchMessages 在指定会话中搜索聊天记录。
//
// size <= 0 时使用默认值 30；beforeTime > 0 时只返回该时间戳（毫秒）及更早的
// 消息（用于分页），beforeTime <= 0 表示不限制时间上限。
// Endpoint: POST /v1/search/chat-search (JSON)，解析响应 data.list。
func (c *Client) SearchMessages(ctx context.Context, token, chatID string, chatType ChatType, word string, size int, beforeTime int64) ([]SearchResult, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, fmt.Errorf("会话 ID 不能为空")
	}
	if chatType == ChatTypeUnknown {
		chatType = ChatTypeUser
	}
	if size <= 0 {
		size = msgSearchDefaultSize
	}
	if beforeTime <= 0 {
		beforeTime = msgSearchAllTime
	}
	payload := map[string]any{
		"word":      word,
		"chatId":    chatID,
		"chatType":  chatType,
		"type":      msgSearchTypeAll,
		"size":      size,
		"time":      beforeTime,
		"direction": 1, // 文档未说明含义，官方客户端固定发送 1
	}
	env, err := c.postJSON(ctx, "/v1/search/chat-search", token, payload)
	if err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return nil, nil
	}
	var data struct {
		List []struct {
			ID        string              `json:"id"`
			Sequence  uint64              `json:"sequence"`
			ChatID    string              `json:"chatId"`
			Name      string              `json:"name"`
			AvatarURL string              `json:"avatarUrl"`
			Content   string              `json:"content"`
			Type      msgActionFlexString `json:"type"`
			Time      int64               `json:"time"`
			TimeText  string              `json:"timeText"`
		} `json:"list"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("解析聊天记录搜索结果失败: %w", err)
	}
	results := make([]SearchResult, 0, len(data.List))
	for _, item := range data.List {
		results = append(results, SearchResult{
			MsgID:        item.ID,
			ChatID:       item.ChatID,
			SenderName:   item.Name,
			SenderAvatar: item.AvatarURL,
			Content:      item.Content,
			Type:         string(item.Type),
			TimeText:     item.TimeText,
			Sequence:     item.Sequence,
			Time:         item.Time,
		})
	}
	return results, nil
}

// msgActionFlexString 兼容服务器把数字写成 JSON 字符串（"1"）或数字（1）两种情况。
// 文档示例中搜索结果的 type 是字符串，但部分接口会返回数字。
type msgActionFlexString string

// UnmarshalJSON 接受字符串、数字或 null。
func (s *msgActionFlexString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*s = ""
		return nil
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = msgActionFlexString(value)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*s = msgActionFlexString(number.String())
	return nil
}

// ---------------------------------------------------------------------------
// 群聊首页搜索

// HomeSearchResult 是群聊首页搜索的结果，按响应分组的标题分类保存原始条目，
// 便于 TUI 自行渲染和扩展。
type HomeSearchResult struct {
	Users  []map[string]any // 用户分组
	Groups []map[string]any // 群组分组
	Bots   []map[string]any // 机器人分组
}

// HomeSearch 在群聊首页搜索用户、群组和机器人。
//
// Endpoint: POST /v1/search/home-search (JSON)，响应 data.list 是按标题分组的
// 数组（用户 / 群组 / 机器人）；分组标题缺失或未知时按文档顺序
// （0-用户，1-群组，2-机器人）归类，无法归类时忽略该分组。
func (c *Client) HomeSearch(ctx context.Context, token, word string) (*HomeSearchResult, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}
	env, err := c.postJSON(ctx, "/v1/search/home-search", token, map[string]string{
		"word": word,
	})
	if err != nil {
		return nil, err
	}
	result := &HomeSearchResult{}
	if len(env.Data) == 0 {
		return result, nil
	}
	var data struct {
		List []struct {
			Title string           `json:"title"`
			List  []map[string]any `json:"list"`
		} `json:"list"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("解析首页搜索结果失败: %w", err)
	}
	for index, section := range data.List {
		if len(section.List) == 0 {
			continue
		}
		var target *[]map[string]any
		switch homeSearchSectionKind(section.Title, index) {
		case "users":
			target = &result.Users
		case "groups":
			target = &result.Groups
		case "bots":
			target = &result.Bots
		default:
			continue
		}
		for _, item := range section.List {
			if item == nil {
				continue
			}
			*target = append(*target, item)
		}
	}
	return result, nil
}

// homeSearchSectionKind 根据分组标题（缺失时按顺序）判断分组类别，
// 返回 "users" / "groups" / "bots"，无法判断时返回空字符串。
func homeSearchSectionKind(title string, index int) string {
	title = strings.TrimSpace(title)
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(title, "机器人") || strings.Contains(lower, "bot"):
		return "bots"
	case strings.Contains(title, "群") || strings.Contains(lower, "group"):
		return "groups"
	case strings.Contains(title, "用户") || strings.Contains(title, "好友") ||
		strings.Contains(lower, "user") || strings.Contains(lower, "friend"):
		return "users"
	}
	switch index {
	case 0:
		return "users"
	case 1:
		return "groups"
	case 2:
		return "bots"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// 草稿同步（WebSocket）

// ErrWSNotConnected 表示实时连接未建立，草稿同步无法发送。
var ErrWSNotConnected = errors.New("WebSocket 未连接")

// DraftInput 是 WebSocket draft_input 事件的内容（MsgInput）。
type DraftInput struct {
	ChatID string // 草稿所属会话 ID
	Input  string // 草稿内容（服务端行为为覆盖，不是追加）
}

// DecodeDraftInput 解码 draft_input 事件的 google.protobuf.Any.value，
// 即 MsgInput{chat_id=1, input=2}。
//
// WSClient 内部把事件解码为 WSEvent{Cmd:"draft_input"}，但不保留原始
// google.protobuf.Any.value；事件接入由 TUI/WS 层在拿到 value 后调用本函数。
func DecodeDraftInput(value []byte) (*DraftInput, error) {
	draft := &DraftInput{}
	err := forEachField(value, func(field protowire.Number, typ protowire.Type, rest []byte) error {
		if typ != protowire.BytesType {
			return nil
		}
		text, n := protowire.ConsumeString(rest)
		if n < 0 {
			return fmt.Errorf("%w: 草稿字段 %d 格式不正确", errWSDecode, field)
		}
		switch field {
		case 1:
			draft.ChatID = text
		case 2:
			draft.Input = text
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("解析草稿同步失败: %w", err)
	}
	return draft, nil
}

// SendInputInfo 通过实时连接发送一次草稿同步（cmd=inputInfo）。
//
// 负载与官方客户端一致：
//
//	{"seq":"...","cmd":"inputInfo","data":{"chatId":"...","input":"...","deviceId":"..."}}
//
// deviceID 为空时使用 WSClient.Login.DeviceID。连接尚未建立（或已断开）时返回
// ErrWSNotConnected，调用方可忽略草稿同步失败（草稿属于尽力而为的功能）。
func (c *WSClient) SendInputInfo(chatID, input, deviceID string) error {
	if c == nil {
		return ErrWSNotConnected
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return fmt.Errorf("会话 ID 不能为空")
	}
	if deviceID == "" {
		deviceID = c.Login.DeviceID
	}
	// 读取连接指针后立即释放锁：WSConn 自带写锁，Close 也只读取同一指针，
	// 避免在持锁期间做网络写导致 Close 被阻塞。
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return ErrWSNotConnected
	}
	payload := map[string]any{
		"seq": randomWSToken(),
		"cmd": "inputInfo",
		"data": map[string]string{
			"chatId":   chatID,
			"input":    input,
			"deviceId": deviceID,
		},
	}
	if err := conn.WriteJSON(payload); err != nil {
		return fmt.Errorf("发送草稿同步失败: %w", err)
	}
	return nil
}
