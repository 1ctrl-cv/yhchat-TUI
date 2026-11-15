package api

// Group profile / member (群资料 / 群成员) and friend-request (好友申请处理)
// API layer.
//
// Endpoints:
//
//	POST /v1/group/info              获取群聊信息        (protobuf)
//	POST /v1/group/list-member       获取群成员列表      (protobuf)
//	POST /v1/friend/agree-apply      处理添加申请        (JSON)
//	POST /v1/friend/ignore-apply     忽略申请            (JSON)
//	POST /v1/friend/delete-request   删除申请记录        (JSON)
//	POST /v1/friend/delete-friend    删除好友/退群/删机器人 (JSON)
//
// The group endpoints speak protobuf (see groups_proto.go) while the friend
// endpoints are plain JSON; every request is authenticated with the "token"
// request header. Reference: yhchatAPI/src/api/v1/group.md and friend.md.

import (
	"context"
	"fmt"
	"strings"
)

const (
	// groupMemberDefaultPageSize is used when the caller passes a non-positive
	// page size for the member list.
	groupMemberDefaultPageSize = 50
	// groupMemberMaxPageSize caps the member list page size.
	groupMemberMaxPageSize = 100
)

// GroupInfo is the decoded GroupData payload of POST /v1/group/info (群资料).
//
// The wire schema is GroupInfoResponse{status=1, data=2, bot=3}: the bot list
// is not needed by the TUI and is skipped during decoding. Several fields are
// only present when the corresponding group switch is enabled (for example
// Private), so the zero value must be treated as "not set / disabled".
type GroupInfo struct {
	GroupID           string   // group_id=1
	Name              string   // name=2
	AvatarURL         string   // avatar_url=3
	AvatarID          uint64   // avatar_id=4
	Introduction      string   // introduction=5
	Headcount         uint64   // headcount=6, 群人数
	CreateBy          string   // create_by=7, 创建者 ID
	DirectJoin        bool     // direct_join=8, 进群免审核
	PermissionLevel   int32    // permisson_level=9, 群主 100 / 管理员 2
	HistoryMsg        bool     // history_msg=10, 新成员可查看历史消息
	CategoryName      string   // category_name=11, 分类名
	CategoryID        uint64   // category_id=12, 分类 ID
	Private           bool     // private=13, 是否私有群
	DoNotDisturb      bool     // do_not_disturb=14, 免打扰
	CommunityID       uint64   // community_id=15, 所属社区 ID
	CommunityName     string   // community_name=16, 所属社区名
	BanID             int64    // ban_id=17, 封禁 ID
	BanUntilTimestamp int64    // ban_until_timestamp=18, 解禁时间戳
	Top               bool     // top=19, 会话置顶
	AdminIDs          []string // admin=20, 管理员 ID 列表
	GagUntilTimestamp int64    // gag_until_timestamp=21, 禁言截止时间戳，永久禁言为 -1
	LimitedMsgType    string   // limited_msg_type=22, 被限制的消息类型，例如 "1,2,3"
	Owner             string   // owner=23, 群主 ID
	Recommendation    bool     // recommandation=24, 是否加入群推荐
	IsGag             bool     // is_gag=25, 我是否被禁言
	MyGroupNickname   string   // my_group_nickname=28, 我的群昵称
	GroupCode         string   // group_code=29, 群口令
	HideGroupMembers  bool     // hide_group_members=30, 是否隐藏群成员
	BanReason         string   // ban_reason=34, 封禁原因
}

// GroupMember is one entry of the group member list (群成员).
type GroupMember struct {
	GroupID         string // User.group_id=1
	UserID          string // User.user_info.user_id=1
	Name            string // User.user_info.name=2
	AvatarURL       string // User.user_info.avatar_url=4
	IsVIP           bool   // User.user_info.is_vip=6
	PermissionLevel int32  // User.permission_level=3, 群主 100 / 管理员 2
	GagTimestamp    int64  // User.gag_timestamp=4, 禁言时间戳，0 表示未禁言
	IsGag           bool   // User.is_gag=5
}

// GroupInfo fetches the full profile (群资料) of one group.
//
// Endpoint: POST /v1/group/info (protobuf GroupInfoRequest{group_id=2} /
// GroupInfoResponse). A non-1 status code is returned as *APIError, so an
// expired token surfaces through IsSessionExpired.
func (c *Client) GroupInfo(ctx context.Context, token, groupID string) (*GroupInfo, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, fmt.Errorf("群聊 ID 不能为空")
	}
	raw, err := c.postProto(ctx, "/v1/group/info", token, encodeGroupInfoRequest(groupID))
	if err != nil {
		return nil, err
	}
	status, info, err := decodeGroupInfoResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析群信息失败: %w", err)
	}
	if status.Code != 1 {
		return nil, &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	if info == nil {
		return nil, &APIError{Msg: "服务器返回的群信息为空"}
	}
	return info, nil
}

// GroupMembers fetches a page of the members of one group (群成员列表) and the
// total member count.
//
// Endpoint: POST /v1/group/list-member (protobuf). pages are 1-based, size
// defaults to 50 and is capped at 100. keywords, when non-empty, filters the
// members by name / group nickname on the server side.
func (c *Client) GroupMembers(ctx context.Context, token, groupID string, page, size int, keywords string) (members []GroupMember, total int, err error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, 0, fmt.Errorf("群聊 ID 不能为空")
	}
	page, size = normalizeGroupMemberPage(page, size)
	body := encodeListMemberRequest(groupID, int32(page), int32(size), strings.TrimSpace(keywords))
	raw, err := c.postProto(ctx, "/v1/group/list-member", token, body)
	if err != nil {
		return nil, 0, err
	}
	status, members, total, err := decodeListMemberResponse(raw)
	if err != nil {
		return nil, 0, fmt.Errorf("解析群成员列表失败: %w", err)
	}
	if status.Code != 1 {
		return nil, 0, &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return members, total, nil
}

// normalizeGroupMemberPage clamps the member-list paging arguments: pages are
// 1-based, the size defaults to 50 and is capped at 100.
func normalizeGroupMemberPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case size <= 0:
		size = groupMemberDefaultPageSize
	case size > groupMemberMaxPageSize:
		size = groupMemberMaxPageSize
	}
	return page, size
}

// ---------------------------------------------------------------------------
// friend / group / bot add-request handling (JSON)

// agreeApplyRequest is the body of POST /v1/friend/agree-apply.
type agreeApplyRequest struct {
	ID    int32 `json:"id"`    // 申请 ID
	Agree int32 `json:"agree"` // 1 通过 / 2 拒绝 / 3 请求过期 / 4 已解散
}

// ignoreApplyRequest is the body of POST /v1/friend/ignore-apply.
type ignoreApplyRequest struct {
	ID     int32 `json:"id"`     // 申请 ID
	Ignore int32 `json:"ignore"` // 固定为 1
}

// deleteRequestRequest is the body of POST /v1/friend/delete-request.
type deleteRequestRequest struct {
	ID int32 `json:"id"` // 申请 ID
}

// deleteFriendRequest is the body of POST /v1/friend/delete-friend.
type deleteFriendRequest struct {
	ChatID   string `json:"chatId"`   // 目标对象 ID
	ChatType int32  `json:"chatType"` // 1 用户 / 2 群聊 / 3 机器人
}

// AgreeFriendRequest processes one friend/group/bot add request.
//
// agree selects the outcome: 1 通过请求, 2 拒绝请求, 3 显示请求过期, 4 显示已解散.
//
// Endpoint: POST /v1/friend/agree-apply (JSON {id, agree}).
func (c *Client) AgreeFriendRequest(ctx context.Context, token string, requestID int32, agree int32) error {
	if requestID <= 0 {
		return fmt.Errorf("申请 ID 无效")
	}
	if agree < 1 || agree > 4 {
		return fmt.Errorf("处理类型必须是 1（通过）、2（拒绝）、3（请求过期）或 4（已解散）")
	}
	_, err := c.postJSON(ctx, "/v1/friend/agree-apply", token, agreeApplyRequest{ID: requestID, Agree: agree})
	return err
}

// IgnoreFriendRequest ignores one friend/group/bot add request.
//
// Endpoint: POST /v1/friend/ignore-apply (JSON {id, ignore}).
func (c *Client) IgnoreFriendRequest(ctx context.Context, token string, requestID int32) error {
	if requestID <= 0 {
		return fmt.Errorf("申请 ID 无效")
	}
	_, err := c.postJSON(ctx, "/v1/friend/ignore-apply", token, ignoreApplyRequest{ID: requestID, Ignore: 1})
	return err
}

// DeleteFriendRequest deletes one processed/ignored add request from the
// request list.
//
// Endpoint: POST /v1/friend/delete-request (JSON {id}).
func (c *Client) DeleteFriendRequest(ctx context.Context, token string, requestID int32) error {
	if requestID <= 0 {
		return fmt.Errorf("申请 ID 无效")
	}
	_, err := c.postJSON(ctx, "/v1/friend/delete-request", token, deleteRequestRequest{ID: requestID})
	return err
}

// DeleteFriend removes a friend, leaves a group (退群) or deletes a bot from the
// address book.
//
// chatType is 1 用户, 2 群聊 or 3 机器人.
//
// Endpoint: POST /v1/friend/delete-friend (JSON {chatId, chatType}).
func (c *Client) DeleteFriend(ctx context.Context, token, chatID string, chatType ChatType) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return fmt.Errorf("对象 ID 不能为空")
	}
	if chatType != ChatTypeUser && chatType != ChatTypeGroup && chatType != ChatTypeBot {
		return fmt.Errorf("对象类型必须是 1（用户）/ 2（群聊）/ 3（机器人）")
	}
	_, err := c.postJSON(ctx, "/v1/friend/delete-friend", token, deleteFriendRequest{
		ChatID:   chatID,
		ChatType: int32(chatType),
	})
	return err
}
