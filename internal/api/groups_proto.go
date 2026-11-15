package api

// Hand-written protobuf codec for the group (群资料 / 群成员) endpoints.
//
// POST /v1/group/info and POST /v1/group/list-member answer with protobuf when
// the request carries a token header (and with a JSON envelope for
// envelope-level failures such as a missing or expired token), so the wire
// messages are encoded and decoded directly on top of
// google.golang.org/protobuf/encoding/protowire, mirroring the project's
// approach for the user info, chat and address book APIs.
//
// Wire schema (../yhchatAPI/src/full.proto, regions GroupInfoRequest,
// GroupInfoResponse, GroupData, ListMemberRequest and ListMemberResponse):
//
//	message GroupInfoRequest {
//	  string group_id = 2; // 群聊 ID
//	}
//	message GroupInfoResponse {
//	  Status status = 1;
//	  GroupData data = 2;
//	  repeated BotData bot = 3; // 群机器人列表，TUI 暂不使用，按未知字段跳过
//	}
//	message GroupData {
//	  string group_id = 1;
//	  string name = 2;
//	  string avatar_url = 3;
//	  uint64 avatar_id = 4;
//	  string introduction = 5;
//	  uint64 headcount = 6;
//	  string create_by = 7;
//	  bool direct_join = 8;
//	  int32 permisson_level = 9;
//	  bool history_msg = 10;
//	  string category_name = 11;
//	  uint64 category_id = 12;
//	  bool private = 13;
//	  bool do_not_disturb = 14;
//	  uint64 community_id = 15;
//	  string community_name = 16;
//	  int64 ban_id = 17;
//	  int64 ban_until_timestamp = 18;
//	  bool top = 19;
//	  repeated string admin = 20;
//	  int64 gag_until_timestamp = 21;
//	  string limited_msg_type = 22;
//	  string owner = 23;
//	  bool recommandation = 24; // 注意 proto 中的拼写
//	  bool is_gag = 25;
//	  repeated string tag_old = 26; // 旧版标签，跳过
//	  repeated Tag tag = 27;        // 标签，跳过
//	  string my_group_nickname = 28;
//	  string group_code = 29;
//	  bool hide_group_members = 30;
//	  int64 field31 = 31; // 未知，跳过
//	  bool auto_delete_message = 32;                  // 跳过
//	  bool deny_members_upload_to_group_disk = 33;    // 跳过
//	  string ban_reason = 34;
//	}
//	message ListMemberRequest {
//	  Data data = 2;
//	  message Data {
//	    int32 size = 1; // 分页大小
//	    int32 page = 2; // 页数
//	  }
//	  string group_id = 3;
//	  string keywords = 4;
//	}
//	message ListMemberResponse {
//	  Status status = 1;
//	  repeated User user = 2;
//	  message User {
//	    string group_id = 1;
//	    UserInfo user_info = 2;
//	    message UserInfo {
//	      string user_id = 1;
//	      string name = 2;
//	      string avatar_url = 4;
//	      bool is_vip = 6;
//	    }
//	    int32 permission_level = 3; // 群主 100, 管理员 2
//	    int64 gag_timestamp = 4;    // 禁言时间戳
//	    bool is_gag = 5;            // 是否被禁言
//	  }
//	  int32 total = 3;
//	}
//
// The decoders are forward compatible in the same way proto.Unmarshal is:
// unknown fields (including known field numbers arriving with an unexpected
// wire type) are skipped. Truncated or otherwise malformed input is reported as
// an error and never panics. An empty body is rejected because an empty HTTP
// 200 body is never a usable response for these endpoints.

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// errDecodeGroups is wrapped by every group protobuf decoding failure, so
// callers can test for a malformed body with errors.Is.
var errDecodeGroups = errors.New("api: invalid group protobuf")

// GroupInfoRequest field numbers.
const (
	groupInfoRequestGroupIDField = 2
)

// GroupInfoResponse field numbers. Field 3 (repeated BotData) is not needed by
// GroupInfo and is skipped as an unknown field.
const (
	groupInfoStatusField = 1
	groupInfoDataField   = 2
)

// GroupData (GroupInfoResponse.data) field numbers.
const (
	groupDataGroupIDField           = 1
	groupDataNameField              = 2
	groupDataAvatarURLField         = 3
	groupDataAvatarIDField          = 4
	groupDataIntroductionField      = 5
	groupDataHeadcountField         = 6
	groupDataCreateByField          = 7
	groupDataDirectJoinField        = 8
	groupDataPermissionLevelField   = 9 // proto: permisson_level
	groupDataHistoryMsgField        = 10
	groupDataCategoryNameField      = 11
	groupDataCategoryIDField        = 12
	groupDataPrivateField           = 13
	groupDataDoNotDisturbField      = 14
	groupDataCommunityIDField       = 15
	groupDataCommunityNameField     = 16
	groupDataBanIDField             = 17
	groupDataBanUntilTimestampField = 18
	groupDataTopField               = 19
	groupDataAdminField             = 20
	groupDataGagUntilTimestampField = 21
	groupDataLimitedMsgTypeField    = 22
	groupDataOwnerField             = 23
	groupDataRecommendationField    = 24 // proto: recommandation
	groupDataIsGagField             = 25
	groupDataMyGroupNicknameField   = 28
	groupDataGroupCodeField         = 29
	groupDataHideGroupMembersField  = 30
	groupDataBanReasonField         = 34
)

// ListMemberRequest field numbers (including the nested Data message).
const (
	listMemberRequestDataField     = 2
	listMemberRequestSizeField     = 1
	listMemberRequestPageField     = 2
	listMemberRequestGroupIDField  = 3
	listMemberRequestKeywordsField = 4
)

// ListMemberResponse field numbers.
const (
	listMemberResponseStatusField = 1
	listMemberResponseUserField   = 2
	listMemberResponseTotalField  = 3
)

// ListMemberResponse.User field numbers.
const (
	listMemberGroupIDField         = 1
	listMemberUserInfoField        = 2
	listMemberPermissionLevelField = 3
	listMemberGagTimestampField    = 4
	listMemberIsGagField           = 5
)

// ListMemberResponse.User.UserInfo field numbers.
const (
	listMemberInfoUserIDField    = 1
	listMemberInfoNameField      = 2
	listMemberInfoAvatarURLField = 4
	listMemberInfoIsVIPField     = 6
)

// ---------------------------------------------------------------------------
// request encoders

// encodeGroupInfoRequest encodes GroupInfoRequest{group_id=2}.
func encodeGroupInfoRequest(groupID string) []byte {
	return pbAppendString(nil, groupInfoRequestGroupIDField, groupID)
}

// encodeListMemberRequest encodes ListMemberRequest{data={size=1,page=2},
// group_id=3, keywords=4}. The nested data message is always emitted because
// the caller normalises both values to at least 1; keywords is only emitted
// when non-empty, which is what a proto3 encoder does for default values.
func encodeListMemberRequest(groupID string, page, size int32, keywords string) []byte {
	data := pbAppendVarint(nil, listMemberRequestSizeField, uint64(int32ToUint32(size)))
	data = pbAppendVarint(data, listMemberRequestPageField, uint64(int32ToUint32(page)))

	var b []byte
	b = pbAppendBytes(b, listMemberRequestDataField, data)
	b = pbAppendString(b, listMemberRequestGroupIDField, groupID)
	if keywords != "" {
		b = pbAppendString(b, listMemberRequestKeywordsField, keywords)
	}
	return b
}

// ---------------------------------------------------------------------------
// response decoders

// decodeGroupInfoResponse decodes GroupInfoResponse and returns the status plus
// the group profile. The returned *GroupInfo is nil when the response carries no
// data field (for example an error response with a non-1 status code).
func decodeGroupInfoResponse(b []byte) (Status, *GroupInfo, error) {
	var (
		status Status
		info   *GroupInfo
	)
	if len(b) == 0 {
		return status, nil, fmt.Errorf("%w: empty body", errDecodeGroups)
	}
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, nil, fmt.Errorf("%w: %w", errDecodeGroups, err)
		}
		b = next
		switch num {
		case groupInfoStatusField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: status: %w", errDecodeGroups, err)
			}
			b = rest
			if !ok {
				continue
			}
			decoded, decodeErr := decodeStatus(value)
			if decodeErr != nil {
				return status, nil, fmt.Errorf("%w: status: %w", errDecodeGroups, decodeErr)
			}
			status = decoded
		case groupInfoDataField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: data: %w", errDecodeGroups, err)
			}
			b = rest
			if !ok {
				continue
			}
			decoded, decodeErr := decodeGroupData(value)
			if decodeErr != nil {
				return status, nil, fmt.Errorf("%w: data: %w", errDecodeGroups, decodeErr)
			}
			info = &decoded
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: %w", errDecodeGroups, err)
			}
		}
	}
	return status, info, nil
}

// decodeGroupData decodes GroupData (GroupInfoResponse.data).
func decodeGroupData(b []byte) (GroupInfo, error) {
	var info GroupInfo
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return info, err
		}
		b = next
		switch num {
		case groupDataGroupIDField:
			if err := decodeStringField(num, typ, &b, &info.GroupID); err != nil {
				return info, fmt.Errorf("group_id: %w", err)
			}
		case groupDataNameField:
			if err := decodeStringField(num, typ, &b, &info.Name); err != nil {
				return info, fmt.Errorf("name: %w", err)
			}
		case groupDataAvatarURLField:
			if err := decodeStringField(num, typ, &b, &info.AvatarURL); err != nil {
				return info, fmt.Errorf("avatar_url: %w", err)
			}
		case groupDataAvatarIDField:
			if err := decodeGroupUint64Field(num, typ, &b, &info.AvatarID); err != nil {
				return info, fmt.Errorf("avatar_id: %w", err)
			}
		case groupDataIntroductionField:
			if err := decodeStringField(num, typ, &b, &info.Introduction); err != nil {
				return info, fmt.Errorf("introduction: %w", err)
			}
		case groupDataHeadcountField:
			if err := decodeGroupUint64Field(num, typ, &b, &info.Headcount); err != nil {
				return info, fmt.Errorf("headcount: %w", err)
			}
		case groupDataCreateByField:
			if err := decodeStringField(num, typ, &b, &info.CreateBy); err != nil {
				return info, fmt.Errorf("create_by: %w", err)
			}
		case groupDataDirectJoinField:
			if err := decodeGroupBoolField(num, typ, &b, &info.DirectJoin); err != nil {
				return info, fmt.Errorf("direct_join: %w", err)
			}
		case groupDataPermissionLevelField:
			if err := decodeInt32Field(num, typ, &b, &info.PermissionLevel); err != nil {
				return info, fmt.Errorf("permisson_level: %w", err)
			}
		case groupDataHistoryMsgField:
			if err := decodeGroupBoolField(num, typ, &b, &info.HistoryMsg); err != nil {
				return info, fmt.Errorf("history_msg: %w", err)
			}
		case groupDataCategoryNameField:
			if err := decodeStringField(num, typ, &b, &info.CategoryName); err != nil {
				return info, fmt.Errorf("category_name: %w", err)
			}
		case groupDataCategoryIDField:
			if err := decodeGroupUint64Field(num, typ, &b, &info.CategoryID); err != nil {
				return info, fmt.Errorf("category_id: %w", err)
			}
		case groupDataPrivateField:
			if err := decodeGroupBoolField(num, typ, &b, &info.Private); err != nil {
				return info, fmt.Errorf("private: %w", err)
			}
		case groupDataDoNotDisturbField:
			if err := decodeGroupBoolField(num, typ, &b, &info.DoNotDisturb); err != nil {
				return info, fmt.Errorf("do_not_disturb: %w", err)
			}
		case groupDataCommunityIDField:
			if err := decodeGroupUint64Field(num, typ, &b, &info.CommunityID); err != nil {
				return info, fmt.Errorf("community_id: %w", err)
			}
		case groupDataCommunityNameField:
			if err := decodeStringField(num, typ, &b, &info.CommunityName); err != nil {
				return info, fmt.Errorf("community_name: %w", err)
			}
		case groupDataBanIDField:
			if err := decodeInt64Field(num, typ, &b, &info.BanID); err != nil {
				return info, fmt.Errorf("ban_id: %w", err)
			}
		case groupDataBanUntilTimestampField:
			if err := decodeInt64Field(num, typ, &b, &info.BanUntilTimestamp); err != nil {
				return info, fmt.Errorf("ban_until_timestamp: %w", err)
			}
		case groupDataTopField:
			if err := decodeGroupBoolField(num, typ, &b, &info.Top); err != nil {
				return info, fmt.Errorf("top: %w", err)
			}
		case groupDataAdminField:
			// admin is the 管理员列表 (repeated string).
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return info, fmt.Errorf("admin: %w", err)
			}
			b = rest
			if ok {
				info.AdminIDs = append(info.AdminIDs, string(value))
			}
		case groupDataGagUntilTimestampField:
			if err := decodeInt64Field(num, typ, &b, &info.GagUntilTimestamp); err != nil {
				return info, fmt.Errorf("gag_until_timestamp: %w", err)
			}
		case groupDataLimitedMsgTypeField:
			if err := decodeStringField(num, typ, &b, &info.LimitedMsgType); err != nil {
				return info, fmt.Errorf("limited_msg_type: %w", err)
			}
		case groupDataOwnerField:
			if err := decodeStringField(num, typ, &b, &info.Owner); err != nil {
				return info, fmt.Errorf("owner: %w", err)
			}
		case groupDataRecommendationField:
			if err := decodeGroupBoolField(num, typ, &b, &info.Recommendation); err != nil {
				return info, fmt.Errorf("recommandation: %w", err)
			}
		case groupDataIsGagField:
			if err := decodeGroupBoolField(num, typ, &b, &info.IsGag); err != nil {
				return info, fmt.Errorf("is_gag: %w", err)
			}
		case groupDataMyGroupNicknameField:
			if err := decodeStringField(num, typ, &b, &info.MyGroupNickname); err != nil {
				return info, fmt.Errorf("my_group_nickname: %w", err)
			}
		case groupDataGroupCodeField:
			if err := decodeStringField(num, typ, &b, &info.GroupCode); err != nil {
				return info, fmt.Errorf("group_code: %w", err)
			}
		case groupDataHideGroupMembersField:
			if err := decodeGroupBoolField(num, typ, &b, &info.HideGroupMembers); err != nil {
				return info, fmt.Errorf("hide_group_members: %w", err)
			}
		case groupDataBanReasonField:
			if err := decodeStringField(num, typ, &b, &info.BanReason); err != nil {
				return info, fmt.Errorf("ban_reason: %w", err)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return info, err
			}
		}
	}
	return info, nil
}

// decodeListMemberResponse decodes ListMemberResponse and returns the status,
// the members in server order and the total member count.
func decodeListMemberResponse(b []byte) (Status, []GroupMember, int, error) {
	var (
		status  Status
		members []GroupMember
		total   int
	)
	if len(b) == 0 {
		return status, nil, 0, fmt.Errorf("%w: empty body", errDecodeGroups)
	}
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, nil, 0, fmt.Errorf("%w: %w", errDecodeGroups, err)
		}
		b = next
		switch num {
		case listMemberResponseStatusField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, 0, fmt.Errorf("%w: status: %w", errDecodeGroups, err)
			}
			b = rest
			if !ok {
				continue
			}
			decoded, decodeErr := decodeStatus(value)
			if decodeErr != nil {
				return status, nil, 0, fmt.Errorf("%w: status: %w", errDecodeGroups, decodeErr)
			}
			status = decoded
		case listMemberResponseUserField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, 0, fmt.Errorf("%w: user: %w", errDecodeGroups, err)
			}
			b = rest
			if !ok {
				continue
			}
			member, decodeErr := decodeGroupMember(value)
			if decodeErr != nil {
				return status, nil, 0, fmt.Errorf("%w: user: %w", errDecodeGroups, decodeErr)
			}
			members = append(members, member)
		case listMemberResponseTotalField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return status, nil, 0, fmt.Errorf("%w: total: %w", errDecodeGroups, err)
			}
			b = rest
			if ok {
				total = int(int32(value))
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return status, nil, 0, fmt.Errorf("%w: %w", errDecodeGroups, err)
			}
		}
	}
	return status, members, total, nil
}

// decodeGroupMember decodes ListMemberResponse.User.
func decodeGroupMember(b []byte) (GroupMember, error) {
	var member GroupMember
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return member, err
		}
		b = next
		switch num {
		case listMemberGroupIDField:
			if err := decodeStringField(num, typ, &b, &member.GroupID); err != nil {
				return member, fmt.Errorf("group_id: %w", err)
			}
		case listMemberUserInfoField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return member, fmt.Errorf("user_info: %w", err)
			}
			b = rest
			if !ok {
				continue
			}
			if err := decodeGroupMemberUserInfo(value, &member); err != nil {
				return member, fmt.Errorf("user_info: %w", err)
			}
		case listMemberPermissionLevelField:
			if err := decodeInt32Field(num, typ, &b, &member.PermissionLevel); err != nil {
				return member, fmt.Errorf("permission_level: %w", err)
			}
		case listMemberGagTimestampField:
			if err := decodeInt64Field(num, typ, &b, &member.GagTimestamp); err != nil {
				return member, fmt.Errorf("gag_timestamp: %w", err)
			}
		case listMemberIsGagField:
			if err := decodeGroupBoolField(num, typ, &b, &member.IsGag); err != nil {
				return member, fmt.Errorf("is_gag: %w", err)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return member, err
			}
		}
	}
	return member, nil
}

// decodeGroupMemberUserInfo decodes ListMemberResponse.User.UserInfo into member.
func decodeGroupMemberUserInfo(b []byte, member *GroupMember) error {
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return err
		}
		b = next
		switch num {
		case listMemberInfoUserIDField:
			if err := decodeStringField(num, typ, &b, &member.UserID); err != nil {
				return fmt.Errorf("user_id: %w", err)
			}
		case listMemberInfoNameField:
			if err := decodeStringField(num, typ, &b, &member.Name); err != nil {
				return fmt.Errorf("name: %w", err)
			}
		case listMemberInfoAvatarURLField:
			if err := decodeStringField(num, typ, &b, &member.AvatarURL); err != nil {
				return fmt.Errorf("avatar_url: %w", err)
			}
		case listMemberInfoIsVIPField:
			if err := decodeGroupBoolField(num, typ, &b, &member.IsVIP); err != nil {
				return fmt.Errorf("is_vip: %w", err)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// decodeGroupUint64Field decodes one uint64 field into dst. A field with an
// unexpected wire type is skipped, leaving dst untouched.
func decodeGroupUint64Field(num protowire.Number, typ protowire.Type, b *[]byte, dst *uint64) error {
	v, next, ok, err := consumeVarintField(num, typ, *b)
	if err != nil {
		return err
	}
	*b = next
	if ok {
		*dst = v
	}
	return nil
}

// decodeGroupBoolField decodes one bool field into dst. A field with an
// unexpected wire type is skipped, leaving dst untouched.
func decodeGroupBoolField(num protowire.Number, typ protowire.Type, b *[]byte, dst *bool) error {
	v, next, ok, err := consumeVarintField(num, typ, *b)
	if err != nil {
		return err
	}
	*b = next
	if ok {
		*dst = v != 0
	}
	return nil
}
