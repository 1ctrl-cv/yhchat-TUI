package api

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// Protobuf field numbers for the chat requests/responses used by the TUI.
const (
	convListStatusField = 1
	convListDataField   = 2
	convListMD5Field    = 4

	convChatIDField    = 1
	convChatTypeField  = 2
	convRemarkField    = 3
	convContentField   = 4
	convTimestampField = 5
	convUnreadField    = 6
	convAtField        = 7
	convAvatarIDField  = 8
	convAvatarURLField = 9
	convDNDField       = 11
	convSendTimeField  = 12
	convVIPField       = 13
	convNameField      = 15
	convCertLevelField = 16

	msgListStatusField = 1
	msgListDataField   = 2

	msgIDField        = 1
	msgSenderField    = 2
	msgDirectionField = 3
	msgContentTypeFld = 4
	msgContentField   = 5
	msgTimestampFld   = 6
	msgRecallField    = 8
	msgQuoteField     = 9
	msgSeqField       = 10
	msgEditTimeFld    = 12

	senderChatIDField   = 1
	senderChatTypeField = 2
	senderNameField     = 3
	senderAvatarField   = 4
	senderVIPField      = 5

	contentTextField      = 1
	contentImageURLField  = 3
	contentFileNameField  = 4
	contentFileURLField   = 5
	contentFormField      = 7
	contentQuoteTextField = 8
	contentImageField     = 9
	contentPostIDField    = 10
	contentPostTitleFld   = 11
	contentPostContentFld = 12
	contentPostTypeFld    = 13
	contentFileSizeField  = 18
	contentVideoURLField  = 19
	contentVideoTimeFld   = 20
	contentAudioURLField  = 21
	contentAudioTimeFld   = 22
	contentRoomNameFld    = 29
	contentCallStatusFld  = 32
	contentWidthFld       = 33
	contentHeightFld      = 34
	contentStickerItemFld = 25
	contentStickerPackFld = 26
	contentTipField       = 37
	contentMentionedField = 6
)

// ---------------------------------------------------------------------------
// request encoders

// appendStringField appends a wire-type-2 (length-delimited) string field.
func pbAppendString(b []byte, field protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

// appendBytesField appends a wire-type-2 nested message field.
func pbAppendBytes(b []byte, field protowire.Number, value []byte) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendBytes(b, value)
}

// appendVarintField appends a wire-type-0 varint field.
func pbAppendVarint(b []byte, field protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

// encodeConversationListRequest encodes ConversationListRequest{md5=2}.
// encodeConversationListRequest encodes ConversationListRequest{md5=2}.
// The real server treats a present md5 field (even an empty one) as an
// incremental sync request and returns an empty list, so an empty md5 means
// "full sync" and the whole body is omitted.
func encodeConversationListRequest(md5 string) []byte {
	if md5 == "" {
		return nil
	}
	return pbAppendString(nil, 2, md5)
}

// encodeRecallMsgRequest encodes RecallMsg{msg_id=2 repeated, chat_id=3,
// chat_type=4}.
func encodeRecallMsgRequest(chatID string, chatType ChatType, msgIDs []string) []byte {
	var b []byte
	for _, msgID := range msgIDs {
		b = pbAppendString(b, 2, msgID)
	}
	b = pbAppendString(b, 3, chatID)
	b = pbAppendVarint(b, 4, uint64(chatType))
	return b
}

// encodeListMsgRequest encodes ListMsgRequest{msg_count=2, msg_id=3,
// chat_type=4, chat_id=5}.
func encodeListMsgRequest(msgCount int32, msgID string, chatType ChatType, chatID string) []byte {
	var b []byte
	b = pbAppendVarint(b, 2, uint64(int32ToUint32(msgCount)))
	if msgID != "" {
		b = pbAppendString(b, 3, msgID)
	}
	b = pbAppendVarint(b, 4, uint64(chatType))
	b = pbAppendString(b, 5, chatID)
	return b
}

// encodeSendTextMessageRequest encodes SendMsgRequest for a plain text message:
// msg_id=2, chat_id=3, chat_type=4, content=5 (MsgContent{text=1}),
// content_type=6 (1 = text).
func encodeSendTextMessageRequest(msgID, chatID string, chatType ChatType, text string) []byte {
	content := pbAppendString(nil, contentTextField, text)
	var b []byte
	b = pbAppendString(b, 2, msgID)
	b = pbAppendString(b, 3, chatID)
	b = pbAppendVarint(b, 4, uint64(chatType))
	b = pbAppendBytes(b, 5, content)
	b = pbAppendVarint(b, 6, uint64(ContentTypeText))
	return b
}

// int32ToUint32 converts a signed proto int32 into varint bits.
func int32ToUint32(v int32) uint32 {
	return uint32(v)
}

// ---------------------------------------------------------------------------
// response decoders

// decodeStatusResponse decodes StatusResponse{status=1}.
func decodeStatusResponse(b []byte) (Status, error) {
	var status Status
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, err
		}
		b = next
		if num == 1 {
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, fmt.Errorf("status: %w", err)
			}
			b = rest
			if ok {
				decoded, decodeErr := decodeStatus(value)
				if decodeErr != nil {
					return status, fmt.Errorf("status: %w", decodeErr)
				}
				status = decoded
			}
			continue
		}
		b, err = skipField(num, typ, b)
		if err != nil {
			return status, err
		}
	}
	return status, nil
}

// decodeConversationListResponse decodes ConversationListResponse and returns
// the status, conversations and list md5.
func decodeConversationListResponse(b []byte) (Status, []Conversation, string, error) {
	var (
		status Status
		convs  []Conversation
		md5    string
	)
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, nil, "", err
		}
		b = next
		switch num {
		case convListStatusField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, "", fmt.Errorf("status: %w", err)
			}
			b = rest
			if ok {
				decoded, decodeErr := decodeStatus(value)
				if decodeErr != nil {
					return status, nil, "", fmt.Errorf("status: %w", decodeErr)
				}
				status = decoded
			}
		case convListDataField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, "", fmt.Errorf("data: %w", err)
			}
			b = rest
			if ok {
				conv, decodeErr := decodeConversation(value)
				if decodeErr != nil {
					return status, nil, "", fmt.Errorf("data: %w", decodeErr)
				}
				convs = append(convs, conv)
			}
		case convListMD5Field:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, "", fmt.Errorf("md5: %w", err)
			}
			b = rest
			if ok {
				md5 = string(value)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return status, nil, "", err
			}
		}
	}
	return status, convs, md5, nil
}

// decodeConversation decodes ConversationListResponse.Data.
func decodeConversation(b []byte) (Conversation, error) {
	var conv Conversation
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return conv, err
		}
		b = next
		switch num {
		case convChatIDField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("chat_id: %w", err)
			}
			b = rest
			if ok {
				conv.ChatID = string(value)
			}
		case convChatTypeField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("chat_type: %w", err)
			}
			b = rest
			if ok {
				conv.ChatType = ChatType(int32(value))
			}
		case convRemarkField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("remark: %w", err)
			}
			b = rest
			if ok {
				conv.Remark = string(value)
			}
		case convContentField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("chat_content: %w", err)
			}
			b = rest
			if ok {
				conv.ChatContent = string(value)
			}
		case convTimestampField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("timestamp_ms: %w", err)
			}
			b = rest
			if ok {
				conv.TimestampMs = value
			}
		case convUnreadField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("unread_msg: %w", err)
			}
			b = rest
			if ok {
				conv.Unread = int32(value)
			}
		case convAtField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("at: %w", err)
			}
			b = rest
			if ok {
				conv.At = value != 0
			}
		case convAvatarIDField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("avatar_id: %w", err)
			}
			b = rest
			if ok {
				conv.AvatarID = value
			}
		case convAvatarURLField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("avatar_url: %w", err)
			}
			b = rest
			if ok {
				conv.AvatarURL = string(value)
			}
		case convDNDField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("do_not_disturb: %w", err)
			}
			b = rest
			if ok {
				conv.DoNotDisturb = value != 0
			}
		case convSendTimeField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("send_timestamp: %w", err)
			}
			b = rest
			if ok {
				conv.SendTimestamp = value
			}
		case convVIPField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("is_vip: %w", err)
			}
			b = rest
			if ok {
				conv.IsVIP = value != 0
			}
		case convNameField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("name: %w", err)
			}
			b = rest
			if ok {
				conv.Name = string(value)
			}
		case convCertLevelField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return conv, fmt.Errorf("certification_level: %w", err)
			}
			b = rest
			if ok {
				conv.CertificationLevel = int32(value)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return conv, err
			}
		}
	}
	return conv, nil
}

// decodeListMsgResponse decodes ListMsgResponse and returns status + messages.
func decodeListMsgResponse(b []byte) (Status, []Message, error) {
	var (
		status Status
		msgs   []Message
	)
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, nil, err
		}
		b = next
		switch num {
		case msgListStatusField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("status: %w", err)
			}
			b = rest
			if ok {
				decoded, decodeErr := decodeStatus(value)
				if decodeErr != nil {
					return status, nil, fmt.Errorf("status: %w", decodeErr)
				}
				status = decoded
			}
		case msgListDataField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("data: %w", err)
			}
			b = rest
			if ok {
				msg, decodeErr := decodeMessage(value)
				if decodeErr != nil {
					return status, nil, fmt.Errorf("data: %w", decodeErr)
				}
				msgs = append(msgs, msg)
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return status, nil, err
			}
		}
	}
	return status, msgs, nil
}

// decodeMessage decodes ListMsgResponse.MsgData.
func decodeMessage(b []byte) (Message, error) {
	var msg Message
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return msg, err
		}
		b = next
		switch num {
		case msgIDField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("msg_id: %w", err)
			}
			b = rest
			if ok {
				msg.MsgID = string(value)
			}
		case msgSenderField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("sender: %w", err)
			}
			b = rest
			if ok {
				sender, decodeErr := decodeSender(value)
				if decodeErr != nil {
					return msg, fmt.Errorf("sender: %w", decodeErr)
				}
				msg.Sender = sender
			}
		case msgDirectionField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("direction: %w", err)
			}
			b = rest
			if ok {
				msg.Direction = string(value)
			}
		case msgContentTypeFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("content_type: %w", err)
			}
			b = rest
			if ok {
				msg.ContentType = int32(value)
			}
		case msgContentField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("content: %w", err)
			}
			b = rest
			if ok {
				content, decodeErr := decodeMessageContent(value)
				if decodeErr != nil {
					return msg, fmt.Errorf("content: %w", decodeErr)
				}
				msg.Content = content
			}
		case msgTimestampFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("send_timestamp_ms: %w", err)
			}
			b = rest
			if ok {
				msg.TimestampMs = value
			}
		case msgRecallField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("recall_timestamp: %w", err)
			}
			b = rest
			if ok {
				msg.Recalled = value != 0
			}
		case msgQuoteField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("quote_msg_id: %w", err)
			}
			b = rest
			if ok {
				msg.QuoteMsgID = string(value)
			}
		case msgSeqField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("msg_seq: %w", err)
			}
			b = rest
			if ok {
				msg.MsgSeq = value
			}
		case msgEditTimeFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return msg, fmt.Errorf("edit_timestamp: %w", err)
			}
			b = rest
			if ok {
				msg.Edited = value != 0
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return msg, err
			}
		}
	}
	return msg, nil
}

// decodeSender decodes the Sender message.
func decodeSender(b []byte) (Sender, error) {
	var sender Sender
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return sender, err
		}
		b = next
		switch num {
		case senderChatIDField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return sender, fmt.Errorf("chat_id: %w", err)
			}
			b = rest
			if ok {
				sender.ID = string(value)
			}
		case senderChatTypeField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return sender, fmt.Errorf("chat_type: %w", err)
			}
			b = rest
			if ok {
				sender.ChatType = ChatType(int32(value))
			}
		case senderNameField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return sender, fmt.Errorf("name: %w", err)
			}
			b = rest
			if ok {
				sender.Name = string(value)
			}
		case senderAvatarField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return sender, fmt.Errorf("avatar_url: %w", err)
			}
			b = rest
			if ok {
				sender.AvatarURL = string(value)
			}
		case senderVIPField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return sender, fmt.Errorf("is_vip: %w", err)
			}
			b = rest
			if ok {
				sender.IsVIP = value != 0
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return sender, err
			}
		}
	}
	return sender, nil
}

// decodeMessageContent decodes the MsgContent message subset used by the TUI.
func decodeMessageContent(b []byte) (MessageContent, error) {
	var content MessageContent
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return content, err
		}
		b = next
		switch num {
		case contentTextField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("text: %w", err)
			}
			b = rest
			if ok {
				content.Text = string(value)
			}
		case contentImageURLField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("image_url: %w", err)
			}
			b = rest
			if ok {
				content.ImageURL = string(value)
			}
		case contentFileNameField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("file_name: %w", err)
			}
			b = rest
			if ok {
				content.FileName = string(value)
			}
		case contentFileURLField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("file_url: %w", err)
			}
			b = rest
			if ok {
				content.FileURL = string(value)
			}
		case contentFileSizeField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("file_size: %w", err)
			}
			b = rest
			if ok {
				content.FileSize = int64(value)
			}
		case contentFormField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("form: %w", err)
			}
			b = rest
			if ok {
				content.Form = string(value)
			}
		case contentQuoteTextField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("quote_msg_text: %w", err)
			}
			b = rest
			if ok {
				content.QuoteMsgText = string(value)
			}
		case contentImageField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("image: %w", err)
			}
			b = rest
			if ok {
				content.Image = string(value)
			}
		case contentPostIDField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("post_id: %w", err)
			}
			b = rest
			if ok {
				content.PostID = string(value)
			}
		case contentPostTitleFld:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("post_title: %w", err)
			}
			b = rest
			if ok {
				content.PostTitle = string(value)
			}
		case contentPostContentFld:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("post_content: %w", err)
			}
			b = rest
			if ok {
				content.PostContent = string(value)
			}
		case contentPostTypeFld:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("post_content_type: %w", err)
			}
			b = rest
			if ok {
				content.PostContentTyp = string(value)
			}
		case contentVideoURLField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("video_url: %w", err)
			}
			b = rest
			if ok {
				content.VideoURL = string(value)
			}
		case contentVideoTimeFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("video_time: %w", err)
			}
			b = rest
			if ok {
				content.VideoTime = int32(value)
			}
		case contentAudioURLField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("audio_url: %w", err)
			}
			b = rest
			if ok {
				content.AudioURL = string(value)
			}
		case contentAudioTimeFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("audio_time: %w", err)
			}
			b = rest
			if ok {
				content.AudioTime = int32(value)
			}
		case contentRoomNameFld:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("room_name: %w", err)
			}
			b = rest
			if ok {
				content.RoomName = string(value)
			}
		case contentCallStatusFld:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("call_status_text: %w", err)
			}
			b = rest
			if ok {
				content.CallStatus = string(value)
			}
		case contentWidthFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("width: %w", err)
			}
			b = rest
			if ok {
				content.Width = int32(value)
			}
		case contentHeightFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("height: %w", err)
			}
			b = rest
			if ok {
				content.Height = int32(value)
			}
		case contentStickerItemFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("sticker_item_id: %w", err)
			}
			b = rest
			if ok {
				content.StickerItemID = int64(value)
			}
		case contentStickerPackFld:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("sticker_pack_id: %w", err)
			}
			b = rest
			if ok {
				content.StickerPackID = int64(value)
			}
		case contentTipField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("tip: %w", err)
			}
			b = rest
			if ok {
				content.Tip = string(value)
			}
		case contentMentionedField:
			value, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return content, fmt.Errorf("mentioned_id: %w", err)
			}
			b = rest
			if ok {
				content.MentionedIDs = append(content.MentionedIDs, string(value))
			}
		default:
			b, err = skipField(num, typ, b)
			if err != nil {
				return content, err
			}
		}
	}
	return content, nil
}
