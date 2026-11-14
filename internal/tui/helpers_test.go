package tui

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"google.golang.org/protobuf/encoding/protowire"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/config"
)

const (
	testDeviceID   = "0123456789abcdef0123456789abcdef"
	testEmail      = "demo@yunhu.chat"
	testPassword   = "demo1234"
	testSMSCode    = "123456"
	testToken      = "test-token-1"
	testName       = "TUI 测试用户"
	testUserID     = "100086"
	testVIPExpires = int64(1893456000) // 2030-01-01
)

// TestMain forces a plain, colorless terminal profile so views are
// deterministic and comparable as text snapshots regardless of the ambient
// NO_COLOR/TERM settings of the machine running the tests.
func TestMain(m *testing.M) {
	os.Setenv("NO_COLOR", "1")
	os.Setenv("TERM", "dumb")
	lipgloss.SetColorProfile(termenv.Ascii)
	// Freeze time and use UTC so snapshots are deterministic across machines.
	timeLocation = time.UTC
	timeNow = func() time.Time {
		return time.Date(2026, 11, 14, 12, 30, 0, 0, time.UTC)
	}
	os.Exit(m.Run())
}

// testServer is a minimal in-memory mock of the Yunhu HTTP API.
type testServer struct {
	srv *httptest.Server

	mu                 sync.Mutex
	sentSMS            bool
	lastSendSMSPayload map[string]any
	lastLoginToken     string
	loggedOut          bool
	sentMessages       []sentTestMessage
	sendError          string
}

// SetSendError makes the next send-message request fail with the given text.
func (ts *testServer) SetSendError(text string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.sendError = text
}

// sentTestMessage records a text message sent through the mock API.
type sentTestMessage struct {
	ChatID   string
	ChatType int32
	Text     string
	MsgID    string
}

// newTestServer starts the mock server.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	captchaPNG, err := os.ReadFile(filepath.Join("..", "captchaimg", "testdata", "captcha.png"))
	if err != nil {
		t.Fatalf("read captcha fixture: %v", err)
	}

	ts := &testServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/user/captcha", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]any{
			"code": 1, "msg": "success",
			"data": map[string]any{
				"b64s": "data:image/png;base64," + base64.StdEncoding.EncodeToString(captchaPNG),
				"id":   "cap-1",
			},
		})
	})

	mux.HandleFunc("/v1/verification/get-verification-code", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		ts.mu.Lock()
		ts.sentSMS = true
		ts.lastSendSMSPayload = payload
		ts.mu.Unlock()
		writeTestJSON(w, map[string]any{"code": 1, "msg": "success"})
	})

	mux.HandleFunc("/v1/user/email-login", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Email != testEmail || payload.Password != testPassword {
			writeTestJSON(w, map[string]any{"code": -1, "msg": "密码错误"})
			return
		}
		writeTestJSON(w, map[string]any{
			"code": 1, "msg": "success",
			"data": map[string]any{"token": testToken},
		})
	})

	mux.HandleFunc("/v1/user/verification-login", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Mobile  string `json:"mobile"`
			Captcha string `json:"captcha"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Captcha != testSMSCode {
			writeTestJSON(w, map[string]any{"code": -1, "msg": "验证码错误"})
			return
		}
		writeTestJSON(w, map[string]any{
			"code": 1, "msg": "success",
			"data": map[string]any{"token": testToken},
		})
	})

	mux.HandleFunc("/v1/user/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("token") != testToken {
			writeTestJSON(w, map[string]any{"code": -101, "msg": "未登录"})
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testUserInfoProto())
	})

	mux.HandleFunc("/v1/user/logout", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.loggedOut = true
		ts.mu.Unlock()
		writeTestJSON(w, map[string]any{"code": 1, "msg": "success"})
	})

	mux.HandleFunc("/v1/conversation/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("token") != testToken {
			writeTestJSON(w, map[string]any{"code": -101, "msg": "未登录"})
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testConversationListProto())
	})

	mux.HandleFunc("/v1/conversation/dismiss-notification", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]any{"code": 1, "msg": "success"})
	})

	mux.HandleFunc("/v1/msg/list-message", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("token") != testToken {
			writeTestJSON(w, map[string]any{"code": -101, "msg": "未登录"})
			return
		}
		body, _ := io.ReadAll(r.Body)
		chatID := string(pbFieldBytes(body, 5))
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testListMsgResponseProto(chatID))
	})

	mux.HandleFunc("/v1/msg/send-message", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("token") != testToken {
			writeTestJSON(w, map[string]any{"code": -101, "msg": "未登录"})
			return
		}
		body, _ := io.ReadAll(r.Body)
		sent := sentTestMessage{
			MsgID:    string(pbFieldBytes(body, 2)),
			ChatID:   string(pbFieldBytes(body, 3)),
			ChatType: int32(pbFieldVarint(body, 4)),
		}
		content := pbFieldBytes(body, 5)
		sent.Text = string(pbFieldBytes(content, 1))
		ts.mu.Lock()
		sendErr := ts.sendError
		ts.sendError = ""
		ts.sentMessages = append(ts.sentMessages, sent)
		ts.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		if sendErr != "" {
			_, _ = w.Write(testStatusResponseProto(-1, sendErr))
			return
		}
		_, _ = w.Write(testStatusResponseProto(1, "success"))
	})

	ts.srv = httptest.NewServer(mux)
	t.Cleanup(ts.srv.Close)
	return ts
}

// newTestApp builds the root model pointed at the mock server.
func newTestApp(t *testing.T, ts *testServer) (*App, *config.Store) {
	t.Helper()
	store := &config.Store{
		Path: filepath.Join(t.TempDir(), "config.json"),
		Data: config.Config{DeviceID: testDeviceID},
	}
	client := api.New(testDeviceID)
	client.BaseURL = ts.srv.URL
	client.Platform = "windows"
	client.HTTP = &http.Client{Timeout: 5 * time.Second}

	app := New(client, store)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	return app, store
}

// writeTestJSON writes a JSON response.
func writeTestJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

// testUserInfoProto builds the /v1/user/info protobuf response.
func testUserInfoProto() []byte {
	var status []byte
	status = protowire.AppendTag(status, 2, protowire.VarintType)
	status = protowire.AppendVarint(status, 1)
	status = appendTestString(status, 3, "success")

	var data []byte
	data = appendTestString(data, 1, testUserID)
	data = appendTestString(data, 2, testName)
	data = appendTestString(data, 4, "https://example.com/avatar.png")
	data = appendTestVarint(data, 5, 42)
	data = appendTestString(data, 6, "13800001234")
	data = appendTestString(data, 7, "demo@yunhu.chat")
	data = protowire.AppendTag(data, 8, protowire.Fixed64Type)
	data = protowire.AppendFixed64(data, math.Float64bits(123.45))
	data = appendTestVarint(data, 9, 1)
	data = appendTestVarint(data, 10, uint64(testVIPExpires))
	data = appendTestVarint(data, 11, 1)
	data = appendTestString(data, 12, "YH-TEST-0001")

	var out []byte
	out = protowire.AppendTag(out, 1, protowire.BytesType)
	out = protowire.AppendBytes(out, status)
	out = protowire.AppendTag(out, 2, protowire.BytesType)
	out = protowire.AppendBytes(out, data)
	return out
}

func appendTestString(b []byte, field protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func appendTestVarint(b []byte, field protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

// collectMsgs executes a command and flattens tea.BatchMsg results.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, collectMsgs(sub)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// waitFor executes cmd and returns the first message of type T.
func waitFor[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	if cmd == nil {
		t.Fatalf("waitFor[%T]: nil command", *new(T))
	}
	for _, msg := range collectMsgs(cmd) {
		if typed, ok := msg.(T); ok {
			return typed
		}
	}
	var zero T
	t.Fatalf("waitFor: no message of type %T", zero)
	return zero
}

// sendKey feeds a key to the app and returns the resulting command.
func sendKey(t *testing.T, app *App, key tea.KeyType) tea.Cmd {
	t.Helper()
	model, cmd := app.Update(tea.KeyMsg{Type: key})
	if updated, ok := model.(*App); ok {
		*app = *updated
	} else {
		t.Fatalf("unexpected model type %T", model)
	}
	return cmd
}

// setEmailForm fills the email form fields.
func setEmailForm(app *App, email, password string) {
	app.login.email.SetValue(email)
	app.login.password.SetValue(password)
}

// setSMSForm fills the SMS form fields for the given stage.
func setSMSForm(app *App, mobile, imageCode, smsCode string) {
	app.login.phone.SetValue(mobile)
	app.login.imageCode.SetValue(imageCode)
	app.login.smsCode.SetValue(smsCode)
}

// loginEmail runs the full email login flow and returns the app.
func loginEmail(t *testing.T, app *App) {
	t.Helper()
	setEmailForm(app, testEmail, testPassword)
	cmd := sendKey(t, app, tea.KeyEnter)
	msg := waitFor[loginSucceededMsg](t, cmd)
	if msg.token != testToken {
		t.Fatalf("token = %q", msg.token)
	}
	if msg.user == nil || msg.user.Data == nil {
		t.Fatalf("expected profile in login result, got %+v (err=%v)", msg.user, msg.userErr)
	}
	_, cmd = app.Update(msg)
	if cmd == nil {
		t.Fatalf("login: no command to load conversations")
	}
	convMsg := waitFor[conversationsLoadedMsg](t, cmd)
	if convMsg.err != nil {
		t.Fatalf("load conversations: %v", convMsg.err)
	}
	_, msgCmd := app.Update(convMsg)
	loadChatMessages(t, app, msgCmd)
}

// loadChatMessages runs a pending message-history command for the selected chat.
func loadChatMessages(t *testing.T, app *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := waitFor[messagesLoadedMsg](t, cmd)
	if msg.err != nil {
		t.Fatalf("load messages: %v", msg.err)
	}
	app.Update(msg)
}

// openHomeScreen switches the app to the account ("我的") screen.
func openHomeScreen(t *testing.T, app *App) {
	t.Helper()
	_, cmd := app.Update(openHomeMsg{})
	if cmd != nil {
		t.Fatalf("openHomeMsg returned unexpected command")
	}
	if app.screen != screenHome {
		t.Fatalf("screen = %v, want home", app.screen)
	}
}

// loginSMS runs the full SMS login flow (captcha → SMS code → login).
func loginSMS(t *testing.T, app *App) {
	t.Helper()
	// Switch to the SMS tab; this triggers a captcha fetch.
	sendKey(t, app, tea.KeyCtrlT) // focus tab bar
	cmd := sendKey(t, app, tea.KeyRight)
	captchaMsg := waitFor[captchaLoadedMsg](t, cmd)
	if captchaMsg.err != nil {
		t.Fatalf("captcha error: %v", captchaMsg.err)
	}
	if captchaMsg.img == nil {
		t.Fatal("captcha image not decoded")
	}
	app.Update(captchaMsg)
	if app.login.stage != smsStageCaptcha {
		t.Fatalf("stage = %v, want captcha", app.login.stage)
	}

	// Enter image captcha text and request the SMS code.
	setSMSForm(app, "13800001234", "abcd", "")
	cmd = sendKey(t, app, tea.KeyEnter)
	smsMsg := waitFor[smsSentMsg](t, cmd)
	if smsMsg.err != nil {
		t.Fatalf("send sms error: %v", smsMsg.err)
	}
	app.Update(smsMsg)
	if app.login.stage != smsStageCode {
		t.Fatalf("stage = %v, want code", app.login.stage)
	}

	// Enter the SMS code and log in.
	setSMSForm(app, "13800001234", "abcd", testSMSCode)
	cmd = sendKey(t, app, tea.KeyEnter)
	loginMsg := waitFor[loginSucceededMsg](t, cmd)
	_, loadCmd := app.Update(loginMsg)
	if loadCmd == nil {
		t.Fatalf("sms login: no command to load conversations")
	}
	convMsg := waitFor[conversationsLoadedMsg](t, loadCmd)
	if convMsg.err != nil {
		t.Fatalf("load conversations: %v", convMsg.err)
	}
	_, msgCmd := app.Update(convMsg)
	loadChatMessages(t, app, msgCmd)
}

// ---------------------------------------------------------------------------
// Chat API protobuf fixtures for the mock server.

const (
	testGroupChatID = "2002"
	testUserChatID  = "1001"
)

// testConversationListProto builds the /v1/conversation/list protobuf response.
func testConversationListProto() []byte {
	group := []byte{}
	group = pbString(group, 1, testGroupChatID)
	group = pbVarint(group, 2, 2)
	group = pbString(group, 4, `{"text":"大家好"}`)
	group = pbVarint(group, 5, 1710000000000)
	group = pbVarint(group, 6, 2)
	group = pbVarint(group, 7, 1)
	group = pbString(group, 9, "https://example.com/group.png")
	group = pbVarint(group, 12, 1710000000)
	group = pbString(group, 15, "测试群")

	user := []byte{}
	user = pbString(user, 1, testUserChatID)
	user = pbVarint(user, 2, 1)
	user = pbString(user, 3, "小明")
	user = pbString(user, 4, "晚上见")
	user = pbVarint(user, 5, 1709999000000)
	user = pbVarint(user, 12, 1709999000)
	user = pbString(user, 15, "小明")

	var out []byte
	out = pbBytes(out, 1, testPBStatus(1, "success"))
	out = pbBytes(out, 2, group)
	out = pbBytes(out, 2, user)
	out = pbVarint(out, 3, 2)
	out = pbString(out, 4, "md5-test")
	return out
}

// testListMsgResponseProto builds the /v1/msg/list-message protobuf response.
func testListMsgResponseProto(chatID string) []byte {
	var messages [][]byte
	switch chatID {
	case testGroupChatID:
		messages = append(messages,
			testMsgProto("gm-1", testUserChatID, 1, "小明", "left", 1, "大家好", 1710000001000, 1, 0),
			testMsgProto("gm-2", testUserID, 1, testName, "right", 1, "我也在", 1710000002000, 2, 0),
			testMsgProto("gm-3", testUserChatID, 1, "小明", "left", 1, "", 1710000003000, 3, 1710000004000),
		)
	case testUserChatID:
		messages = append(messages,
			testMsgProto("um-1", testUserChatID, 1, "小明", "left", 1, "晚上见", 1709999001000, 1, 0),
			testMsgProto("um-2", testUserID, 1, testName, "right", 1, "好的，明天见", 1709999002000, 2, 0),
		)
	}
	var out []byte
	out = pbBytes(out, 1, testPBStatus(1, "success"))
	for _, message := range messages {
		out = pbBytes(out, 2, message)
	}
	out = pbVarint(out, 3, uint64(len(messages)))
	return out
}

// testMsgProto builds one ListMsgResponse.MsgData protobuf message.
func testMsgProto(msgID, senderID string, senderChatType int32, senderName, direction string,
	contentType int32, text string, timestampMs, seq, recallTimestamp uint64) []byte {
	sender := []byte{}
	sender = pbString(sender, 1, senderID)
	sender = pbVarint(sender, 2, uint64(senderChatType))
	sender = pbString(sender, 3, senderName)

	content := []byte{}
	if text != "" {
		content = pbString(content, 1, text)
	}

	message := []byte{}
	message = pbString(message, 1, msgID)
	message = pbBytes(message, 2, sender)
	message = pbString(message, 3, direction)
	message = pbVarint(message, 4, uint64(contentType))
	message = pbBytes(message, 5, content)
	message = pbVarint(message, 6, timestampMs)
	message = pbVarint(message, 10, seq)
	if recallTimestamp > 0 {
		message = pbVarint(message, 8, recallTimestamp)
	}
	return message
}

// testPBStatus builds a Status protobuf message.
func testPBStatus(code int32, msg string) []byte {
	var status []byte
	status = pbVarint(status, 2, uint64(code))
	if msg != "" {
		status = pbString(status, 3, msg)
	}
	return status
}

// testStatusResponseProto builds StatusResponse{status}.
func testStatusResponseProto(code int32, msg string) []byte {
	var out []byte
	out = pbBytes(out, 1, testPBStatus(code, msg))
	return out
}

// ---------------------------------------------------------------------------
// Protobuf encode/parse helpers for tests.

func pbString(b []byte, field protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func pbBytes(b []byte, field protowire.Number, value []byte) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendBytes(b, value)
}

func pbVarint(b []byte, field protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

// pbFieldBytes returns the bytes value of a protobuf field (nil when absent).
func pbFieldBytes(body []byte, field protowire.Number) []byte {
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

// pbFieldVarint returns the varint value of a protobuf field (0 when absent).
func pbFieldVarint(body []byte, field protowire.Number) uint64 {
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
