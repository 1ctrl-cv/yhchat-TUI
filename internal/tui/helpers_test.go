package tui

import (
	"encoding/base64"
	"encoding/json"
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
	if _, err := app.Update(msg); err != nil {
		t.Fatalf("update: %v", err)
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
	app.Update(loginMsg)
}
