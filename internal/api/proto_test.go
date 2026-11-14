package api

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// invalidTokenHex is the live GET /v1/user/info protobuf body captured for an
// invalid token (see docs/API_NOTES.md):
//
//	0a 23          field 1 (status), 35 bytes
//	  08 <varint>  request_id  (9 bytes: d0 af d1 f7 bc 9c a6 cf 49)
//	  10 02        code = 2
//	  1a 15 <msg>  msg = "未登录，请登录"
const invalidTokenHex = "0a2308d0afd1f7bc9ca6cf4910021a15e69caae799bbe5bd95efbc8ce8afb7e799bbe5bd95"

// testInvalidTokenWire returns the fixture above as bytes.
func testInvalidTokenWire(t *testing.T) []byte {
	t.Helper()
	raw, err := hex.DecodeString(invalidTokenHex)
	if err != nil {
		t.Fatalf("hex.DecodeString(%q): %v", invalidTokenHex, err)
	}
	return raw
}

// -- protobuf encoding helpers for building test messages -------------------

func appendVarintField(b []byte, num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, num, protowire.VarintType), v)
}

// appendInt64Field encodes a (possibly negative) int64 the way protobuf does:
// sign-extended to a uint64 varint. The v parameter makes the uint64
// conversion legal for negative constants.
func appendInt64Field(b []byte, num protowire.Number, v int64) []byte {
	return appendVarintField(b, num, uint64(v))
}

func appendFixed32Field(b []byte, num protowire.Number, v uint32) []byte {
	return protowire.AppendFixed32(protowire.AppendTag(b, num, protowire.Fixed32Type), v)
}

func appendFixed64Field(b []byte, num protowire.Number, v uint64) []byte {
	return protowire.AppendFixed64(protowire.AppendTag(b, num, protowire.Fixed64Type), v)
}

func appendDoubleField(b []byte, num protowire.Number, v float64) []byte {
	return appendFixed64Field(b, num, math.Float64bits(v))
}

func appendBytesField(b []byte, num protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), v)
}

func appendStringField(b []byte, num protowire.Number, v string) []byte {
	return protowire.AppendString(protowire.AppendTag(b, num, protowire.BytesType), v)
}

// --------------------------------------------------------------------------

func TestDecodeUserInfoInvalidTokenFixture(t *testing.T) {
	got, err := DecodeUserInfo(testInvalidTokenWire(t))
	if err != nil {
		t.Fatalf("DecodeUserInfo() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("DecodeUserInfo() = nil, want non-nil")
	}
	// The 9-byte request_id varint d0 af d1 f7 bc 9c a6 cf 49 is the 64-bit
	// value 0x499e98e3cef457d0 = 5304845515286140880, which fits in int64.
	wantStatus := Status{
		RequestID: 5304845515286140880,
		Code:      2,
		Msg:       "未登录，请登录",
	}
	if got.Status != wantStatus {
		t.Errorf("Status = %+v, want %+v", got.Status, wantStatus)
	}
	if got.Data != nil {
		t.Errorf("Data = %+v, want nil (error responses carry no data)", got.Data)
	}
}

func TestDecodeUserInfoSuccess(t *testing.T) {
	// status = {request_id: 42, code: 1, msg: "success"}
	var statusWire []byte
	statusWire = appendVarintField(statusWire, 1, 42)
	statusWire = appendVarintField(statusWire, 2, 1)
	statusWire = appendStringField(statusWire, 3, "success")

	// data with every field set.
	var dataWire []byte
	dataWire = appendStringField(dataWire, 1, "u_123456")
	dataWire = appendStringField(dataWire, 2, "测试用户")
	dataWire = appendInt64Field(dataWire, 3, -7) // negative int32, sign-extended on the wire
	dataWire = appendStringField(dataWire, 4, "https://example.com/avatar.png")
	dataWire = appendVarintField(dataWire, 5, 9007199254740993) // 2^53+1, varint beyond float64 precision
	dataWire = appendStringField(dataWire, 6, "13800000000")
	dataWire = appendStringField(dataWire, 7, "user@example.com")
	dataWire = appendDoubleField(dataWire, 8, -1234.5678901234567) // negative double through fixed64
	dataWire = appendVarintField(dataWire, 9, 1)                   // is_vip = true
	dataWire = appendInt64Field(dataWire, 10, -2)                  // negative int64
	dataWire = appendVarintField(dataWire, 11, 2)                  // VipStatus: expired
	dataWire = appendStringField(dataWire, 12, "INVITE-42")
	// Unknown fields inside Data must be skipped, including group-encoded ones.
	dataWire = appendVarintField(dataWire, 99, 7)
	dataWire = appendFixed32Field(dataWire, 100, 0xdeadbeef)
	dataWire = appendBytesField(dataWire, 101, []byte{1, 2, 3})
	dataWire = protowire.AppendTag(dataWire, 50, protowire.StartGroupType)
	dataWire = appendVarintField(dataWire, 1, 77)
	dataWire = protowire.AppendTag(dataWire, 50, protowire.EndGroupType)

	var wire []byte
	wire = appendBytesField(wire, 1, statusWire)
	wire = appendBytesField(wire, 2, dataWire)
	// Unknown top-level fields must be skipped too.
	wire = appendVarintField(wire, 3, 123)
	wire = appendFixed64Field(wire, 4, 456)
	wire = appendStringField(wire, 5, "ignore me")

	got, err := DecodeUserInfo(wire)
	if err != nil {
		t.Fatalf("DecodeUserInfo() error = %v, want nil", err)
	}
	want := &UserInfo{
		Status: Status{RequestID: 42, Code: 1, Msg: "success"},
		Data: &UserInfoData{
			ID:                  "u_123456",
			Name:                "测试用户",
			Field3:              -7,
			AvatarURL:           "https://example.com/avatar.png",
			AvatarID:            9007199254740993,
			Phone:               "13800000000",
			Email:               "user@example.com",
			Coin:                -1234.5678901234567,
			IsVIP:               true,
			VIPExpiredTimestamp: -2,
			VIPStatus:           2,
			InvitationCode:      "INVITE-42",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodeUserInfo() = %+v, want %+v", got, want)
	}
}

func TestDecodeUserInfoNegativeRequestID(t *testing.T) {
	// int64 request_id = -1 is encoded as a ten-byte sign-extended varint.
	var statusWire []byte
	statusWire = appendInt64Field(statusWire, 1, -1)
	wire := appendBytesField(nil, 1, statusWire)

	got, err := DecodeUserInfo(wire)
	if err != nil {
		t.Fatalf("DecodeUserInfo() error = %v, want nil", err)
	}
	if got.Status.RequestID != -1 {
		t.Errorf("Status.RequestID = %d, want -1", got.Status.RequestID)
	}
}

func TestDecodeUserInfoDataPresentButEmpty(t *testing.T) {
	// An explicitly present but empty Data message must yield a non-nil Data,
	// unlike an absent one.
	wire := appendBytesField(nil, 2, nil)

	got, err := DecodeUserInfo(wire)
	if err != nil {
		t.Fatalf("DecodeUserInfo() error = %v, want nil", err)
	}
	want := &UserInfo{Data: &UserInfoData{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodeUserInfo() = %+v, want %+v", got, want)
	}
}

func TestDecodeUserInfoUnknownFieldsOnly(t *testing.T) {
	var wire []byte
	wire = appendVarintField(wire, 3, 1)
	wire = appendFixed64Field(wire, 4, 2)
	wire = appendStringField(wire, 5, "unknown")

	got, err := DecodeUserInfo(wire)
	if err != nil {
		t.Fatalf("DecodeUserInfo() error = %v, want nil", err)
	}
	want := &UserInfo{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodeUserInfo() = %+v, want %+v", got, want)
	}
}

func TestDecodeUserInfoWrongWireTypeSkipped(t *testing.T) {
	// Known field numbers carrying an unexpected wire type are treated as
	// unknown fields (like proto.Unmarshal), not as a hard error.
	var wire []byte
	wire = appendVarintField(wire, 1, 99) // status as a varint, not a message
	wire = appendVarintField(wire, 2, 5)  // data as a varint, not a message

	got, err := DecodeUserInfo(wire)
	if err != nil {
		t.Fatalf("DecodeUserInfo() error = %v, want nil", err)
	}
	want := &UserInfo{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodeUserInfo() = %+v, want %+v", got, want)
	}

	// Same for a scalar inside Status: code sent as a string instead of a varint.
	var statusWire []byte
	statusWire = appendStringField(statusWire, 2, "not a varint")
	got, err = DecodeUserInfo(appendBytesField(nil, 1, statusWire))
	if err != nil {
		t.Fatalf("DecodeUserInfo() error = %v, want nil", err)
	}
	if got.Status.Code != 0 {
		t.Errorf("Status.Code = %d, want 0", got.Status.Code)
	}
}

func TestDecodeUserInfoMalformed(t *testing.T) {
	tests := []struct {
		name    string
		in      []byte
		wantMsg string // optional substring the error must contain
	}{
		{name: "nil body", in: nil, wantMsg: "empty body"},
		{name: "empty body", in: []byte{}, wantMsg: "empty body"},
		{name: "tag without length", in: []byte{0x0a}},
		{name: "truncated status message", in: []byte{0x0a, 0x05, 0x08}},
		{name: "truncated request_id varint", in: []byte{0x0a, 0x01, 0x08}},
		{name: "truncated msg string", in: []byte{0x0a, 0x02, 0x1a, 0x05}},
		{name: "truncated data message", in: []byte{0x12, 0x02, 0x0a, 0x05}},
		{name: "truncated coin fixed64", in: []byte{0x12, 0x03, 0x41, 0x00, 0x00}},
		{name: "truncated unknown varint", in: []byte{0xf8, 0x07, 0x80, 0x80}},
		{name: "invalid field number zero", in: []byte{0x00}},
		{name: "invalid wire type", in: []byte{0x0f}},
		{name: "length larger than body", in: []byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0x0f}},
		{name: "json error body", in: []byte(`{"code":-101,"msg":"未登录"}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeUserInfo(tc.in)
			if err == nil {
				t.Fatalf("DecodeUserInfo(% x) = %+v, want error", tc.in, got)
			}
			if got != nil {
				t.Errorf("DecodeUserInfo() = %+v on error, want nil", got)
			}
			if !errors.Is(err, errDecodeUserInfo) {
				t.Errorf("errors.Is(err, errDecodeUserInfo) = false; err = %v", err)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tc.wantMsg)
			}
		})
	}
}

// TestDecodeUserInfoNeverPanics feeds truncations, corruptions and random
// input to the decoder: it may fail, but it must never panic.
func TestDecodeUserInfoNeverPanics(t *testing.T) {
	raw := testInvalidTokenWire(t)

	// Every truncation of a valid message.
	for i := 0; i <= len(raw); i++ {
		if _, err := DecodeUserInfo(raw[:i]); err != nil && i == len(raw) {
			t.Fatalf("DecodeUserInfo(valid) error = %v", err)
		}
	}

	// Every single-bit corruption of a valid message.
	for i := range raw {
		for bit := 0; bit < 8; bit++ {
			corrupt := bytes.Clone(raw)
			corrupt[i] ^= 1 << bit
			_, _ = DecodeUserInfo(corrupt)
		}
	}

	// Pseudo-random buffers of varying lengths.
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		buf := make([]byte, rng.Intn(64))
		rng.Read(buf)
		_, _ = DecodeUserInfo(buf)
	}
}
