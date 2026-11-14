package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChatType identifies what a conversation or message target is.
type ChatType int32

// Chat types used by the Yunhu platform.
const (
	ChatTypeUnknown ChatType = 0
	ChatTypeUser    ChatType = 1
	ChatTypeGroup   ChatType = 2
	ChatTypeBot     ChatType = 3
)

// Label returns the Chinese label of the chat type.
func (t ChatType) Label() string {
	switch t {
	case ChatTypeUser:
		return "用户"
	case ChatTypeGroup:
		return "群聊"
	case ChatTypeBot:
		return "机器人"
	default:
		return "会话"
	}
}

// Conversation is one entry of the conversation (session) list.
type Conversation struct {
	ChatID             string
	ChatType           ChatType
	Remark             string // 备注名
	ChatContent        string // 最新消息内容（通常是 JSON）
	TimestampMs        uint64 // 加入会话列表的时间戳（毫秒）
	Unread             int32  // 未读消息数
	At                 bool   // 是否被 @
	AvatarID           uint64
	AvatarURL          string
	DoNotDisturb       bool
	SendTimestamp      uint64 // 最后一条消息时间戳（秒）
	IsVIP              bool
	CertificationLevel int32
	Name               string // 用户/群真实名称
}

// DisplayName returns the best display name: remark, real name, then chat id.
func (c Conversation) DisplayName() string {
	for _, candidate := range []string{c.Remark, c.Name} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return c.ChatID
}

// Preview returns a one-line preview of the latest message. Structured payloads
// (JSON) return an empty string when no displayable text is found; plain text
// payloads are returned as-is.
func (c Conversation) Preview() string {
	raw := strings.TrimSpace(c.ChatContent)
	if raw == "" {
		return ""
	}
	if text := extractTextPayload(raw); text != "" {
		return collapseWhitespace(text)
	}
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		return ""
	}
	return collapseWhitespace(raw)
}

// Sender describes the author of a message.
type Sender struct {
	ID        string
	ChatType  ChatType
	Name      string
	AvatarURL string
	IsVIP     bool
}

// MessageContent mirrors the MsgContent protobuf message (fields used by the
// TUI; unknown fields are skipped during decoding).
type MessageContent struct {
	Text           string
	ImageURL       string
	FileName       string
	FileURL        string
	FileSize       int64
	Form           string
	QuoteMsgText   string
	Image          string
	PostID         string
	PostTitle      string
	PostContent    string
	PostContentTyp string
	VideoURL       string
	VideoTime      int32
	AudioURL       string
	AudioTime      int32
	Tip            string
	MentionedIDs   []string
	StickerItemID  int64
	StickerPackID  int64
}

// Message is one chat message.
type Message struct {
	MsgID       string
	Sender      Sender
	Direction   string // "left" / "right" (set by the server for the current user)
	ContentType int32  // 1 文本, 2 图片, 3 markdown, 4 文件, 5 表单, 7 表情, 8 html, 11 语音, 13 通话, 14 A2UI
	Content     MessageContent
	TimestampMs uint64
	MsgSeq      uint64
	Recalled    bool
	QuoteMsgID  string
	Edited      bool
}

// ContentTypeText is the content_type value for plain text messages.
const ContentTypeText = int32(1)

// Preview returns a one-line representation of the message.
func (m Message) Preview() string {
	switch m.ContentType {
	case ContentTypeText, 0:
		if text := collapseWhitespace(m.Content.Text); text != "" {
			return text
		}
		return "[文本消息]"
	case 2:
		return "[图片]"
	case 3:
		if text := collapseWhitespace(m.Content.Text); text != "" {
			return text
		}
		return "[Markdown]"
	case 4:
		if name := strings.TrimSpace(m.Content.FileName); name != "" {
			return "[文件] " + name
		}
		return "[文件]"
	case 5:
		return "[表单]"
	case 6:
		if title := strings.TrimSpace(m.Content.PostTitle); title != "" {
			return "[文章] " + title
		}
		return "[文章]"
	case 7:
		return "[表情]"
	case 8:
		if text := collapseWhitespace(m.Content.Text); text != "" {
			return text
		}
		return "[HTML 消息]"
	case 11:
		return "[语音]"
	case 13:
		return "[语音通话]"
	case 14:
		return "[卡片消息]"
	default:
		if text := collapseWhitespace(m.Content.Text); text != "" {
			return text
		}
		return fmt.Sprintf("[消息类型 %d]", m.ContentType)
	}
}

// IsOwn reports whether the server marked the message as sent by the current
// account (direction "right"). Callers may additionally compare Sender.ID.
func (m Message) IsOwn() bool {
	return strings.EqualFold(m.Direction, "right")
}

// extractTextPayload pulls a displayable text out of the latest-message payload
// stored in a conversation. The server usually stores a JSON object there; the
// exact shape is not documented, so this tolerates the common variants.
func extractTextPayload(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "{") {
		var payload any
		if json.Unmarshal([]byte(raw), &payload) == nil {
			if text := firstTextValue(payload, 0); text != "" {
				return text
			}
		}
		return ""
	}
	if strings.HasPrefix(raw, `"`) {
		var text string
		if json.Unmarshal([]byte(raw), &text) == nil {
			return text
		}
	}
	return ""
}

// firstTextValue walks a decoded JSON payload and returns the first useful text.
func firstTextValue(value any, depth int) string {
	if depth > 6 {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"text", "content", "msgContent", "message", "title", "name"} {
			child, ok := v[key]
			if !ok {
				continue
			}
			if text := firstTextValue(child, depth+1); text != "" {
				return text
			}
		}
		for _, child := range v {
			if text := firstTextValue(child, depth+1); text != "" {
				return text
			}
		}
	default:
		return ""
	}
	return ""
}

// collapseWhitespace joins all whitespace runs into single spaces and trims.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
