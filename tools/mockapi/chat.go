package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// mockMessage is one message of the demo chat history.
type mockMessage struct {
	MsgID       string
	SenderID    string
	SenderName  string
	Direction   string // left / right
	ContentType int32
	Text        string
	TimestampMs uint64
	Seq         uint64
	Recalled    bool
}

// mockConversation is one demo conversation.
type mockConversation struct {
	ChatID        string
	ChatType      int32 // 1 用户, 2 群聊, 3 机器人
	Name          string
	Remark        string
	Unread        int32
	At            bool
	AvatarURL     string
	SendTimestamp uint64 // seconds
	Messages      []mockMessage
}

// chatStore holds the mutable demo chat state.
type chatStore struct {
	mu            sync.Mutex
	conversations []*mockConversation
	nextSeq       uint64
}

// newChatStore builds demo conversations and message history.
func newChatStore() *chatStore {
	now := time.Now()
	msAgo := func(minutes int) uint64 {
		return uint64(now.Add(-time.Duration(minutes) * time.Minute).UnixMilli())
	}

	store := &chatStore{}
	add := func(conv *mockConversation, messages []mockMessage) {
		for i := range messages {
			store.nextSeq++
			messages[i].Seq = store.nextSeq
		}
		conv.Messages = messages
		if len(messages) > 0 {
			last := messages[len(messages)-1]
			conv.SendTimestamp = last.TimestampMs / 1000
		}
		store.conversations = append(store.conversations, conv)
	}

	add(&mockConversation{
		ChatID:    "3001",
		ChatType:  2,
		Name:      "ACGNM 云湖交流群",
		Unread:    2,
		At:        true,
		AvatarURL: "https://chat-img.jwznb.com/demo-group.png",
	}, []mockMessage{
		{MsgID: "demo-1", SenderID: "1001", SenderName: "Kira-chan", Direction: "left", ContentType: 1,
			Text: "欢迎来到云湖 TUI 演示群～", TimestampMs: msAgo(42)},
		{MsgID: "demo-2", SenderID: "1002", SenderName: "TunRat", Direction: "left", ContentType: 1,
			Text: "这是本地 mock 数据，不连云湖服务器。", TimestampMs: msAgo(40)},
		{MsgID: "demo-3", SenderID: demoUserID, SenderName: demoName, Direction: "right", ContentType: 1,
			Text: "收到，主窗口已经能用了！", TimestampMs: msAgo(38)},
		{MsgID: "demo-4", SenderID: "1001", SenderName: "Kira-chan", Direction: "left", ContentType: 1,
			Text: "按 Enter 在输入框里试试发消息吧。", TimestampMs: msAgo(35)},
		{MsgID: "demo-5", SenderID: "1002", SenderName: "TunRat", Direction: "left", ContentType: 1,
			Text: "（这条是演示撤回效果）", TimestampMs: msAgo(33), Recalled: true},
	})

	add(&mockConversation{
		ChatID:    "1001",
		ChatType:  1,
		Name:      "小明",
		Remark:    "小明",
		AvatarURL: "https://chat-img.jwznb.com/demo-user.png",
	}, []mockMessage{
		{MsgID: "demo-10", SenderID: "1001", SenderName: "小明", Direction: "left", ContentType: 1,
			Text: "晚上一起打游戏吗？", TimestampMs: msAgo(120)},
		{MsgID: "demo-11", SenderID: demoUserID, SenderName: demoName, Direction: "right", ContentType: 1,
			Text: "好呀，八点上线。", TimestampMs: msAgo(118)},
		{MsgID: "demo-12", SenderID: "1001", SenderName: "小明", Direction: "left", ContentType: 1,
			Text: "👌", TimestampMs: msAgo(115)},
	})

	add(&mockConversation{
		ChatID:    "4001",
		ChatType:  3,
		Name:      "云湖助手",
		AvatarURL: "https://chat-img.jwznb.com/demo-bot.png",
	}, []mockMessage{
		{MsgID: "demo-20", SenderID: "4001", SenderName: "云湖助手", Direction: "left", ContentType: 1,
			Text: "你好，我是演示机器人。输入 /help 查看可用指令。", TimestampMs: msAgo(240)},
	})

	add(&mockConversation{
		ChatID:    "3002",
		ChatType:  2,
		Name:      "摸鱼闲聊茶馆",
		Unread:    5,
		AvatarURL: "https://chat-img.jwznb.com/demo-tea.png",
	}, []mockMessage{
		{MsgID: "demo-30", SenderID: "1003", SenderName: "糖味堂", Direction: "left", ContentType: 1,
			Text: "今天也是摸鱼的一天。", TimestampMs: msAgo(300)},
	})

	return store
}

// ---------------------------------------------------------------------------
// HTTP handlers

// handleConversationList serves POST /v1/conversation/list.
func (s *server) handleConversationList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupToken(r.Header.Get("token")); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"code": -101, "msg": "未登录"})
		return
	}
	s.chat.mu.Lock()
	payload := encodeConversationList(s.chat.conversations)
	s.chat.mu.Unlock()

	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(payload)
}

// handleListMessage serves POST /v1/msg/list-message.
func (s *server) handleListMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupToken(r.Header.Get("token")); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"code": -101, "msg": "未登录"})
		return
	}
	body, _ := io.ReadAll(r.Body)
	chatID := string(protoFieldBytes(body, 5))

	s.chat.mu.Lock()
	var messages []mockMessage
	for _, conv := range s.chat.conversations {
		if conv.ChatID == chatID {
			messages = append([]mockMessage(nil), conv.Messages...)
			break
		}
	}
	s.chat.mu.Unlock()
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].Seq < messages[j].Seq })

	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(encodeListMsgResponse(messages))
}

// handleSendMessage serves POST /v1/msg/send-message.
func (s *server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupToken(r.Header.Get("token")); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"code": -101, "msg": "未登录"})
		return
	}
	body, _ := io.ReadAll(r.Body)
	msgID := string(protoFieldBytes(body, 2))
	chatID := string(protoFieldBytes(body, 3))
	chatType := int32(protoFieldVarint(body, 4))
	content := protoFieldBytes(body, 5)
	text := string(protoFieldBytes(content, 1))

	if chatID == "" || text == "" {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(encodeStatusResponse(-1, "消息内容不能为空"))
		return
	}

	s.chat.mu.Lock()
	s.chat.nextSeq++
	message := mockMessage{
		MsgID:       msgID,
		SenderID:    demoUserID,
		SenderName:  demoName,
		Direction:   "right",
		ContentType: 1,
		Text:        text,
		TimestampMs: uint64(time.Now().UnixMilli()),
		Seq:         s.chat.nextSeq,
	}
	found := false
	for i, conv := range s.chat.conversations {
		if conv.ChatID != chatID {
			continue
		}
		found = true
		conv.Messages = append(conv.Messages, message)
		conv.SendTimestamp = message.TimestampMs / 1000
		conv.Unread = 0
		conv.At = false
		// Move the conversation to the top like the official client.
		s.chat.conversations = append(s.chat.conversations[:i], s.chat.conversations[i+1:]...)
		s.chat.conversations = append([]*mockConversation{conv}, s.chat.conversations...)
		break
	}
	if !found {
		s.chat.conversations = append([]*mockConversation{{
			ChatID:        chatID,
			ChatType:      chatType,
			Name:          "新会话 " + chatID,
			SendTimestamp: message.TimestampMs / 1000,
			Messages:      []mockMessage{message},
		}}, s.chat.conversations...)
	}
	s.chat.mu.Unlock()

	log.Printf("  send-message chat=%s text=%q", chatID, text)
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(encodeStatusResponse(1, "success"))
}

// handleDismissNotification serves POST /v1/conversation/dismiss-notification.
func (s *server) handleDismissNotification(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ChatID string `json:"chatId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	s.chat.mu.Lock()
	for _, conv := range s.chat.conversations {
		if conv.ChatID == payload.ChatID {
			conv.Unread = 0
			conv.At = false
		}
	}
	s.chat.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": "success"})
}

// ---------------------------------------------------------------------------
// protobuf encoders

func encodeConversationList(conversations []*mockConversation) []byte {
	var out []byte
	out = protoBytes(out, 1, encodeStatus(1, "success"))
	for _, conv := range conversations {
		var data []byte
		data = protoString(data, 1, conv.ChatID)
		data = protoVarint(data, 2, uint64(conv.ChatType))
		if conv.Remark != "" {
			data = protoString(data, 3, conv.Remark)
		}
		preview := ""
		if len(conv.Messages) > 0 {
			preview = `{"text":` + safePreviewJSON(lastPreviewText(conv.Messages[len(conv.Messages)-1])) + `}`
		}
		data = protoString(data, 4, preview)
		data = protoVarint(data, 5, uint64(time.Now().UnixMilli()))
		data = protoVarint(data, 6, uint64(int32ToUint32(conv.Unread)))
		if conv.At {
			data = protoVarint(data, 7, 1)
		}
		data = protoString(data, 9, conv.AvatarURL)
		data = protoVarint(data, 12, conv.SendTimestamp)
		data = protoString(data, 15, conv.Name)
		out = protoBytes(out, 2, data)
	}
	out = protoVarint(out, 3, uint64(len(conversations)))
	out = protoString(out, 4, "mock-md5")
	return out
}

func lastPreviewText(message mockMessage) string {
	if message.Recalled {
		return "撤回了一条消息"
	}
	if message.ContentType == 1 {
		return message.Text
	}
	return "[消息]"
}

func encodeListMsgResponse(messages []mockMessage) []byte {
	var out []byte
	out = protoBytes(out, 1, encodeStatus(1, "success"))
	for _, message := range messages {
		var sender []byte
		sender = protoString(sender, 1, message.SenderID)
		sender = protoVarint(sender, 2, 1)
		sender = protoString(sender, 3, message.SenderName)

		var content []byte
		if message.Text != "" {
			content = protoString(content, 1, message.Text)
		}

		var data []byte
		data = protoString(data, 1, message.MsgID)
		data = protoBytes(data, 2, sender)
		data = protoString(data, 3, message.Direction)
		data = protoVarint(data, 4, uint64(int32ToUint32(message.ContentType)))
		data = protoBytes(data, 5, content)
		data = protoVarint(data, 6, message.TimestampMs)
		if message.Recalled {
			data = protoVarint(data, 8, message.TimestampMs+1000)
		}
		data = protoVarint(data, 10, message.Seq)
		out = protoBytes(out, 2, data)
	}
	out = protoVarint(out, 3, uint64(len(messages)))
	return out
}

func encodeStatusResponse(code int32, msg string) []byte {
	var out []byte
	out = protoBytes(out, 1, encodeStatus(code, msg))
	return out
}

func encodeStatus(code int32, msg string) []byte {
	var status []byte
	status = protoVarint(status, 2, uint64(int32ToUint32(code)))
	status = protoString(status, 3, msg)
	return status
}

func int32ToUint32(v int32) uint32 { return uint32(v) }

func protoString(b []byte, field protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func protoBytes(b []byte, field protowire.Number, value []byte) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendBytes(b, value)
}

func protoVarint(b []byte, field protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

// protoFieldBytes returns the bytes value of a protobuf field.
func protoFieldBytes(body []byte, field protowire.Number) []byte {
	for len(body) > 0 {
		num, typ, n := protowire.ConsumeTag(body)
		if n < 0 {
			return nil
		}
		body = body[n:]
		if typ == protowire.BytesType {
			value, m := protowire.ConsumeBytes(body)
			if m < 0 {
				return nil
			}
			body = body[m:]
			if num == field {
				return value
			}
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, body)
		if m < 0 {
			return nil
		}
		body = body[m:]
	}
	return nil
}

// protoFieldVarint returns the varint value of a protobuf field.
func protoFieldVarint(body []byte, field protowire.Number) uint64 {
	for len(body) > 0 {
		num, typ, n := protowire.ConsumeTag(body)
		if n < 0 {
			return 0
		}
		body = body[n:]
		if typ == protowire.VarintType {
			value, m := protowire.ConsumeVarint(body)
			if m < 0 {
				return 0
			}
			body = body[m:]
			if num == field {
				return value
			}
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, body)
		if m < 0 {
			return 0
		}
		body = body[m:]
	}
	return 0
}

// safePreviewJSON escapes quotes/newlines for the chat_content JSON payload.
func safePreviewJSON(s string) string {
	raw, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(raw)
}
