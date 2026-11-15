package api

// 消息操作 API 的单元测试：通用发送编码、转发、聊天记录搜索、首页搜索、草稿同步。
// 全部离线：HTTP 使用 httptest，WebSocket 使用 net.Pipe 假连接，不访问真实服务。

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// msgActionTestRepeatedStrings 收集 protobuf 消息中某个可重复 bytes 字段的全部值。
func msgActionTestRepeatedStrings(t *testing.T, body []byte, field protowire.Number) []string {
	t.Helper()
	var out []string
	for len(body) > 0 {
		num, typ, n := protowire.ConsumeTag(body)
		if n < 0 {
			t.Fatalf("bad tag: %v", protowire.ParseError(n))
		}
		body = body[n:]
		if typ == protowire.BytesType {
			value, m := protowire.ConsumeBytes(body)
			if m < 0 {
				t.Fatalf("bad bytes field: %v", protowire.ParseError(m))
			}
			body = body[m:]
			if num == field {
				out = append(out, string(value))
			}
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, body)
		if m < 0 {
			t.Fatalf("bad field value: %v", protowire.ParseError(m))
		}
		body = body[m:]
	}
	return out
}

// ---------------------------------------------------------------------------
// 通用发送消息

func TestEncodeMsgActionSendRequest(t *testing.T) {
	body := encodeMsgActionSendRequest("msg-1", "2002", ChatTypeGroup, MessageContent{
		Text:         "你好",
		ImageURL:     "https://example.com/a.png",
		FileName:     "报表.xlsx",
		FileURL:      "file_key_123",
		FileSize:     4096,
		MentionedIDs: []string{"u1", "u2"},
		QuoteMsgText: "小明: 原文",
	}, 1, "q-1")

	if got := string(parseRequestFieldBytes(t, body, 2)); got != "msg-1" {
		t.Errorf("msg_id = %q", got)
	}
	if got := string(parseRequestFieldBytes(t, body, 3)); got != "2002" {
		t.Errorf("chat_id = %q", got)
	}
	if got := parseRequestFieldVarint(t, body, 4); got != 2 {
		t.Errorf("chat_type = %d, want 2", got)
	}
	if got := parseRequestFieldVarint(t, body, 6); got != 1 {
		t.Errorf("content_type = %d, want 1", got)
	}
	if got := string(parseRequestFieldBytes(t, body, 8)); got != "q-1" {
		t.Errorf("quote_msg_id = %q", got)
	}

	content := parseRequestFieldBytes(t, body, 5)
	if got := string(parseRequestFieldBytes(t, content, 1)); got != "你好" {
		t.Errorf("content.text = %q", got)
	}
	if got := string(parseRequestFieldBytes(t, content, 3)); got != "https://example.com/a.png" {
		t.Errorf("content.image_url = %q", got)
	}
	if got := string(parseRequestFieldBytes(t, content, 4)); got != "报表.xlsx" {
		t.Errorf("content.file_name = %q", got)
	}
	if got := string(parseRequestFieldBytes(t, content, 5)); got != "file_key_123" {
		t.Errorf("content.file_url = %q", got)
	}
	if got := parseRequestFieldVarint(t, content, 18); got != 4096 {
		t.Errorf("content.file_size = %d, want 4096", got)
	}
	if got := string(parseRequestFieldBytes(t, content, 8)); got != "小明: 原文" {
		t.Errorf("content.quote_msg_text = %q", got)
	}
	mentions := msgActionTestRepeatedStrings(t, content, 6)
	if len(mentions) != 2 || mentions[0] != "u1" || mentions[1] != "u2" {
		t.Errorf("content.mentioned_id = %v", mentions)
	}

	// 文本 + 不引用消息时不应编码 quote_msg_id，也不应编码空的 content 字段。
	minimal := encodeMsgActionSendRequest("msg-2", "1001", ChatTypeUser, MessageContent{Text: "hi"}, ContentTypeText, "")
	if got := parseRequestFieldBytes(t, minimal, 8); len(got) != 0 {
		t.Errorf("quote_msg_id should be absent, got %q", got)
	}
	minimalContent := parseRequestFieldBytes(t, minimal, 5)
	for _, field := range []protowire.Number{3, 4, 5, 6, 8, 18} {
		if got := parseRequestFieldBytes(t, minimalContent, field); len(got) != 0 {
			t.Errorf("content field %d should be absent, got %q", field, got)
		}
	}
}

func TestSendMessageWithOptions(t *testing.T) {
	var requestMsgID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/msg/send-message" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-1" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-protobuf" {
			t.Errorf("content-type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		requestMsgID = string(parseRequestFieldBytes(t, body, 2))
		if len(requestMsgID) != 32 {
			t.Errorf("msg_id length = %d, want 32", len(requestMsgID))
		}
		if _, err := hex.DecodeString(requestMsgID); err != nil {
			t.Errorf("msg_id is not hex: %v", err)
		}
		if got := string(parseRequestFieldBytes(t, body, 3)); got != "2002" {
			t.Errorf("chat_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	msgID, err := client.SendMessageWithOptions(context.Background(), "tok-1", "2002", ChatTypeGroup,
		MessageContent{Text: "你好"}, ContentTypeText, "")
	if err != nil {
		t.Fatalf("SendMessageWithOptions: %v", err)
	}
	if msgID == "" || msgID != requestMsgID {
		t.Fatalf("returned msg id = %q, request msg id = %q", msgID, requestMsgID)
	}
}

func TestSendMessageWithOptionsSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(2, "未登录"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.SendMessageWithOptions(context.Background(), "bad-token", "2002", ChatTypeGroup,
		MessageContent{Text: "你好"}, ContentTypeText, "")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestSendMessageWithOptionsValidation(t *testing.T) {
	client := newTestClient(t)
	_, err := client.SendMessageWithOptions(context.Background(), "tok-1", "  ", ChatTypeGroup,
		MessageContent{Text: "你好"}, ContentTypeText, "")
	if err == nil || !strings.Contains(err.Error(), "会话 ID 不能为空") {
		t.Fatalf("expected chat id validation error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 转发消息

func TestForwardMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/msg/msg-forward" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-1" {
			t.Errorf("token header = %q", got)
		}
		body := mustJSON(t, r)
		if body["msgId"] != "msg-1" {
			t.Errorf("msgId = %v", body["msgId"])
		}
		if got, ok := body["chatType"].(float64); !ok || got != 2 {
			t.Errorf("chatType = %v", body["chatType"])
		}
		receive, ok := body["receive"].([]any)
		if !ok || len(receive) != 2 {
			t.Fatalf("receive = %v", body["receive"])
		}
		first, _ := receive[0].(map[string]any)
		second, _ := receive[1].(map[string]any)
		if first["chatId"] != "bob" || first["chatType"].(float64) != 1 {
			t.Errorf("receive[0] = %v", first)
		}
		if second["chatId"] != "3003" || second["chatType"].(float64) != 2 {
			t.Errorf("receive[1] = %v", second)
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	err := client.ForwardMessage(context.Background(), "tok-1", "msg-1", ChatTypeGroup, []ForwardTarget{
		{ChatID: "bob"}, // ChatType 未知时按用户处理
		{ChatID: "3003", ChatType: ChatTypeGroup},
	})
	if err != nil {
		t.Fatalf("ForwardMessage: %v", err)
	}
}

func TestForwardMessageAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-1,"msg":"转发失败"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	err := client.ForwardMessage(context.Background(), "tok-1", "msg-1", ChatTypeGroup,
		[]ForwardTarget{{ChatID: "bob"}})
	if err == nil || !strings.Contains(err.Error(), "转发失败") {
		t.Fatalf("expected forward error, got %v", err)
	}
}

func TestForwardMessageValidation(t *testing.T) {
	client := newTestClient(t)
	cases := []struct {
		name      string
		msgID     string
		targets   []ForwardTarget
		wantInErr string
	}{
		{"empty msg id", " ", []ForwardTarget{{ChatID: "bob"}}, "消息 ID 不能为空"},
		{"no targets", "msg-1", nil, "转发目标不能为空"},
		{"empty target", "msg-1", []ForwardTarget{{ChatID: "bob"}, {ChatID: " "}}, "第 2 个转发目标的会话 ID 不能为空"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.ForwardMessage(context.Background(), "tok-1", tc.msgID, ChatTypeGroup, tc.targets)
			if err == nil || !strings.Contains(err.Error(), tc.wantInErr) {
				t.Fatalf("expected %q, got %v", tc.wantInErr, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 聊天记录搜索

func TestSearchMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/search/chat-search" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-1" {
			t.Errorf("token header = %q", got)
		}
		body := mustJSON(t, r)
		if body["word"] != "你好" {
			t.Errorf("word = %v", body["word"])
		}
		if body["chatId"] != "2002" {
			t.Errorf("chatId = %v", body["chatId"])
		}
		if got, ok := body["chatType"].(float64); !ok || got != 2 {
			t.Errorf("chatType = %v", body["chatType"])
		}
		if got, ok := body["size"].(float64); !ok || got != 20 {
			t.Errorf("size = %v", body["size"])
		}
		if got, ok := body["time"].(float64); !ok || got != 1710000000000 {
			t.Errorf("time = %v", body["time"])
		}
		if body["type"] != "all" {
			t.Errorf("type = %v", body["type"])
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"list":[
			{"id":"m-1","sequence":42,"chatId":"2002","chatType":0,"name":"小明",
			 "avatarUrl":"https://example.com/a.png","content":"你好呀","type":"1",
			 "time":1710000000000,"timeText":"2024-03-09 00:00:00"},
			{"id":"m-2","sequence":43,"chatId":"2002","name":"小红","content":"图片",
			 "type":2,"time":1710000001000,"timeText":"2024-03-09 00:00:01"}
		]}}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	results, err := client.SearchMessages(context.Background(), "tok-1", "2002", ChatTypeGroup,
		"你好", 20, 1710000000000)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	first := results[0]
	if first.MsgID != "m-1" || first.ChatID != "2002" || first.SenderName != "小明" ||
		first.SenderAvatar != "https://example.com/a.png" || first.Content != "你好呀" ||
		first.Type != "1" || first.Sequence != 42 || first.Time != 1710000000000 ||
		first.TimeText != "2024-03-09 00:00:00" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	// type 为数字时也应能解析为字符串。
	if results[1].Type != "2" {
		t.Fatalf("numeric type = %q, want \"2\"", results[1].Type)
	}
}

func TestSearchMessagesDefaultsAndEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := mustJSON(t, r)
		if got, ok := body["size"].(float64); !ok || got != msgSearchDefaultSize {
			t.Errorf("default size = %v", body["size"])
		}
		if got, ok := body["time"].(float64); !ok || int64(got) != msgSearchAllTime {
			t.Errorf("default time = %v", body["time"])
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"list":null}}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	results, err := client.SearchMessages(context.Background(), "tok-1", "1001", ChatTypeUser, "hi", 0, 0)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %v, want empty", results)
	}
}

func TestSearchMessagesValidation(t *testing.T) {
	client := newTestClient(t)
	if _, err := client.SearchMessages(context.Background(), "tok-1", "2002", ChatTypeGroup, "  ", 10, 0); err == nil ||
		!strings.Contains(err.Error(), "搜索关键词不能为空") {
		t.Fatalf("expected word validation error, got %v", err)
	}
	if _, err := client.SearchMessages(context.Background(), "tok-1", " ", ChatTypeGroup, "hi", 10, 0); err == nil ||
		!strings.Contains(err.Error(), "会话 ID 不能为空") {
		t.Fatalf("expected chat id validation error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 群聊首页搜索

func TestHomeSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/search/home-search" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body := mustJSON(t, r)
		if body["word"] != "测试" {
			t.Errorf("word = %v", body["word"])
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"list":[
			{"title":"用户","list":null},
			{"title":"群组","list":[{"friendId":"2002","friendType":2,"nickname":"测试群",
			 "name":"","avatarUrl":"https://example.com/g.png","hit":1}]},
			{"title":"机器人","list":[{"botId":"3003","name":"测试机器人"}]}
		]}}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	result, err := client.HomeSearch(context.Background(), "tok-1", "测试")
	if err != nil {
		t.Fatalf("HomeSearch: %v", err)
	}
	if len(result.Users) != 0 {
		t.Errorf("users = %v, want empty", result.Users)
	}
	if len(result.Groups) != 1 || result.Groups[0]["nickname"] != "测试群" {
		t.Errorf("groups = %v", result.Groups)
	}
	if len(result.Bots) != 1 || result.Bots[0]["name"] != "测试机器人" {
		t.Errorf("bots = %v", result.Bots)
	}
}

func TestHomeSearchFallbackByIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"list":[
			{"list":[{"kind":"user"}]},
			{"list":[{"kind":"group"}]},
			{"list":[{"kind":"bot"}]},
			{"list":[{"kind":"unknown"}]}
		]}}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	result, err := client.HomeSearch(context.Background(), "tok-1", "测试")
	if err != nil {
		t.Fatalf("HomeSearch: %v", err)
	}
	if len(result.Users) != 1 || result.Users[0]["kind"] != "user" {
		t.Errorf("users = %v", result.Users)
	}
	if len(result.Groups) != 1 || result.Groups[0]["kind"] != "group" {
		t.Errorf("groups = %v", result.Groups)
	}
	if len(result.Bots) != 1 || result.Bots[0]["kind"] != "bot" {
		t.Errorf("bots = %v", result.Bots)
	}
}

func TestHomeSearchValidation(t *testing.T) {
	client := newTestClient(t)
	if _, err := client.HomeSearch(context.Background(), "tok-1", "  "); err == nil ||
		!strings.Contains(err.Error(), "搜索关键词不能为空") {
		t.Fatalf("expected word validation error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 草稿同步

func TestDecodeDraftInput(t *testing.T) {
	raw := testAppendString(nil, 1, "2002")
	raw = testAppendString(raw, 2, "测试草稿")
	// 未知字段（含未知 wire type）应被跳过。
	raw = testAppendString(raw, 9, "ignored")
	raw = testAppendVarint(raw, 10, 1)

	draft, err := DecodeDraftInput(raw)
	if err != nil {
		t.Fatalf("DecodeDraftInput: %v", err)
	}
	if draft.ChatID != "2002" || draft.Input != "测试草稿" {
		t.Fatalf("draft = %+v", draft)
	}

	empty, err := DecodeDraftInput(nil)
	if err != nil || empty == nil || empty.ChatID != "" || empty.Input != "" {
		t.Fatalf("empty draft = %+v, err = %v", empty, err)
	}

	if _, err := DecodeDraftInput([]byte{0xFF, 0xFF}); err == nil ||
		!strings.Contains(err.Error(), "解析草稿同步失败") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestSendInputInfoNotConnected(t *testing.T) {
	client := NewWSClient("http://example.invalid", WSLoginInfo{DeviceID: "dev-1"})
	if err := client.SendInputInfo("2002", "草稿", "dev-1"); !errors.Is(err, ErrWSNotConnected) {
		t.Fatalf("expected ErrWSNotConnected, got %v", err)
	}

	var nilClient *WSClient
	if err := nilClient.SendInputInfo("2002", "草稿", "dev-1"); !errors.Is(err, ErrWSNotConnected) {
		t.Fatalf("nil client: expected ErrWSNotConnected, got %v", err)
	}

	if err := client.SendInputInfo("  ", "草稿", "dev-1"); err == nil ||
		!strings.Contains(err.Error(), "会话 ID 不能为空") {
		t.Fatalf("expected chat id validation error, got %v", err)
	}
}

func TestSendInputInfoWritesPacket(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	client := NewWSClient("http://example.invalid", WSLoginInfo{DeviceID: "dev-login"})
	client.conn = &WSConn{conn: clientConn, br: bufio.NewReader(clientConn)}

	type frameResult struct {
		opcode  byte
		payload []byte
		err     error
	}
	frames := make(chan frameResult, 1)
	go func() {
		opcode, _, payload, err := readWSFrame(bufio.NewReader(serverConn))
		frames <- frameResult{opcode: opcode, payload: payload, err: err}
	}()

	// deviceID 为空时应回退到 WSClient.Login.DeviceID。
	if err := client.SendInputInfo("2002", "测试草稿", ""); err != nil {
		t.Fatalf("SendInputInfo: %v", err)
	}

	select {
	case frame := <-frames:
		if frame.err != nil {
			t.Fatalf("read frame: %v", frame.err)
		}
		if frame.opcode != wsOpText {
			t.Fatalf("opcode = %d, want text", frame.opcode)
		}
		var packet map[string]any
		if err := json.Unmarshal(frame.payload, &packet); err != nil {
			t.Fatalf("decode packet %q: %v", frame.payload, err)
		}
		if packet["cmd"] != "inputInfo" {
			t.Errorf("cmd = %v", packet["cmd"])
		}
		if seq, ok := packet["seq"].(string); !ok || seq == "" {
			t.Errorf("seq = %v", packet["seq"])
		}
		data, _ := packet["data"].(map[string]any)
		if data["chatId"] != "2002" || data["input"] != "测试草稿" || data["deviceId"] != "dev-login" {
			t.Errorf("data = %v", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inputInfo frame")
	}
}
