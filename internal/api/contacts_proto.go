package api

// Hand-written protobuf codec for the address book (通讯录) endpoints.
//
// The endpoints below answer with protobuf when the request carries a token
// header (and with a JSON envelope for envelope-level failures such as a
// missing/expired token), so the wire messages are encoded/decoded directly on
// top of google.golang.org/protobuf/encoding/protowire, mirroring the project's
// existing approach for /v1/user/info and the chat APIs.
//
// Wire schema (../yhchatAPI/src/full.proto, regions AddressBookListRequest,
// AddressBookListResponse and RequestListResponse):
//
//	message AddressBookListRequest {
//	  string md5 = 2; // 上一次通讯录的 MD5
//	}
//	message AddressBookListResponse {
//	  Status status = 1;
//	  repeated Data data = 2;
//	  string md5 = 3;
//	  message Data {
//	    string list_name = 1;     // "用户"/"我加入的群聊"/"机器人"
//	    repeated DataList data = 2;
//	    ChatType chat_type = 3;
//	    message DataList {
//	      string chat_id = 1;
//	      string remark = 2;
//	      string avatar_url = 3;
//	      int32 permisson_level = 4; // 群主 100, 管理员 2
//	      bool no_disturb = 5;
//	      string name = 8;
//	    }
//	  }
//	}
//	message RequestListResponse {
//	  Status status = 1;
//	  message Request { ... fields 1-7, 9-14, 16-20, 22-24 ... }
//	  repeated Request requests = 2;
//	  int32 total = 3;
//	  int32 pending = 4;
//	}
//
// The decoders are forward compatible in the same way proto.Unmarshal is:
// unknown fields (including known field numbers arriving with an unexpected
// wire type) are skipped. Truncated or otherwise malformed input is reported
// as an error and never panics. An empty body is rejected because an empty
// HTTP 200 body is never a usable response for these endpoints.

import (
	"errors"
	"fmt"
)

// errDecodeContacts is wrapped by every address book / request list decoding
// failure, so callers can test for a malformed body with errors.Is.
var errDecodeContacts = errors.New("api: invalid contacts protobuf")

// AddressBookListRequest fields.
const (
	abRequestMD5Field = 2
)

// AddressBookListResponse fields.
const (
	abResStatusField = 1
	abResDataField   = 2
	abResMD5Field    = 3
)

// AddressBookListResponse.Data (ContactGroup) fields.
const (
	abGroupListNameField = 1
	abGroupItemsField    = 2
	abGroupChatTypeField = 3
)

// AddressBookListResponse.Data.DataList (Contact) fields.
const (
	abItemChatIDField          = 1
	abItemRemarkField          = 2
	abItemAvatarURLField       = 3
	abItemPermissionLevelField = 4
	abItemNoDisturbField       = 5
	abItemNameField            = 8
)

// RequestListResponse fields.
const (
	rlStatusField   = 1
	rlRequestsField = 2
	rlTotalField    = 3
	rlPendingField  = 4
)

// RequestListResponse.Request fields (field 8 duplicates inviter_id and 15/21
// are unknown, so those are skipped as unknown fields).
const (
	rlReceiverNameField   = 1
	rlReceiverAvatarField = 2
	rlNameField           = 3
	rlAvatarField         = 4
	rlGroupNameField      = 5
	rlGroupAvatarField    = 6
	rlInviterIDField      = 7
	rlSourceTypeField     = 9
	rlTargetTypeField     = 10
	rlTargetIDField       = 11
	rlReceiverIDField     = 12
	rlResultField         = 13
	rlProcessedAtField    = 14
	rlInviteAtField       = 16
	rlInviteAtStrField    = 17
	rlRequestIDField      = 18
	rlBotNameField        = 19
	rlBotAvatarField      = 20
	rlProcessorNameField  = 22
	rlNoteField           = 23
	rlIsVIPField          = 24
)

// ---------------------------------------------------------------------------
// request encoders

// encodeAddressBookListRequest encodes AddressBookListRequest{md5=2}. The md5
// field is always emitted (even when empty), matching what the official
// clients send for the first page request.
func encodeAddressBookListRequest(md5 string) []byte {
	return pbAppendString(nil, abRequestMD5Field, md5)
}

// ---------------------------------------------------------------------------
// response decoders

// decodeAddressBookListResponse decodes AddressBookListResponse and returns the
// status plus the groups in server order (groups without items are kept).
func decodeAddressBookListResponse(b []byte) (Status, []ContactGroup, error) {
	var (
		status Status
		groups []ContactGroup
	)
	if len(b) == 0 {
		return status, nil, fmt.Errorf("%w: empty body", errDecodeContacts)
	}
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, nil, fmt.Errorf("%w: %w", errDecodeContacts, err)
		}
		b = next
		switch num {
		case abResStatusField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: status: %w", errDecodeContacts, err)
			}
			b = rest
			if !ok {
				continue
			}
			decoded, decodeErr := decodeStatus(value)
			if decodeErr != nil {
				return status, nil, fmt.Errorf("%w: status: %w", errDecodeContacts, decodeErr)
			}
			status = decoded
		case abResDataField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: data: %w", errDecodeContacts, err)
			}
			b = rest
			if !ok {
				continue
			}
			group, decodeErr := decodeContactGroup(value)
			if decodeErr != nil {
				return status, nil, fmt.Errorf("%w: data: %w", errDecodeContacts, decodeErr)
			}
			groups = append(groups, group)
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: %w", errDecodeContacts, err)
			}
		}
	}
	return status, groups, nil
}

// decodeContactGroup decodes AddressBookListResponse.Data.
func decodeContactGroup(b []byte) (ContactGroup, error) {
	var group ContactGroup
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return group, err
		}
		b = next
		switch num {
		case abGroupListNameField:
			if err := decodeStringField(num, typ, &b, &group.Name); err != nil {
				return group, fmt.Errorf("list_name: %w", err)
			}
		case abGroupItemsField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return group, fmt.Errorf("data: %w", err)
			}
			b = rest
			if !ok {
				continue
			}
			contact, decodeErr := decodeContact(value)
			if decodeErr != nil {
				return group, fmt.Errorf("data: %w", decodeErr)
			}
			group.Items = append(group.Items, contact)
		case abGroupChatTypeField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return group, fmt.Errorf("chat_type: %w", err)
			}
			b = rest
			if ok {
				group.ChatType = ChatType(int32(value))
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return group, err
			}
		}
	}
	return group, nil
}

// decodeContact decodes AddressBookListResponse.Data.DataList.
func decodeContact(b []byte) (Contact, error) {
	var contact Contact
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return contact, err
		}
		b = next
		switch num {
		case abItemChatIDField:
			if err := decodeStringField(num, typ, &b, &contact.ID); err != nil {
				return contact, fmt.Errorf("chat_id: %w", err)
			}
		case abItemRemarkField:
			if err := decodeStringField(num, typ, &b, &contact.Remark); err != nil {
				return contact, fmt.Errorf("remark: %w", err)
			}
		case abItemAvatarURLField:
			if err := decodeStringField(num, typ, &b, &contact.AvatarURL); err != nil {
				return contact, fmt.Errorf("avatar_url: %w", err)
			}
		case abItemPermissionLevelField:
			if err := decodeInt32Field(num, typ, &b, &contact.PermissionLevel); err != nil {
				return contact, fmt.Errorf("permisson_level: %w", err)
			}
		case abItemNoDisturbField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return contact, fmt.Errorf("no_disturb: %w", err)
			}
			b = rest
			if ok {
				contact.NoDisturb = value != 0
			}
		case abItemNameField:
			if err := decodeStringField(num, typ, &b, &contact.Name); err != nil {
				return contact, fmt.Errorf("name: %w", err)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return contact, err
			}
		}
	}
	return contact, nil
}

// decodeRequestListResponse decodes RequestListResponse. The returned
// FriendRequests value is always non-nil on success.
func decodeRequestListResponse(b []byte) (Status, *FriendRequests, error) {
	var status Status
	requests := &FriendRequests{}
	if len(b) == 0 {
		return status, nil, fmt.Errorf("%w: empty body", errDecodeContacts)
	}
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, nil, fmt.Errorf("%w: %w", errDecodeContacts, err)
		}
		b = next
		switch num {
		case rlStatusField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: status: %w", errDecodeContacts, err)
			}
			b = rest
			if !ok {
				continue
			}
			decoded, decodeErr := decodeStatus(value)
			if decodeErr != nil {
				return status, nil, fmt.Errorf("%w: status: %w", errDecodeContacts, decodeErr)
			}
			status = decoded
		case rlRequestsField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: requests: %w", errDecodeContacts, err)
			}
			b = rest
			if !ok {
				continue
			}
			request, decodeErr := decodeInviteRequest(value)
			if decodeErr != nil {
				return status, nil, fmt.Errorf("%w: requests: %w", errDecodeContacts, decodeErr)
			}
			requests.Requests = append(requests.Requests, request)
		case rlTotalField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: total: %w", errDecodeContacts, err)
			}
			b = rest
			if ok {
				requests.Total = int32(value)
			}
		case rlPendingField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: pending: %w", errDecodeContacts, err)
			}
			b = rest
			if ok {
				requests.Pending = int32(value)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("%w: %w", errDecodeContacts, err)
			}
		}
	}
	return status, requests, nil
}

// decodeInviteRequest decodes RequestListResponse.Request.
func decodeInviteRequest(b []byte) (InviteRequest, error) {
	var request InviteRequest
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return request, err
		}
		b = next
		switch num {
		case rlReceiverNameField:
			if err := decodeStringField(num, typ, &b, &request.ReceiverName); err != nil {
				return request, fmt.Errorf("receiver_name: %w", err)
			}
		case rlReceiverAvatarField:
			if err := decodeStringField(num, typ, &b, &request.ReceiverAvatar); err != nil {
				return request, fmt.Errorf("receiver_avatar: %w", err)
			}
		case rlNameField:
			if err := decodeStringField(num, typ, &b, &request.Name); err != nil {
				return request, fmt.Errorf("name: %w", err)
			}
		case rlAvatarField:
			if err := decodeStringField(num, typ, &b, &request.Avatar); err != nil {
				return request, fmt.Errorf("avatar: %w", err)
			}
		case rlGroupNameField:
			if err := decodeStringField(num, typ, &b, &request.GroupName); err != nil {
				return request, fmt.Errorf("group_name: %w", err)
			}
		case rlGroupAvatarField:
			if err := decodeStringField(num, typ, &b, &request.GroupAvatar); err != nil {
				return request, fmt.Errorf("group_avatar: %w", err)
			}
		case rlInviterIDField:
			if err := decodeStringField(num, typ, &b, &request.InviterID); err != nil {
				return request, fmt.Errorf("inviter_id: %w", err)
			}
		case rlSourceTypeField:
			if err := decodeInt32Field(num, typ, &b, &request.SourceType); err != nil {
				return request, fmt.Errorf("source_type: %w", err)
			}
		case rlTargetTypeField:
			if err := decodeInt32Field(num, typ, &b, &request.TargetType); err != nil {
				return request, fmt.Errorf("target_type: %w", err)
			}
		case rlTargetIDField:
			if err := decodeStringField(num, typ, &b, &request.TargetID); err != nil {
				return request, fmt.Errorf("target_id: %w", err)
			}
		case rlReceiverIDField:
			if err := decodeStringField(num, typ, &b, &request.ReceiverID); err != nil {
				return request, fmt.Errorf("receiver_id: %w", err)
			}
		case rlResultField:
			if err := decodeInt32Field(num, typ, &b, &request.Result); err != nil {
				return request, fmt.Errorf("result: %w", err)
			}
		case rlProcessedAtField:
			if err := decodeInt64Field(num, typ, &b, &request.ProcessedAt); err != nil {
				return request, fmt.Errorf("processed_at: %w", err)
			}
		case rlInviteAtField:
			if err := decodeInt64Field(num, typ, &b, &request.InviteAt); err != nil {
				return request, fmt.Errorf("invite_at: %w", err)
			}
		case rlInviteAtStrField:
			if err := decodeStringField(num, typ, &b, &request.InviteAtStr); err != nil {
				return request, fmt.Errorf("invite_at_str: %w", err)
			}
		case rlRequestIDField:
			if err := decodeInt32Field(num, typ, &b, &request.RequestID); err != nil {
				return request, fmt.Errorf("request_id: %w", err)
			}
		case rlBotNameField:
			if err := decodeStringField(num, typ, &b, &request.BotName); err != nil {
				return request, fmt.Errorf("bot_name: %w", err)
			}
		case rlBotAvatarField:
			if err := decodeStringField(num, typ, &b, &request.BotAvatar); err != nil {
				return request, fmt.Errorf("bot_avatar: %w", err)
			}
		case rlProcessorNameField:
			if err := decodeStringField(num, typ, &b, &request.ProcessorName); err != nil {
				return request, fmt.Errorf("processor_name: %w", err)
			}
		case rlNoteField:
			if err := decodeStringField(num, typ, &b, &request.Note); err != nil {
				return request, fmt.Errorf("note: %w", err)
			}
		case rlIsVIPField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return request, fmt.Errorf("is_vip: %w", err)
			}
			b = rest
			if ok {
				request.IsVIP = value != 0
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return request, err
			}
		}
	}
	return request, nil
}
