package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// UserData is the profile payload returned by POST /v1/user/get-user-data.
type UserData struct {
	ID                int64  `json:"id"`
	UserID            string `json:"userId"`
	LastLoginTime     int64  `json:"lastLoginTime"`
	UpdateTime        int64  `json:"update_time"`
	Introduction      string `json:"introduction"`
	Gender            int32  `json:"gender"`   // 1 男, 2 女, 3 其他
	BirthdayTimestamp int64  `json:"birthday"` // 生日时间戳
	Province          string `json:"province"`
	City              string `json:"city"`
	District          string `json:"district"`
	LocationCode      string `json:"locationCode"`
}

// EditNickname changes the account nickname.
//
// Endpoint: POST /v1/user/edit-nickname (protobuf EditNicknameRequest{name=3}).
func (c *Client) EditNickname(ctx context.Context, token, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("昵称不能为空")
	}
	body := pbAppendString(nil, 3, name)
	raw, err := c.postProto(ctx, "/v1/user/edit-nickname", token, body)
	if err != nil {
		return err
	}
	status, err := decodeStatusResponse(raw)
	if err != nil {
		return fmt.Errorf("解析昵称修改结果失败: %w", err)
	}
	if status.Code != 1 {
		return &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return nil
}

// EditAvatar changes the account avatar URL.
//
// Endpoint: POST /v1/user/edit-avatar (protobuf EditAvatarRequest{url=2}).
func (c *Client) EditAvatar(ctx context.Context, token, avatarURL string) error {
	avatarURL = strings.TrimSpace(avatarURL)
	if avatarURL == "" {
		return fmt.Errorf("头像 URL 不能为空")
	}
	if !strings.HasPrefix(avatarURL, "http://") && !strings.HasPrefix(avatarURL, "https://") {
		return fmt.Errorf("头像 URL 必须以 http(s):// 开头")
	}
	body := pbAppendString(nil, 2, avatarURL)
	raw, err := c.postProto(ctx, "/v1/user/edit-avatar", token, body)
	if err != nil {
		return err
	}
	status, err := decodeStatusResponse(raw)
	if err != nil {
		return fmt.Errorf("解析头像修改结果失败: %w", err)
	}
	if status.Code != 1 {
		return &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return nil
}

// UserData loads the extended profile data of the current account.
//
// Endpoint: POST /v1/user/get-user-data (JSON).
func (c *Client) UserData(ctx context.Context, token string) (*UserData, error) {
	env, err := c.postJSON(ctx, "/v1/user/get-user-data", token, nil)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Data *UserData `json:"data"`
	}
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &wrapper); err != nil {
			return nil, &APIError{Msg: "个人资料响应格式不正确"}
		}
	}
	if wrapper.Data == nil {
		return nil, &APIError{Msg: "服务器返回的个人资料为空"}
	}
	return wrapper.Data, nil
}

// SaveUserData updates the extended profile fields.
//
// Endpoint: POST /v1/user/save-user-data (JSON).
func (c *Client) SaveUserData(ctx context.Context, token string, data UserData) error {
	payload := map[string]any{
		"introduction": data.Introduction,
		"gender":       data.Gender,
		"birthday":     data.BirthdayTimestamp,
		"province":     data.Province,
		"city":         data.City,
		"district":     data.District,
		"locationCode": data.LocationCode,
	}
	_, err := c.postJSON(ctx, "/v1/user/save-user-data", token, payload)
	return err
}

// AddFriend sends an add-friend/add-group/add-bot request.
//
// Endpoint: POST /v1/friend/apply (JSON).
func (c *Client) AddFriend(ctx context.Context, token, chatID string, chatType ChatType, remark string) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return fmt.Errorf("对象 ID 不能为空")
	}
	if chatType != ChatTypeUser && chatType != ChatTypeGroup && chatType != ChatTypeBot {
		return fmt.Errorf("对象类型必须是 1（用户）/ 2（群聊）/ 3（机器人）")
	}
	_, err := c.postJSON(ctx, "/v1/friend/apply", token, map[string]any{
		"chatId":   chatID,
		"chatType": int32(chatType),
		"remark":   strings.TrimSpace(remark),
	})
	return err
}
