package api

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// newTestClient returns a client pointed at a test server.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	client := New("device-123")
	client.Platform = "windows"
	client.HTTP = &http.Client{Timeout: 5 * time.Second}
	return client
}

// mustJSON decodes a JSON request body into a map.
func mustJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode body %q: %v", string(raw), err)
	}
	return decoded
}

func writeJSONResp(w http.ResponseWriter, code int, payload string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, payload)
}

func TestFetchImageCaptcha(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/user/captcha" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writeJSONResp(w, 200, `{"code":1,"data":{"b64s":"data:image/png;base64,AAAA","id":"cap-1"},"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	captcha, err := client.FetchImageCaptcha(context.Background())
	if err != nil {
		t.Fatalf("FetchImageCaptcha: %v", err)
	}
	if captcha.ID != "cap-1" || captcha.DataURL != "data:image/png;base64,AAAA" {
		t.Fatalf("unexpected captcha: %+v", captcha)
	}
}

func TestLoginEmailSendsExpectedPayloadAndReturnsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/email-login" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("content-type = %q", got)
		}
		body := mustJSON(t, r)
		if body["email"] != "a@b.com" || body["password"] != "secret" ||
			body["deviceId"] != "device-123" || body["platform"] != "windows" {
			t.Errorf("unexpected login body: %+v", body)
		}
		writeJSONResp(w, 200, `{"code":1,"data":{"token":"tok-1"},"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	token, err := client.LoginEmail(context.Background(), "a@b.com", "secret")
	if err != nil {
		t.Fatalf("LoginEmail: %v", err)
	}
	if token != "tok-1" {
		t.Fatalf("token = %q", token)
	}
}

func TestLoginEmailWrongPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-1,"msg":"密码错误"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.LoginEmail(context.Background(), "a@b.com", "bad")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != -1 || apiErr.Msg != "密码错误" {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
	if got := FriendlyMessage(err); got != "密码错误" {
		t.Fatalf("FriendlyMessage = %q", got)
	}
}

func TestSendSMSCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/verification/get-verification-code" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body := mustJSON(t, r)
		if body["mobile"] != "13800001234" || body["code"] != "abcd" ||
			body["id"] != "cap-1" || body["captcha_type"] != "" || body["platform"] != "windows" {
			t.Errorf("unexpected body: %+v", body)
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if err := client.SendSMSCode(context.Background(), "13800001234", "abcd", "cap-1"); err != nil {
		t.Fatalf("SendSMSCode: %v", err)
	}
}

func TestLoginSMS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/verification-login" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body := mustJSON(t, r)
		if body["mobile"] != "13800001234" || body["captcha"] != "123456" || body["deviceId"] != "device-123" {
			t.Errorf("unexpected body: %+v", body)
		}
		writeJSONResp(w, 200, `{"code":1,"data":{"token":"sms-tok"},"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	token, err := client.LoginSMS(context.Background(), "13800001234", "123456")
	if err != nil {
		t.Fatalf("LoginSMS: %v", err)
	}
	if token != "sms-tok" {
		t.Fatalf("token = %q", token)
	}
}

// userInfoProto builds a UserInfo protobuf payload for tests.
func userInfoProto(t *testing.T) []byte {
	t.Helper()
	var status []byte
	status = protowire.AppendTag(status, 2, protowire.VarintType)
	status = protowire.AppendVarint(status, 1)

	var data []byte
	data = appendStr(data, 1, "42")
	data = appendStr(data, 2, "测试用户")
	data = appendStr(data, 4, "https://example.com/a.png")
	data = appendVarint(data, 5, 12345)
	data = appendStr(data, 6, "13800001234")
	data = appendStr(data, 7, "user@example.com")
	data = protowire.AppendTag(data, 8, protowire.Fixed64Type)
	data = protowire.AppendFixed64(data, math.Float64bits(99.5))
	data = appendVarint(data, 9, 1)
	data = appendVarint(data, 10, 1893456000)
	data = appendVarint(data, 11, 1)
	data = appendStr(data, 12, "INVITE42")

	var out []byte
	out = protowire.AppendTag(out, 1, protowire.BytesType)
	out = protowire.AppendBytes(out, status)
	out = protowire.AppendTag(out, 2, protowire.BytesType)
	out = protowire.AppendBytes(out, data)
	return out
}

func appendStr(b []byte, field protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func appendVarint(b []byte, field protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

func TestUserInfoProtobuf(t *testing.T) {
	payload := userInfoProto(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/user/info" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("token") != "tok-1" {
			t.Errorf("token header = %q", r.Header.Get("token"))
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	info, err := client.UserInfo(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	if info.Status.Code != 1 || info.Status.Msg != "" {
		t.Fatalf("unexpected status: %+v", info.Status)
	}
	d := info.Data
	if d == nil {
		t.Fatal("nil data")
	}
	if d.ID != "42" || d.Name != "测试用户" || d.Phone != "13800001234" ||
		d.Email != "user@example.com" || d.Coin != 99.5 || !d.IsVIP ||
		d.VIPExpiredTimestamp != 1893456000 || d.VIPStatus != 1 || d.InvitationCode != "INVITE42" {
		t.Fatalf("unexpected data: %+v", d)
	}
}

func TestUserInfoJSONNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-101,"msg":"未登录"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.UserInfo(context.Background(), "tok-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestUserInfoProtobufNotLoggedIn(t *testing.T) {
	// Protobuf status code 2 means the token was rejected.
	var status []byte
	status = protowire.AppendTag(status, 2, protowire.VarintType)
	status = protowire.AppendVarint(status, 2)
	status = appendStr(status, 3, "未登录，请登录")
	var payload []byte
	payload = protowire.AppendTag(payload, 1, protowire.BytesType)
	payload = protowire.AppendBytes(payload, status)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.UserInfo(context.Background(), "tok-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestUserInfoEmptyToken(t *testing.T) {
	client := newTestClient(t)
	_, err := client.UserInfo(context.Background(), "")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestLogout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/logout" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("token") != "tok-1" {
			t.Errorf("token header = %q", r.Header.Get("token"))
		}
		body := mustJSON(t, r)
		if body["device-id"] != "device-123" {
			t.Errorf("device-id = %v", body["device-id"])
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if err := client.Logout(context.Background(), "tok-1"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
}

func TestNonJSONResponseReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>bad gateway</html>")
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.LoginEmail(context.Background(), "a@b.com", "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(FriendlyMessage(err), "无法解析服务器响应") {
		t.Fatalf("unexpected friendly message: %q", FriendlyMessage(err))
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.LoginEmail(context.Background(), "a@b.com", "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(FriendlyMessage(err), "HTTP 502") {
		t.Fatalf("unexpected friendly message: %q", FriendlyMessage(err))
	}
}

func TestLoginResponseWithoutToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.LoginEmail(context.Background(), "a@b.com", "x")
	if err == nil || !strings.Contains(err.Error(), "缺少 token") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

// asAPIError is a small errors.As wrapper to keep the test readable.
func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
