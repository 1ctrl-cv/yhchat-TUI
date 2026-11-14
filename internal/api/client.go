// Package api is a small HTTP client for the Yunhu (云湖) social platform API.
//
// Only the endpoints needed by the login flow are implemented:
//
//	POST /v1/user/captcha                       获取图文验证码
//	POST /v1/user/email-login                   邮箱密码登录
//	POST /v1/verification/get-verification-code 发送短信验证码
//	POST /v1/user/verification-login            短信验证码登录
//	GET  /v1/user/info                          获取自身信息 (protobuf)
//	POST /v1/user/logout                        退出登录
//
// References: docs/API_NOTES.md and the upstream documentation repository
// (C:\Users\Admin\Desktop\Codes\yhchatAPI).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the public Yunhu HTTP API endpoint.
	DefaultBaseURL = "https://chat-go.jwzhd.com"
	// DefaultTimeout bounds every individual API request.
	DefaultTimeout = 30 * time.Second
	// maxBodyBytes protects against unexpectedly huge responses.
	maxBodyBytes = 8 << 20
	userAgent    = "yhchat-tui/0.1 (+go)"
)

// Client talks to the Yunhu HTTP API.
type Client struct {
	BaseURL  string
	DeviceID string
	Platform string
	HTTP     *http.Client
}

// New creates a client for the current OS. deviceID must be stable across runs
// (see internal/config) because the API binds login sessions to it.
func New(deviceID string) *Client {
	return &Client{
		BaseURL:  DefaultBaseURL,
		DeviceID: deviceID,
		Platform: DefaultPlatform(),
		HTTP:     &http.Client{Timeout: DefaultTimeout},
	}
}

// DefaultPlatform maps the local OS to a platform value accepted by the API.
func DefaultPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	case "darwin":
		return "macos"
	default:
		return strings.ToLower(runtime.GOOS)
	}
}

// ImageCaptcha is the image captcha required before sending an SMS code.
type ImageCaptcha struct {
	ID      string
	DataURL string // "data:image/png;base64,..."
}

// envelope is the shared JSON response wrapper.
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// FetchImageCaptcha requests a human-readable image captcha.
func (c *Client) FetchImageCaptcha(ctx context.Context) (*ImageCaptcha, error) {
	env, err := c.postJSON(ctx, "/v1/user/captcha", "", nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		B64s string `json:"b64s"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, &APIError{Msg: "验证码响应格式不正确"}
	}
	if data.B64s == "" || data.ID == "" {
		return nil, &APIError{Msg: "验证码响应缺少图片或 ID"}
	}
	return &ImageCaptcha{ID: data.ID, DataURL: data.B64s}, nil
}

// LoginEmail logs in with email + password and returns the account token.
func (c *Client) LoginEmail(ctx context.Context, email, password string) (string, error) {
	env, err := c.postJSON(ctx, "/v1/user/email-login", "", map[string]string{
		"email":    strings.TrimSpace(email),
		"password": password,
		"deviceId": c.DeviceID,
		"platform": c.Platform,
	})
	if err != nil {
		return "", err
	}
	return tokenFrom(env)
}

// SendSMSCode asks the server to send an SMS verification code. captchaCode is
// the text of the image captcha identified by captchaID.
func (c *Client) SendSMSCode(ctx context.Context, mobile, captchaCode, captchaID string) error {
	_, err := c.postJSON(ctx, "/v1/verification/get-verification-code", "", map[string]any{
		"mobile":       strings.TrimSpace(mobile),
		"code":         strings.TrimSpace(captchaCode),
		"captcha_type": "",
		"id":           captchaID,
		"platform":     c.Platform,
	})
	return err
}

// LoginSMS logs in with a mobile number + SMS verification code.
func (c *Client) LoginSMS(ctx context.Context, mobile, smsCode string) (string, error) {
	env, err := c.postJSON(ctx, "/v1/user/verification-login", "", map[string]string{
		"mobile":   strings.TrimSpace(mobile),
		"captcha":  strings.TrimSpace(smsCode),
		"deviceId": c.DeviceID,
		"platform": c.Platform,
	})
	if err != nil {
		return "", err
	}
	return tokenFrom(env)
}

// UserInfo fetches the logged-in account profile. The API answers with a
// protobuf body when the token header is present (and JSON for "no token at
// all"), so both formats are handled here.
func (c *Client) UserInfo(ctx context.Context, token string) (*UserInfo, error) {
	if token == "" {
		return nil, &APIError{Code: -101, Msg: "未登录"}
	}
	raw, header, err := c.do(ctx, http.MethodGet, "/v1/user/info", token, nil)
	if err != nil {
		return nil, err
	}

	contentType := header.Get("Content-Type")
	if strings.Contains(contentType, "json") || looksLikeJSON(raw) {
		var env envelope
		if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
			return nil, &APIError{Msg: "无法解析服务器响应"}
		}
		if env.Code != 1 {
			return nil, &APIError{Code: env.Code, Msg: env.Msg}
		}
		return nil, &APIError{Msg: "服务器返回的用户信息为空"}
	}

	info, err := DecodeUserInfo(raw)
	if err != nil {
		return nil, fmt.Errorf("解析用户信息失败: %w", err)
	}
	if info.Status.Code != 1 {
		return nil, &APIError{Code: int(info.Status.Code), Msg: info.Status.Msg}
	}
	if info.Data == nil {
		return nil, &APIError{Msg: "服务器返回的用户信息为空"}
	}
	return info, nil
}

// Logout invalidates the token for this device.
func (c *Client) Logout(ctx context.Context, token string) error {
	_, err := c.postJSON(ctx, "/v1/user/logout", token, map[string]string{
		"device-id": c.DeviceID,
	})
	return err
}

// ---------------------------------------------------------------------------
// transport helpers

func (c *Client) postJSON(ctx context.Context, path, token string, payload any) (*envelope, error) {
	raw, header, err := c.do(ctx, http.MethodPost, path, token, payload)
	if err != nil {
		return nil, err
	}
	env, err := parseEnvelope(raw, header)
	if err != nil {
		return nil, err
	}
	if env.Code != 1 {
		return env, &APIError{Code: env.Code, Msg: env.Msg}
	}
	return env, nil
}

func (c *Client) do(ctx context.Context, method, path, token string, payload any) ([]byte, http.Header, error) {
	var body []byte
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("编码请求失败: %w", err)
		}
		body = raw
	}
	return c.doRaw(ctx, method, path, token, "application/json", body)
}

// doRaw performs an HTTP request with an explicit content type and raw body.
// body may be nil (no request body).
func (c *Client) doRaw(ctx context.Context, method, path, token, contentType string, body []byte) ([]byte, http.Header, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := strings.TrimRight(base, "/") + path

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("构建请求失败: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json, application/x-protobuf")
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("token", token)
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, resp.Header, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Try to surface the API's own message even on non-200 responses.
		var env envelope
		if json.Unmarshal(raw, &env) == nil && (env.Msg != "" || env.Code != 0) {
			return raw, resp.Header, &APIError{Code: env.Code, Msg: env.Msg, HTTPStatus: resp.StatusCode}
		}
		return raw, resp.Header, &APIError{Msg: "服务器返回错误", HTTPStatus: resp.StatusCode}
	}
	return raw, resp.Header, nil
}

func parseEnvelope(raw []byte, header http.Header) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		if snippet == "" {
			snippet = "空响应"
		}
		_ = header
		return nil, &APIError{Msg: "无法解析服务器响应: " + snippet}
	}
	return &env, nil
}

func tokenFrom(env *envelope) (string, error) {
	if len(env.Data) > 0 {
		var data struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(env.Data, &data); err == nil && data.Token != "" {
			return data.Token, nil
		}
	}
	return "", &APIError{Msg: "登录响应缺少 token"}
}

func looksLikeJSON(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}
