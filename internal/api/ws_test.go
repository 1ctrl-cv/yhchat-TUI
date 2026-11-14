package api

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// testWrapper encodes a WsWrapper{info=1{cmd=2}, data=2 Any{value=2}}.
func testWrapper(t *testing.T, cmd string, value []byte) []byte {
	t.Helper()
	info := testAppendString(nil, 2, cmd)
	anyMsg := testAppendBytes(nil, 2, value)
	out := testAppendBytes(nil, 1, info)
	out = testAppendBytes(out, 2, anyMsg)
	return out
}

// testSender encodes a Sender protobuf.
func testSender(t *testing.T, id, name string) []byte {
	t.Helper()
	out := testAppendString(nil, 1, id)
	out = testAppendVarint(out, 2, 1)
	out = testAppendString(out, 3, name)
	return out
}

// testMsgContent encodes a MsgContent protobuf.
func testMsgContent(t *testing.T, text string) []byte {
	t.Helper()
	return testAppendString(nil, 1, text)
}

// testPushMessage encodes a PushMessage protobuf.
func testPushMessage(t *testing.T, msgID, chatID, text string) []byte {
	t.Helper()
	out := testAppendString(nil, 1, msgID)
	out = testAppendBytes(out, 2, testSender(t, "1001", "小明"))
	out = testAppendString(out, 4, chatID)
	out = testAppendVarint(out, 5, 2)
	out = testAppendBytes(out, 6, testMsgContent(t, text))
	out = testAppendVarint(out, 7, 1)
	out = testAppendVarint(out, 8, 1710000000000)
	out = testAppendVarint(out, 12, 42)
	return out
}

func TestDecodeWSWrapper(t *testing.T) {
	raw := testWrapper(t, "push_message", []byte{0x01, 0x02})
	got, err := decodeWSWrapper(raw)
	if err != nil {
		t.Fatalf("decodeWSWrapper: %v", err)
	}
	if got.Cmd != "push_message" {
		t.Fatalf("cmd = %q", got.Cmd)
	}
	if len(got.Value) != 2 || got.Value[0] != 0x01 {
		t.Fatalf("value = %v", got.Value)
	}
}

func TestDecodeWSEvents(t *testing.T) {
	pushPayload := testPushMessage(t, "msg-1", "2002", "你好")
	event := decodeWSEvent(testWrapper(t, "push_message", pushPayload))
	if event.Kind != WSEventPush || event.Push == nil {
		t.Fatalf("event = %+v", event)
	}
	if event.Push.MsgID != "msg-1" || event.Push.ChatID != "2002" || event.Push.ChatType != ChatTypeGroup {
		t.Fatalf("push = %+v", event.Push)
	}
	if event.Push.Content.Text != "你好" || event.Push.ContentType != 1 {
		t.Fatalf("content = %+v", event.Push.Content)
	}
	if event.Push.Sender.Name != "小明" || event.Push.MsgSeq != 42 {
		t.Fatalf("sender/seq = %+v / %d", event.Push.Sender, event.Push.MsgSeq)
	}

	editEvent := decodeWSEvent(testWrapper(t, "edit_message", pushPayload))
	if editEvent.Kind != WSEventEdit || editEvent.Push == nil {
		t.Fatalf("edit event = %+v", editEvent)
	}

	streamPayload := testAppendString(nil, 1, "msg-2")
	streamPayload = testAppendString(streamPayload, 3, "2002")
	streamPayload = testAppendString(streamPayload, 4, "继续")
	streamEvent := decodeWSEvent(testWrapper(t, "stream_message", streamPayload))
	if streamEvent.Kind != WSEventStream || streamEvent.Stream.Content != "继续" {
		t.Fatalf("stream event = %+v", streamEvent)
	}

	heartbeat := decodeWSEvent(testWrapper(t, "heartbeat_ack", nil))
	if heartbeat.Kind != WSEventHeartbeatAck {
		t.Fatalf("heartbeat event = %+v", heartbeat)
	}

	unknown := decodeWSEvent(testWrapper(t, "something_new", nil))
	if unknown.Kind != WSEventUnknown || unknown.Cmd != "something_new" {
		t.Fatalf("unknown event = %+v", unknown)
	}
}

func TestDecodeWSRecall(t *testing.T) {
	recallPayload := testAppendString(nil, 2, "msg-1")
	recallPayload = testAppendString(recallPayload, 2, "msg-2")
	recallPayload = testAppendString(recallPayload, 3, "2002")
	recallPayload = testAppendVarint(recallPayload, 4, 2)
	for _, cmd := range []string{"recall_message", "recalled_message", "revoke_message"} {
		event := decodeWSEvent(testWrapper(t, cmd, recallPayload))
		if event.Kind != WSEventRecall || event.Recall == nil {
			t.Fatalf("%s event = %+v", cmd, event)
		}
		if len(event.Recall.MsgIDs) != 2 || event.Recall.ChatID != "2002" || event.Recall.ChatType != ChatTypeGroup {
			t.Fatalf("%s recall = %+v", cmd, event.Recall)
		}
	}

	// A sparse PushMessage with delete_timestamp behaves like a recall.
	sparse := testAppendString(nil, 1, "msg-9")
	sparse = testAppendString(sparse, 4, "2002")
	sparse = testAppendVarint(sparse, 10, 1710000000000)
	sparseEvent := decodeWSEvent(testWrapper(t, "push_message", sparse))
	if sparseEvent.Kind != WSEventRecall || sparseEvent.Recall == nil || sparseEvent.Recall.MsgIDs[0] != "msg-9" {
		t.Fatalf("sparse recall = %+v", sparseEvent)
	}
}

func TestWSFrameRoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		done <- writeWSFrame(client, wsOpBinary, []byte("hello"))
	}()
	opcode, fin, payload, err := readWSFrame(bufio.NewReader(server))
	if err != nil {
		t.Fatalf("readWSFrame: %v", err)
	}
	if opcode != wsOpBinary || !fin || string(payload) != "hello" {
		t.Fatalf("frame = %d/%v/%q", opcode, fin, payload)
	}
	if err := <-done; err != nil {
		t.Fatalf("writeWSFrame: %v", err)
	}
}

// testWSServer is a minimal server used by the WS client integration test.
type testWSServer struct {
	ln       net.Listener
	pushCmd  string
	pushData []byte
	loginCh  chan map[string]any
}

// newTestWSServer starts a one-connection WebSocket server.
func newTestWSServer(t *testing.T) *testWSServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &testWSServer{ln: ln, loginCh: make(chan map[string]any, 1)}
	t.Cleanup(func() { _ = ln.Close() })
	go srv.serve(t)
	return srv
}

// URL returns the ws:// URL of the test server.
func (s *testWSServer) URL() string { return "ws://" + s.ln.Addr().String() + "/ws" }

// serve accepts one connection, performs the handshake and exchanges frames.
func (s *testWSServer) serve(t *testing.T) {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)

	// Read the HTTP upgrade request.
	requestLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(requestLine, "GET /ws") {
		return
	}
	headers := map[string]string{}
	for {
		line, readErr := br.ReadString('\n')
		if readErr != nil || strings.TrimSpace(line) == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
		}
	}
	key := headers["sec-websocket-key"]
	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: "+accept+"\r\n\r\n")

	// Read the login frame (masked text).
	opcode, _, payload, err := readWSFrame(br)
	if err != nil || opcode != wsOpText {
		return
	}
	var login map[string]any
	if err := json.Unmarshal(payload, &login); err != nil {
		return
	}
	s.loginCh <- login

	// Send a push event.
	if s.pushData != nil {
		_ = writeWSFrame(conn, wsOpBinary, s.pushData)
	}

	// Wait for the heartbeat frame from the client.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		op, _, data, readErr := readWSFrame(br)
		if readErr != nil {
			return
		}
		if op == wsOpText {
			var heartbeat map[string]any
			if json.Unmarshal(data, &heartbeat) == nil && heartbeat["cmd"] == "heartbeat" {
				return
			}
		}
	}
}

func TestWSClientIntegration(t *testing.T) {
	srv := newTestWSServer(t)
	push := testPushMessage(t, "msg-1", "2002", "实时消息")
	srv.pushData = testWrapper(t, "push_message", push)

	client := NewWSClient(srv.URL(), WSLoginInfo{
		UserID:   "100086",
		Token:    "test-token",
		Platform: "windows",
		DeviceID: "dev-1",
	})
	client.hbInterval = 50 * time.Millisecond
	client.Start()
	defer client.Close()

	deadline := time.After(5 * time.Second)
	var sawConnect, sawPush bool
	for !sawConnect || !sawPush {
		select {
		case event, ok := <-client.Events():
			if !ok {
				t.Fatal("events channel closed unexpectedly")
			}
			switch event.Kind {
			case WSEventConnected:
				sawConnect = true
			case WSEventPush:
				sawPush = true
				if event.Push == nil || event.Push.Content.Text != "实时消息" {
					t.Fatalf("push event = %+v", event.Push)
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for events (connected=%v push=%v)", sawConnect, sawPush)
		}
	}

	select {
	case login := <-srv.loginCh:
		if login["cmd"] != "login" {
			t.Fatalf("login cmd = %v", login["cmd"])
		}
		data, _ := login["data"].(map[string]any)
		if data["userId"] != "100086" || data["token"] != "test-token" {
			t.Fatalf("login data = %v", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login frame not received")
	}
}
