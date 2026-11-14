package api

// Hand-written protobuf decoder for GET /v1/user/info.
//
// The endpoint answers with `Content-Type: application/x-protobuf` when the
// request carries a `token` header (and with JSON, e.g. {"code":-101,...},
// when it does not), so exactly one message shape needs wire decoding and a
// code-generation step for a single .proto message would be overkill. The
// decoder below is therefore built directly on
// google.golang.org/protobuf/encoding/protowire.
//
// Wire schema (src/full.proto, region UserInfo):
//
//	message UserInfo {
//	  Status status = 1;
//	  Data data = 2;
//	}
//	message Status {
//	  int64 request_id = 1;
//	  int32 code = 2; // 1 = success
//	  string msg = 3;
//	}
//	message Data {
//	  string id = 1;
//	  string name = 2;
//	  int32 field3 = 3;
//	  string avatar_url = 4;
//	  int64 avatar_id = 5;
//	  string phone = 6;
//	  string email = 7;
//	  double coin = 8;
//	  bool is_vip = 9;
//	  int64 vip_expired_timestamp = 10;
//	  VipStatus vip_status = 11;
//	  string invitation_code = 12;
//	}
//
// The decoder is forward compatible in the same way proto.Unmarshal is:
// unknown fields (including known field numbers that arrive with an
// unexpected wire type) are skipped. Truncated or otherwise malformed input
// is reported as an error and never panics. An empty body is rejected because
// an empty HTTP 200 body is never a usable UserInfo response.

import (
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// errDecodeUserInfo is wrapped by every DecodeUserInfo failure, so callers can
// test for a malformed/unexpected body with errors.Is.
var errDecodeUserInfo = errors.New("api: invalid user info protobuf")

// Protobuf field numbers. These mirror src/full.proto and must not change
// independently of the server's schema.
const (
	userInfoStatusField protowire.Number = 1
	userInfoDataField   protowire.Number = 2
)

const (
	statusRequestIDField protowire.Number = 1
	statusCodeField      protowire.Number = 2
	statusMsgField       protowire.Number = 3
)

const (
	dataIDField                  protowire.Number = 1
	dataNameField                protowire.Number = 2
	dataField3Field              protowire.Number = 3
	dataAvatarURLField           protowire.Number = 4
	dataAvatarIDField            protowire.Number = 5
	dataPhoneField               protowire.Number = 6
	dataEmailField               protowire.Number = 7
	dataCoinField                protowire.Number = 8
	dataIsVIPField               protowire.Number = 9
	dataVIPExpiredTimestampField protowire.Number = 10
	dataVIPStatusField           protowire.Number = 11
	dataInvitationCodeField      protowire.Number = 12
)

// DecodeUserInfo decodes the raw protobuf body returned by GET /v1/user/info.
//
// Error responses (for example an invalid or expired token) decode without an
// error but carry no data: Status.Code is not 1 and UserInfo.Data is nil.
// Callers must inspect Status.Code before using Data.
func DecodeUserInfo(b []byte) (*UserInfo, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("%w: empty body", errDecodeUserInfo)
	}
	info, err := decodeUserInfoMessage(b)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errDecodeUserInfo, err)
	}
	return info, nil
}

// decodeUserInfoMessage decodes a UserInfo message (top-level fields 1 and 2).
func decodeUserInfoMessage(b []byte) (*UserInfo, error) {
	info := &UserInfo{}
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return nil, err
		}
		b = next
		switch num {
		case userInfoStatusField:
			payload, next, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return nil, fmt.Errorf("status: %w", err)
			}
			b = next
			if !ok {
				continue
			}
			st, err := decodeStatus(payload)
			if err != nil {
				return nil, fmt.Errorf("status: %w", err)
			}
			info.Status = st
		case userInfoDataField:
			payload, next, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return nil, fmt.Errorf("data: %w", err)
			}
			b = next
			if !ok {
				continue
			}
			data := &UserInfoData{}
			if err := decodeUserInfoData(payload, data); err != nil {
				return nil, fmt.Errorf("data: %w", err)
			}
			info.Data = data
		default:
			next, err := skipField(num, typ, b)
			if err != nil {
				return nil, err
			}
			b = next
		}
	}
	return info, nil
}

// decodeStatus decodes the Status message (fields 1, 2 and 3).
func decodeStatus(b []byte) (Status, error) {
	var st Status
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return st, err
		}
		b = next
		switch num {
		case statusRequestIDField:
			v, next, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return st, fmt.Errorf("request_id: %w", err)
			}
			b = next
			if ok {
				st.RequestID = int64(v)
			}
		case statusCodeField:
			v, next, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return st, fmt.Errorf("code: %w", err)
			}
			b = next
			if ok {
				st.Code = int32(v)
			}
		case statusMsgField:
			v, next, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return st, fmt.Errorf("msg: %w", err)
			}
			b = next
			if ok {
				st.Msg = string(v)
			}
		default:
			next, err := skipField(num, typ, b)
			if err != nil {
				return st, err
			}
			b = next
		}
	}
	return st, nil
}

// decodeUserInfoData decodes the UserInfo.Data message (fields 1 through 12).
func decodeUserInfoData(b []byte, data *UserInfoData) error {
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return err
		}
		b = next
		switch num {
		case dataIDField:
			if err := decodeStringField(num, typ, &b, &data.ID); err != nil {
				return fmt.Errorf("id: %w", err)
			}
		case dataNameField:
			if err := decodeStringField(num, typ, &b, &data.Name); err != nil {
				return fmt.Errorf("name: %w", err)
			}
		case dataField3Field:
			if err := decodeInt32Field(num, typ, &b, &data.Field3); err != nil {
				return fmt.Errorf("field3: %w", err)
			}
		case dataAvatarURLField:
			if err := decodeStringField(num, typ, &b, &data.AvatarURL); err != nil {
				return fmt.Errorf("avatar_url: %w", err)
			}
		case dataAvatarIDField:
			if err := decodeInt64Field(num, typ, &b, &data.AvatarID); err != nil {
				return fmt.Errorf("avatar_id: %w", err)
			}
		case dataPhoneField:
			if err := decodeStringField(num, typ, &b, &data.Phone); err != nil {
				return fmt.Errorf("phone: %w", err)
			}
		case dataEmailField:
			if err := decodeStringField(num, typ, &b, &data.Email); err != nil {
				return fmt.Errorf("email: %w", err)
			}
		case dataCoinField:
			v, next, ok, err := consumeFixed64Field(num, typ, b)
			if err != nil {
				return fmt.Errorf("coin: %w", err)
			}
			b = next
			if ok {
				data.Coin = math.Float64frombits(v)
			}
		case dataIsVIPField:
			v, next, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("is_vip: %w", err)
			}
			b = next
			if ok {
				data.IsVIP = v != 0
			}
		case dataVIPExpiredTimestampField:
			if err := decodeInt64Field(num, typ, &b, &data.VIPExpiredTimestamp); err != nil {
				return fmt.Errorf("vip_expired_timestamp: %w", err)
			}
		case dataVIPStatusField:
			if err := decodeInt32Field(num, typ, &b, &data.VIPStatus); err != nil {
				return fmt.Errorf("vip_status: %w", err)
			}
		case dataInvitationCodeField:
			if err := decodeStringField(num, typ, &b, &data.InvitationCode); err != nil {
				return fmt.Errorf("invitation_code: %w", err)
			}
		default:
			next, err := skipField(num, typ, b)
			if err != nil {
				return err
			}
			b = next
		}
	}
	return nil
}

// decodeStringField decodes one string field into dst and advances *b past it.
// A field with an unexpected wire type is skipped, leaving dst untouched.
func decodeStringField(num protowire.Number, typ protowire.Type, b *[]byte, dst *string) error {
	v, next, ok, err := consumeBytesField(num, typ, *b)
	if err != nil {
		return err
	}
	*b = next
	if ok {
		*dst = string(v)
	}
	return nil
}

// decodeInt32Field decodes one int32 field into dst. The wire value is
// truncated exactly like protobuf does, so negative int32 values (encoded
// sign-extended to ten varint bytes) decode correctly.
func decodeInt32Field(num protowire.Number, typ protowire.Type, b *[]byte, dst *int32) error {
	v, next, ok, err := consumeVarintField(num, typ, *b)
	if err != nil {
		return err
	}
	*b = next
	if ok {
		*dst = int32(v)
	}
	return nil
}

// decodeInt64Field decodes one int64 field into dst, preserving protobuf's
// two's-complement varint semantics for negative values.
func decodeInt64Field(num protowire.Number, typ protowire.Type, b *[]byte, dst *int64) error {
	v, next, ok, err := consumeVarintField(num, typ, *b)
	if err != nil {
		return err
	}
	*b = next
	if ok {
		*dst = int64(v)
	}
	return nil
}

// consumeTag reads one field tag, returning the remaining buffer.
func consumeTag(b []byte) (num protowire.Number, typ protowire.Type, rest []byte, err error) {
	num, typ, n := protowire.ConsumeTag(b)
	if n < 0 {
		return 0, 0, nil, fmt.Errorf("invalid field tag: %w", protowire.ParseError(n))
	}
	return num, typ, b[n:], nil
}

// consumeVarintField decodes a varint-typed value. When the field arrives with
// a wire type other than varint it is skipped and ok is false, mirroring how
// protobuf parsers treat wire-type mismatches as unknown fields.
func consumeVarintField(num protowire.Number, typ protowire.Type, b []byte) (val uint64, rest []byte, ok bool, err error) {
	if typ != protowire.VarintType {
		rest, err = skipField(num, typ, b)
		return 0, rest, false, err
	}
	val, n := protowire.ConsumeVarint(b)
	if n < 0 {
		return 0, nil, false, fieldParseError(num, n)
	}
	return val, b[n:], true, nil
}

// consumeFixed64Field decodes a fixed64-typed value (used for double fields).
func consumeFixed64Field(num protowire.Number, typ protowire.Type, b []byte) (val uint64, rest []byte, ok bool, err error) {
	if typ != protowire.Fixed64Type {
		rest, err = skipField(num, typ, b)
		return 0, rest, false, err
	}
	val, n := protowire.ConsumeFixed64(b)
	if n < 0 {
		return 0, nil, false, fieldParseError(num, n)
	}
	return val, b[n:], true, nil
}

// consumeBytesField decodes a length-delimited value (string or embedded
// message).
func consumeBytesField(num protowire.Number, typ protowire.Type, b []byte) (val []byte, rest []byte, ok bool, err error) {
	if typ != protowire.BytesType {
		rest, err = skipField(num, typ, b)
		return nil, rest, false, err
	}
	val, n := protowire.ConsumeBytes(b)
	if n < 0 {
		return nil, nil, false, fieldParseError(num, n)
	}
	return val, b[n:], true, nil
}

// skipField consumes and discards one field value (including groups).
func skipField(num protowire.Number, typ protowire.Type, b []byte) (rest []byte, err error) {
	n := protowire.ConsumeFieldValue(num, typ, b)
	if n < 0 {
		return nil, fieldParseError(num, n)
	}
	return b[n:], nil
}

// fieldParseError turns protowire's negative length code into an error that
// names the offending field.
func fieldParseError(num protowire.Number, n int) error {
	return fmt.Errorf("field %d: %w", num, protowire.ParseError(n))
}
