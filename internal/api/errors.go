package api

import (
	"errors"
	"fmt"
	"strings"
)

// APIError is a business-level error returned by the Yunhu API. The HTTP call
// itself succeeded (status 200) but the JSON/protobuf envelope reported a
// non-success code.
type APIError struct {
	Code       int
	Msg        string
	HTTPStatus int // non-zero when the failure was an HTTP status error
}

func (e *APIError) Error() string {
	msg := e.Msg
	if msg == "" {
		msg = "未知错误"
	}
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("%s (HTTP %d)", msg, e.HTTPStatus)
	}
	if e.Code != 0 {
		return fmt.Sprintf("%s (code %d)", msg, e.Code)
	}
	return msg
}

// IsSessionExpired reports whether err means the stored token is no longer
// valid. The API uses JSON code -101 and protobuf status code 2 for this.
func IsSessionExpired(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Code == -101 || apiErr.Code == 2 {
		return true
	}
	return strings.Contains(apiErr.Msg, "未登录")
}

// FriendlyMessage converts an error into a short message suitable for the TUI
// status bar.
func FriendlyMessage(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.HTTPStatus != 0 {
			return fmt.Sprintf("服务器请求失败 (HTTP %d)", apiErr.HTTPStatus)
		}
		if apiErr.Msg != "" {
			return apiErr.Msg
		}
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "请求超时，请检查网络后重试"
	case strings.Contains(lower, "connection") || strings.Contains(lower, "network") ||
		strings.Contains(lower, "no such host") || strings.Contains(lower, "eof"):
		return "网络连接失败，请检查网络后重试"
	}
	return msg
}
