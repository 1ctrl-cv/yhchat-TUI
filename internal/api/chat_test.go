package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// ---------------------------------------------------------------------------
// protobuf fixtures for tests

func testStatusBytes(code int32, msg string) []byte {
	var b []byte
	b = testAppendVarint(b, 2, uint64(code))
	if msg != "" {
		b = testAppendString(b, 3, msg)
	}
	return b
}

func testAppendString(b []byte, field protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func testAppendBytes(b []byte, field protowire.Number, value []byte) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendBytes(b, value)
}

func testAppendVarint(b []byte, field protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

// testConversationListResponse builds a successful ConversationListResponse
// with two conversations.
func testConversationListResponse() []byte {
	userConv := []byte{}
	userConv = testAppendString(userConv, 1, "1001")
	userConv = testAppendVarint(userConv, 2, 1)
	userConv = testAppendString(userConv, 3, "小明")
	userConv = testAppendString(userConv, 4, `{"text":"晚上见"}`)
	userConv = testAppendVarint(userConv, 5, 1710000000000)
	userConv = testAppendVarint(userConv, 6, 3)
	userConv = testAppendVarint(userConv, 7, 1)
	userConv = testAppendString(userConv, 9, "https://example.com/a.png")
	userConv = testAppendVarint(userConv, 12, 1710000000)
	userConv = testAppendString(userConv, 15, "小明真名")

	groupConv := []byte{}
	groupConv = testAppendString(groupConv, 1, "2002")
	groupConv = testAppendVarint(groupConv, 2, 2)
	groupConv = testAppendString(groupConv, 4, "普通文本预览")
	groupConv = testAppendVarint(groupConv, 6, 0)
	groupConv = testAppendString(groupConv, 15, "测试群")

	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(1, "success"))
	out = testAppendBytes(out, 2, userConv)
	out = testAppendBytes(out, 2, groupConv)
	out = testAppendVarint(out, 3, 2)
	out = testAppendString(out, 4, "md5-value")
	return out
}

// testListMsgResponse builds a successful ListMsgResponse with two messages in
// reverse order (the client must sort them chronologically).
func testListMsgResponse() []byte {
	sender := []byte{}
	sender = testAppendString(sender, 1, "1001")
	sender = testAppendVarint(sender, 2, 1)
	sender = testAppendString(sender, 3, "小明")
	sender = testAppendString(sender, 4, "https://example.com/avatar.png")
	sender = testAppendVarint(sender, 5, 1)

	content := []byte{}
	content = testAppendString(content, 1, "你好")

	msg1 := []byte{}
	msg1 = testAppendString(msg1, 1, "msg-2")
	msg1 = testAppendBytes(msg1, 2, sender)
	msg1 = testAppendString(msg1, 3, "left")
	msg1 = testAppendVarint(msg1, 4, 1)
	msg1 = testAppendBytes(msg1, 5, content)
	msg1 = testAppendVarint(msg1, 6, 1710000002000)
	msg1 = testAppendVarint(msg1, 10, 2)

	msg2 := []byte{}
	msg2 = testAppendString(msg2, 1, "msg-1")
	msg2 = testAppendVarint(msg2, 4, 1)
	msg2 = testAppendBytes(msg2, 5, testAppendString(nil, 1, "第一条"))
	msg2 = testAppendVarint(msg2, 6, 1710000001000)
	msg2 = testAppendString(msg2, 9, "quoted-msg")
	msg2 = testAppendVarint(msg2, 10, 1)
	msg2 = testAppendVarint(msg2, 12, 1710000003000)

	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(1, "success"))
	out = testAppendBytes(out, 2, msg1)
	out = testAppendBytes(out, 2, msg2)
	out = testAppendVarint(out, 3, 2)
	return out
}

// testStatusResponse builds StatusResponse{status}.
func testStatusResponse(code int32, msg string) []byte {
	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(code, msg))
	return out
}

// parseRequestFieldBytes returns the bytes value of a field in a protobuf body.
func parseRequestFieldBytes(t *testing.T, body []byte, field protowire.Number) []byte {
	t.Helper()
	for len(body) > 0 {
		num, typ, n := protowire.ConsumeTag(body)
		if n < 0 {
			t.Fatalf("bad tag in request: %v", protowire.ParseError(n))
		}
		body = body[n:]
		if typ == protowire.BytesType {
			value, m := protowire.ConsumeBytes(body)
			if m < 0 {
				t.Fatalf("bad bytes field: %v", protowire.ParseError(m))
			}
			body = body[m:]
			if num == field {
				return value
			}
			continue
		}
		if num == field && typ == protowire.VarintType {
			t.Fatalf("field %d is varint, caller expected bytes", field)
		}
		m := protowire.ConsumeFieldValue(num, typ, body)
		if m < 0 {
			t.Fatalf("bad field value: %v", protowire.ParseError(m))
		}
		body = body[m:]
	}
	return nil
}

// parseRequestFieldVarint returns the varint value of a field.
func parseRequestFieldVarint(t *testing.T, body []byte, field protowire.Number) uint64 {
	t.Helper()
	for len(body) > 0 {
		num, typ, n := protowire.ConsumeTag(body)
		if n < 0 {
			t.Fatalf("bad tag in request: %v", protowire.ParseError(n))
		}
		body = body[n:]
		if typ == protowire.VarintType {
			value, m := protowire.ConsumeVarint(body)
			if m < 0 {
				t.Fatalf("bad varint field: %v", protowire.ParseError(m))
			}
			body = body[m:]
			if num == field {
				return value
			}
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, body)
		if m < 0 {
			t.Fatalf("bad field value: %v", protowire.ParseError(m))
		}
		body = body[m:]
	}
	return 0
}

// ---------------------------------------------------------------------------
// conversations

func TestConversations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/conversation/list" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("token") != "tok-1" {
			t.Errorf("token header = %q", r.Header.Get("token"))
		}
		body, _ := io.ReadAll(r.Body)
		// A full sync must not send the md5 field; the server would treat any
		// present md5 as "client is up to date" and return an empty list.
		if len(body) != 0 {
			t.Errorf("conversation list request body = %x, want empty", body)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testConversationListResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	conversations, err := client.Conversations(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("Conversations: %v", err)
	}
	if len(conversations) != 2 {
		t.Fatalf("conversations = %d, want 2", len(conversations))
	}
	_, total, err := client.ConversationsWithTotal(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("ConversationsWithTotal: %v", err)
	}
	if total != 2 {
		t.Fatalf("conversation total = %d, want 2", total)
	}
	userConv := conversations[0]
	if userConv.ChatID != "1001" || userConv.ChatType != ChatTypeUser || userConv.Remark != "小明" ||
		userConv.Name != "小明真名" || userConv.Unread != 3 || !userConv.At || userConv.SendTimestamp != 1710000000 {
		t.Fatalf("unexpected user conversation: %+v", userConv)
	}
	if userConv.DisplayName() != "小明" {
		t.Fatalf("display name = %q", userConv.DisplayName())
	}
	if userConv.Preview() != "晚上见" {
		t.Fatalf("preview = %q", userConv.Preview())
	}
	groupConv := conversations[1]
	if groupConv.ChatType != ChatTypeGroup || groupConv.DisplayName() != "测试群" {
		t.Fatalf("unexpected group conversation: %+v", groupConv)
	}
	if groupConv.Preview() != "普通文本预览" {
		t.Fatalf("group preview = %q", groupConv.Preview())
	}
}

func TestConversationsNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testAppendBytes(nil, 1, testStatusBytes(2, "未登录，请登录")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.Conversations(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsSessionExpired(err) {
		t.Fatalf("expected session expired, got %v", err)
	}
}

func TestConversationsJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":-101,"msg":"未登录"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.Conversations(context.Background(), "bad-token")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestConversationsMalformedProto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write([]byte{0xff, 0xff, 0xff})
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.Conversations(context.Background(), "tok-1")
	if err == nil || !strings.Contains(err.Error(), "解析会话列表失败") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// messages

func TestMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/msg/list-message" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if got := parseRequestFieldVarint(t, body, 2); got != 25 {
			t.Errorf("msg_count = %d, want 25", got)
		}
		if got := string(parseRequestFieldBytes(t, body, 3)); got != "before-msg" {
			t.Errorf("msg_id = %q", got)
		}
		if got := parseRequestFieldVarint(t, body, 4); got != 2 {
			t.Errorf("chat_type = %d, want 2", got)
		}
		if got := string(parseRequestFieldBytes(t, body, 5)); got != "2002" {
			t.Errorf("chat_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testListMsgResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	messages, err := client.Messages(context.Background(), "tok-1", "2002", ChatTypeGroup, 25, "before-msg")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	// Must be sorted by msg_seq ascending even though the server returned 2, 1.
	if messages[0].MsgID != "msg-1" || messages[1].MsgID != "msg-2" {
		t.Fatalf("messages not sorted: %s, %s", messages[0].MsgID, messages[1].MsgID)
	}
	first := messages[0]
	if first.Content.Text != "第一条" || first.MsgSeq != 1 || first.QuoteMsgID != "quoted-msg" || !first.Edited {
		t.Fatalf("unexpected first message: %+v", first)
	}
	second := messages[1]
	if second.Sender.Name != "小明" || second.Sender.ID != "1001" || !second.Sender.IsVIP ||
		second.Direction != "left" || second.Content.Text != "你好" || second.TimestampMs != 1710000002000 {
		t.Fatalf("unexpected second message: %+v", second)
	}
	if second.IsOwn() {
		t.Fatal("left message must not be own")
	}
}

func TestMessagesNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testAppendBytes(nil, 1, testStatusBytes(2, "未登录，请登录")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.Messages(context.Background(), "bad-token", "1", ChatTypeUser, 20, "")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// send message

func TestSendTextMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/msg/send-message" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("token") != "tok-1" {
			t.Errorf("token header = %q", r.Header.Get("token"))
		}
		body, _ := io.ReadAll(r.Body)
		msgID := string(parseRequestFieldBytes(t, body, 2))
		if len(msgID) != 32 {
			t.Errorf("msg_id length = %d, want 32 (%q)", len(msgID), msgID)
		}
		if _, err := hex.DecodeString(msgID); err != nil {
			t.Errorf("msg_id is not hex: %v", err)
		}
		if got := string(parseRequestFieldBytes(t, body, 3)); got != "1001" {
			t.Errorf("chat_id = %q", got)
		}
		if got := parseRequestFieldVarint(t, body, 4); got != 1 {
			t.Errorf("chat_type = %d", got)
		}
		content := parseRequestFieldBytes(t, body, 5)
		if got := string(parseRequestFieldBytes(t, content, 1)); got != "你好，世界" {
			t.Errorf("content.text = %q", got)
		}
		if got := parseRequestFieldVarint(t, body, 6); got != 1 {
			t.Errorf("content_type = %d", got)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	msgID, err := client.SendTextMessage(context.Background(), "tok-1", "1001", ChatTypeUser, "  你好，世界  ")
	if err != nil {
		t.Fatalf("SendTextMessage: %v", err)
	}
	if len(msgID) != 32 {
		t.Fatalf("returned msg id = %q", msgID)
	}
}

func TestSendTextMessageEmptyText(t *testing.T) {
	client := newTestClient(t)
	if _, err := client.SendTextMessage(context.Background(), "tok", "1", ChatTypeUser, "   "); err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestSendTextMessageAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(-1, "消息发送太快了"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.SendTextMessage(context.Background(), "tok-1", "1001", ChatTypeUser, "hi")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "消息发送太快了") {
		t.Fatalf("err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// dismiss notification

func TestDismissNotification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/conversation/dismiss-notification" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if payload["chatId"] != "1001" {
			t.Errorf("chatId = %q", payload["chatId"])
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if err := client.DismissNotification(context.Background(), "tok-1", "1001"); err != nil {
		t.Fatalf("DismissNotification: %v", err)
	}
}

// ---------------------------------------------------------------------------
// preview helpers

func TestConversationPreviewPayloads(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"text":"hello"}`, "hello"},
		{`{"content":{"text":"nested"}}`, "nested"},
		{`{"msgContent":{"text":"deep"}}`, "deep"},
		{`"plain json string"`, "plain json string"},
		{`普通文本`, "普通文本"},
		{``, ""},
		{`{"unknown":123}`, ""},
	}
	for _, tc := range cases {
		conv := Conversation{ChatContent: tc.raw}
		if got := conv.Preview(); got != tc.want {
			t.Errorf("Preview(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestMessagePreview(t *testing.T) {
	cases := []struct {
		msg  Message
		want string
	}{
		{Message{ContentType: 1, Content: MessageContent{Text: " 你好  世界 "}}, "你好 世界"},
		{Message{ContentType: 2}, "[图片]"},
		{Message{ContentType: 4, Content: MessageContent{FileName: "a.txt"}}, "[文件] a.txt"},
		{Message{ContentType: 7}, "[表情]"},
		{Message{ContentType: 10}, "[视频]"},
		{Message{ContentType: 11}, "[语音]"},
		{Message{ContentType: 12}, "[视频]"},
		{Message{ContentType: 99, Content: MessageContent{Text: "fallback"}}, "fallback"},
		{Message{ContentType: 99}, "[消息类型 99]"},
	}
	for _, tc := range cases {
		if got := tc.msg.Preview(); got != tc.want {
			t.Errorf("Preview(%+v) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

func TestSortMessagesChronologically(t *testing.T) {
	messages := []Message{
		{MsgID: "b", MsgSeq: 5},
		{MsgID: "a", MsgSeq: 1},
		{MsgID: "c", TimestampMs: 123},
	}
	sortMessagesChronologically(messages)
	if messages[0].MsgID != "c" && messages[0].MsgID != "a" {
		t.Fatalf("unexpected order: %+v", messages)
	}
	seqMessages := []Message{{MsgID: "b", MsgSeq: 5}, {MsgID: "a", MsgSeq: 1}}
	sortMessagesChronologically(seqMessages)
	if seqMessages[0].MsgID != "a" {
		t.Fatalf("seq order: %+v", seqMessages)
	}
}

// keep time import used if future fixtures need timestamps
var _ = time.Now

func TestRecallMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/msg/recall-msg" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if got := string(parseRequestFieldBytes(t, body, 3)); got != "2002" {
			t.Errorf("chat id = %q", got)
		}
		var ids []string
		rest := body
		for len(rest) > 0 {
			num, typ, n := protowire.ConsumeTag(rest)
			if n < 0 {
				break
			}
			rest = rest[n:]
			if typ == protowire.BytesType {
				value, m := protowire.ConsumeBytes(rest)
				if m < 0 {
					break
				}
				if num == 2 {
					ids = append(ids, string(value))
				}
				rest = rest[m:]
				continue
			}
			m := protowire.ConsumeFieldValue(num, typ, rest)
			if m < 0 {
				break
			}
			rest = rest[m:]
		}
		if len(ids) != 2 || ids[0] != "msg-a" || ids[1] != "msg-b" {
			t.Errorf("msg ids = %v", ids)
		}
		if got := parseRequestFieldVarint(t, body, 4); got != 2 {
			t.Errorf("chat type = %d", got)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if err := client.RecallMessages(context.Background(), "tok-1", "2002", ChatTypeGroup, "msg-a", "msg-b"); err != nil {
		t.Fatalf("RecallMessages: %v", err)
	}
	if err := client.RecallMessages(context.Background(), "tok-1", "", ChatTypeGroup, "msg-a"); err == nil {
		t.Fatal("expected error for empty chat id")
	}
	if err := client.RecallMessages(context.Background(), "tok-1", "2002", ChatTypeGroup); err == nil {
		t.Fatal("expected error for empty msg ids")
	}
}
