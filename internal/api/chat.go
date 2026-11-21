package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Conversations fetches the conversation (session) list.
//
// Endpoint: POST /v1/conversation/list (protobuf request/response).
func (c *Client) Conversations(ctx context.Context, token string) ([]Conversation, error) {
	conversations, _, err := c.ConversationsWithTotal(ctx, token)
	return conversations, err
}

// ConversationsWithTotal is Conversations plus the server-reported total. The
// server currently caps the returned list at 200 conversations, so callers can
// detect that older conversations are not included.
func (c *Client) ConversationsWithTotal(ctx context.Context, token string) ([]Conversation, int, error) {
	body := encodeConversationListRequest("")
	raw, err := c.postProto(ctx, "/v1/conversation/list", token, body)
	if err != nil {
		return nil, 0, err
	}
	status, conversations, _, total, err := decodeConversationListResponse(raw)
	if err != nil {
		return nil, 0, fmt.Errorf("解析会话列表失败: %w", err)
	}
	if status.Code != 1 {
		return nil, 0, &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return conversations, total, nil
}

// Messages fetches historical messages of a conversation, oldest first.
//
// Endpoint: POST /v1/msg/list-message (protobuf request/response).
// count is the number of messages to fetch; beforeMsgID, when set, lists the
// messages before that message id (used for paging upwards).
func (c *Client) Messages(ctx context.Context, token, chatID string, chatType ChatType, count int, beforeMsgID string) ([]Message, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, fmt.Errorf("会话 ID 不能为空")
	}
	if chatType == ChatTypeUnknown {
		chatType = ChatTypeUser
	}
	if count <= 0 || count > 100 {
		count = 30
	}
	body := encodeListMsgRequest(int32(count), beforeMsgID, chatType, chatID)
	raw, err := c.postProto(ctx, "/v1/msg/list-message", token, body)
	if err != nil {
		return nil, err
	}
	status, messages, err := decodeListMsgResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析消息列表失败: %w", err)
	}
	if status.Code != 1 {
		return nil, &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	sortMessagesChronologically(messages)
	return messages, nil
}

// SendTextMessage sends a plain text message and returns its generated msg id.
//
// Endpoint: POST /v1/msg/send-message (protobuf request/response).
func (c *Client) SendTextMessage(ctx context.Context, token, chatID string, chatType ChatType, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("消息内容不能为空")
	}
	if strings.TrimSpace(chatID) == "" {
		return "", fmt.Errorf("会话 ID 不能为空")
	}
	if chatType == ChatTypeUnknown {
		chatType = ChatTypeUser
	}
	msgID, err := newMessageID()
	if err != nil {
		return "", err
	}
	body := encodeSendTextMessageRequest(msgID, chatID, chatType, text)
	raw, err := c.postProto(ctx, "/v1/msg/send-message", token, body)
	if err != nil {
		return "", err
	}
	status, err := decodeStatusResponse(raw)
	if err != nil {
		return "", fmt.Errorf("解析发送结果失败: %w", err)
	}
	if status.Code != 1 {
		return "", &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return msgID, nil
}

// DismissNotification marks a conversation as read.
//
// Endpoint: POST /v1/conversation/dismiss-notification (JSON).
func (c *Client) DismissNotification(ctx context.Context, token, chatID string) error {
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("会话 ID 不能为空")
	}
	_, err := c.postJSON(ctx, "/v1/conversation/dismiss-notification", token, map[string]string{
		"chatId": chatID,
	})
	return err
}

// RecallMessages recalls one or more messages sent by the current account.
//
// Endpoint: POST /v1/msg/recall-msg (protobuf RecallMsg).
func (c *Client) RecallMessages(ctx context.Context, token, chatID string, chatType ChatType, msgIDs ...string) error {
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("会话 ID 不能为空")
	}
	if len(msgIDs) == 0 {
		return fmt.Errorf("消息 ID 不能为空")
	}
	if chatType == ChatTypeUnknown {
		chatType = ChatTypeUser
	}
	body := encodeRecallMsgRequest(chatID, chatType, msgIDs)
	raw, err := c.postProto(ctx, "/v1/msg/recall-msg", token, body)
	if err != nil {
		return err
	}
	status, err := decodeStatusResponse(raw)
	if err != nil {
		return fmt.Errorf("解析撤回结果失败: %w", err)
	}
	if status.Code != 1 {
		return &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return nil
}

// newMessageID returns a random 32 hex character message id, matching the
// uuid4().hex values the official clients send.
func newMessageID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("生成消息 ID 失败: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// NewMessageID is the exported form of newMessageID (used by the TUI to insert
// pending messages and by tests).
func NewMessageID() (string, error) { return newMessageID() }

// sortMessagesChronologically orders messages by sequence (falling back to the
// millisecond timestamp) so the chat view renders oldest first regardless of
// the order the server returned.
func sortMessagesChronologically(messages []Message) {
	sort.SliceStable(messages, func(i, j int) bool {
		a, b := messages[i], messages[j]
		if a.MsgSeq != 0 && b.MsgSeq != 0 {
			return a.MsgSeq < b.MsgSeq
		}
		if a.TimestampMs != 0 && b.TimestampMs != 0 {
			return a.TimestampMs < b.TimestampMs
		}
		return false
	})
}

// postProto sends a protobuf request body and returns the raw protobuf
// response. Servers reply with JSON only for envelope-level failures (for
// example a missing token), which is translated into an APIError here.
func (c *Client) postProto(ctx context.Context, path, token string, body []byte) ([]byte, error) {
	raw, _, err := c.doRaw(ctx, http.MethodPost, path, token, "application/x-protobuf", body)
	if err != nil {
		return nil, err
	}
	if looksLikeJSON(raw) {
		var env envelope
		if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
			return nil, &APIError{Msg: "无法解析服务器响应"}
		}
		if env.Code != 1 {
			return nil, &APIError{Code: env.Code, Msg: env.Msg}
		}
		return nil, &APIError{Msg: "服务器返回了非预期的 JSON 响应"}
	}
	return raw, nil
}
