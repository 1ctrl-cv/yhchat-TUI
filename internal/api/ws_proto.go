package api

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// errWSDecode wraps malformed WebSocket protobuf payloads.
var errWSDecode = errors.New("WebSocket 消息格式不正确")

// wsWrapper is the outer frame of every Yunhu WebSocket push.
type wsWrapper struct {
	Cmd   string
	Value []byte // google.protobuf.Any.value
}

// decodeWSWrapper parses a WsWrapper{info=1,data=2} payload.
func decodeWSWrapper(data []byte) (*wsWrapper, error) {
	wrapper := &wsWrapper{}
	err := forEachField(data, func(field protowire.Number, typ protowire.Type, rest []byte) error {
		switch field {
		case 1: // WsInfo info
			if typ != protowire.BytesType {
				return nil
			}
			info, n := protowire.ConsumeBytes(rest)
			if n < 0 {
				return errWSDecode
			}
			var cmd string
			infoErr := forEachField(info, func(f protowire.Number, t protowire.Type, r []byte) error {
				if f == 2 && t == protowire.BytesType {
					if value, m := protowire.ConsumeString(r); m >= 0 {
						cmd = value
					}
				}
				return nil
			})
			if infoErr != nil {
				return infoErr
			}
			wrapper.Cmd = cmd
		case 2: // google.protobuf.Any data
			if typ != protowire.BytesType {
				return nil
			}
			anyMsg, n := protowire.ConsumeBytes(rest)
			if n < 0 {
				return errWSDecode
			}
			anyErr := forEachField(anyMsg, func(f protowire.Number, t protowire.Type, r []byte) error {
				if f == 2 && t == protowire.BytesType { // Any.value
					if value, m := protowire.ConsumeBytes(r); m >= 0 {
						wrapper.Value = value
					}
				}
				return nil
			})
			if anyErr != nil {
				return anyErr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if wrapper.Cmd == "" {
		return nil, fmt.Errorf("%w: 缺少 info.cmd", errWSDecode)
	}
	return wrapper, nil
}

// PushMessage is the decoded payload of push_message / edit_message /
// blocked_message WebSocket events.
type PushMessage struct {
	MsgID           string
	Sender          Sender
	RecvID          string
	ChatID          string
	ChatType        ChatType
	Content         MessageContent
	ContentType     int32
	TimestampMs     uint64
	QuoteMsgID      string
	MsgSeq          uint64
	DeleteTimestamp uint64
	EditTimestamp   uint64
}

// decodePushMessage parses a PushMessage payload.
func decodePushMessage(data []byte) (*PushMessage, error) {
	msg := &PushMessage{}
	err := forEachField(data, func(field protowire.Number, typ protowire.Type, rest []byte) error {
		switch field {
		case 1, 3, 4, 11:
			if typ != protowire.BytesType {
				return nil
			}
			value, n := protowire.ConsumeString(rest)
			if n < 0 {
				return errWSDecode
			}
			switch field {
			case 1:
				msg.MsgID = value
			case 3:
				msg.RecvID = value
			case 4:
				msg.ChatID = value
			case 11:
				msg.QuoteMsgID = value
			}
		case 2: // Sender
			if typ != protowire.BytesType {
				return nil
			}
			raw, n := protowire.ConsumeBytes(rest)
			if n < 0 {
				return errWSDecode
			}
			sender, senderErr := decodeSender(raw)
			if senderErr != nil {
				return senderErr
			}
			msg.Sender = sender
		case 5: // chat_type
			if typ == protowire.VarintType {
				value, n := protowire.ConsumeVarint(rest)
				if n < 0 {
					return errWSDecode
				}
				msg.ChatType = ChatType(value)
			}
		case 6: // MsgContent
			if typ != protowire.BytesType {
				return nil
			}
			raw, n := protowire.ConsumeBytes(rest)
			if n < 0 {
				return errWSDecode
			}
			content, contentErr := decodeMessageContent(raw)
			if contentErr != nil {
				return contentErr
			}
			msg.Content = content
		case 7, 8, 10, 12, 14: // content_type, timestamp, delete_timestamp, msg_seq, edit_timestamp
			if typ != protowire.VarintType {
				return nil
			}
			value, n := protowire.ConsumeVarint(rest)
			if n < 0 {
				return errWSDecode
			}
			switch field {
			case 7:
				msg.ContentType = int32(value)
			case 8:
				msg.TimestampMs = value
			case 10:
				msg.DeleteTimestamp = value
			case 12:
				msg.MsgSeq = value
			case 14:
				msg.EditTimestamp = value
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if msg.MsgID == "" && msg.ChatID == "" {
		return nil, fmt.Errorf("%w: 推送消息缺少 ID", errWSDecode)
	}
	return msg, nil
}

// StreamMessage is the decoded payload of a stream_message event.
type StreamMessage struct {
	MsgID   string
	RecvID  string
	ChatID  string
	Content string
}

// decodeStreamMessage parses a StreamMessage payload.
func decodeStreamMessage(data []byte) (*StreamMessage, error) {
	stream := &StreamMessage{}
	err := forEachField(data, func(field protowire.Number, typ protowire.Type, rest []byte) error {
		if typ != protowire.BytesType {
			return nil
		}
		value, n := protowire.ConsumeString(rest)
		if n < 0 {
			return errWSDecode
		}
		switch field {
		case 1:
			stream.MsgID = value
		case 2:
			stream.RecvID = value
		case 3:
			stream.ChatID = value
		case 4:
			stream.Content = value
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stream.MsgID == "" {
		return nil, fmt.Errorf("%w: 流式消息缺少 ID", errWSDecode)
	}
	return stream, nil
}

// RecallMessage is the decoded payload of a recall event (either a RecallMsg
// request-like payload or a sparse PushMessage).
type RecallMessage struct {
	ChatID   string
	ChatType ChatType
	MsgIDs   []string
	MsgSeq   uint64
}

// decodeRecallMessage parses a RecallMsg payload (msg_id=2 repeated, chat_id=3,
// chat_type=4).
func decodeRecallMessage(data []byte) (*RecallMessage, error) {
	recall := &RecallMessage{}
	err := forEachField(data, func(field protowire.Number, typ protowire.Type, rest []byte) error {
		switch field {
		case 2:
			if typ == protowire.BytesType {
				if value, n := protowire.ConsumeString(rest); n >= 0 && value != "" {
					recall.MsgIDs = append(recall.MsgIDs, value)
				}
			}
		case 3:
			if typ == protowire.BytesType {
				if value, n := protowire.ConsumeString(rest); n >= 0 {
					recall.ChatID = value
				}
			}
		case 4:
			if typ == protowire.VarintType {
				if value, n := protowire.ConsumeVarint(rest); n >= 0 {
					recall.ChatType = ChatType(value)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(recall.MsgIDs) == 0 {
		return nil, fmt.Errorf("%w: 撤回消息缺少消息 ID", errWSDecode)
	}
	return recall, nil
}

// forEachField iterates the top-level fields of a protobuf message.
func forEachField(data []byte, fn func(field protowire.Number, typ protowire.Type, rest []byte) error) error {
	for len(data) > 0 {
		field, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return errWSDecode
		}
		data = data[n:]
		if err := fn(field, typ, data); err != nil {
			return err
		}
		m := protowire.ConsumeFieldValue(field, typ, data)
		if m < 0 {
			return errWSDecode
		}
		data = data[m:]
	}
	return nil
}
