package api

import (
	"context"
	"fmt"
	"strings"
)

// ContactGroup is one section of the address book (通讯录). The server returns
// one group per chat type, ordered as 用户 / 我加入的群聊 / 机器人.
type ContactGroup struct {
	// Name is AddressBookListResponse.Data.list_name, e.g. "用户".
	Name string
	// ChatType is AddressBookListResponse.Data.chat_type
	// (1 用户, 2 群聊, 3 机器人).
	ChatType ChatType
	// Items are the entries of this section, in server order.
	Items []Contact
}

// Contact is one person / group / bot in the address book.
type Contact struct {
	ID              string // chat_id=1
	Remark          string // remark=2
	Name            string // name=8
	AvatarURL       string // avatar_url=3
	PermissionLevel int32  // permisson_level=4（群主=100，管理员=2，其余为 0）
	NoDisturb       bool   // no_disturb=5
}

// DisplayName returns the best display name: remark, then real name, then id.
func (c Contact) DisplayName() string {
	for _, candidate := range []string{c.Remark, c.Name} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return c.ID
}

// InviteRequest is one entry of the friend/invite request list.
type InviteRequest struct {
	RequestID      int32  // request_id=18
	ReceiverName   string // receiver_name=1
	ReceiverAvatar string // receiver_avatar=2
	Name           string // name=3
	Avatar         string // avatar=4
	GroupName      string // group_name=5
	GroupAvatar    string // group_avatar=6
	InviterID      string // inviter_id=7
	SourceType     int32  // source_type=9
	TargetType     int32  // target_type=10
	TargetID       string // target_id=11
	ReceiverID     string // receiver_id=12
	Result         int32  // result=13
	ProcessedAt    int64  // processed_at=14
	InviteAt       int64  // invite_at=16
	InviteAtStr    string // invite_at_str=17
	BotName        string // bot_name=19
	BotAvatar      string // bot_avatar=20
	ProcessorName  string // processor_name=22
	Note           string // note=23
	IsVIP          bool   // is_vip=24
}

// FriendRequests is the decoded /v1/friend/request-list response.
type FriendRequests struct {
	Requests []InviteRequest
	Total    int32 // total=3, 总请求数
	Pending  int32 // pending=4, 待处理请求数
}

// AddressBook fetches the address book (users, joined groups and bots).
//
// Endpoint: POST /v1/friend/address-book-list (protobuf request/response).
// The request always sends AddressBookListRequest with an empty md5 (the md5
// cache only saves bandwidth and is not needed by the TUI). The returned
// groups keep the server order; a group may have no items.
func (c *Client) AddressBook(ctx context.Context, token string) ([]ContactGroup, error) {
	body := encodeAddressBookListRequest("")
	raw, err := c.postProto(ctx, "/v1/friend/address-book-list", token, body)
	if err != nil {
		return nil, err
	}
	status, groups, err := decodeAddressBookListResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析通讯录失败: %w", err)
	}
	if status.Code != 1 {
		return nil, &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return groups, nil
}

// FriendRequests fetches the friend/invite request list.
//
// Endpoint: POST /v1/friend/request-list (protobuf response; the request
// message has no fields, so an empty body is sent).
func (c *Client) FriendRequests(ctx context.Context, token string) (*FriendRequests, error) {
	raw, err := c.postProto(ctx, "/v1/friend/request-list", token, nil)
	if err != nil {
		return nil, err
	}
	status, requests, err := decodeRequestListResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析申请列表失败: %w", err)
	}
	if status.Code != 1 {
		return nil, &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return requests, nil
}
