package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// ---------------------------------------------------------------------------
// protobuf fixtures for the address book / request list endpoints

// testAppendInt32 appends a signed proto int32 as a varint field (negative
// values are sign-extended like the official encoders do).
func testAppendInt32(b []byte, field protowire.Number, value int32) []byte {
	return testAppendVarint(b, field, uint64(int32ToUint32(value)))
}

// testContactItem builds one AddressBookListResponse.Data.DataList message.
// An unknown field (6) is appended to exercise unknown-field skipping.
func testContactItem(chatID, remark, avatarURL, name string, permissionLevel int32, noDisturb bool) []byte {
	var b []byte
	b = testAppendString(b, 1, chatID)
	if remark != "" {
		b = testAppendString(b, 2, remark)
	}
	if avatarURL != "" {
		b = testAppendString(b, 3, avatarURL)
	}
	if permissionLevel != 0 {
		b = testAppendInt32(b, 4, permissionLevel)
	}
	if noDisturb {
		b = testAppendVarint(b, 5, 1)
	}
	b = testAppendVarint(b, 6, 999) // unknown field in DataList
	if name != "" {
		b = testAppendString(b, 8, name)
	}
	return b
}

// testContactGroupBytes builds one AddressBookListResponse.Data message.
func testContactGroupBytes(listName string, chatType int32, items ...[]byte) []byte {
	var b []byte
	b = testAppendString(b, 1, listName)
	for _, item := range items {
		b = testAppendBytes(b, 2, item)
	}
	b = testAppendInt32(b, 3, chatType)
	b = testAppendString(b, 7, "unknown-group-field") // unknown field in Data
	return b
}

// testAddressBookResponse builds a successful AddressBookListResponse with all
// three groups (users, joined groups and bots). Group permissions are included
// for the group section (群主=100, 管理员=2).
func testAddressBookResponse() []byte {
	userGroup := testContactGroupBytes("用户", 1,
		testContactItem("u-1001", "备注小明", "https://img.example/u1.png", "小明", 0, false),
		testContactItem("u-1002", "", "", "小红", 0, true),
	)
	groupGroup := testContactGroupBytes("我加入的群聊", 2,
		testContactItem("g-2001", "测试群", "https://img.example/g1.png", "测试群真名", 100, false),
		testContactItem("g-2002", "", "", "管理群", 2, true),
	)
	botGroup := testContactGroupBytes("机器人", 3,
		testContactItem("bot-3001", "小助手备注", "https://img.example/bot.png", "小助手", 0, true),
	)

	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(1, "success"))
	out = testAppendBytes(out, 2, userGroup)
	out = testAppendBytes(out, 2, groupGroup)
	out = testAppendBytes(out, 2, botGroup)
	out = testAppendString(out, 3, "address-book-md5")
	out = testAppendVarint(out, 9, 42) // unknown top-level field
	return out
}

// testRequestListResponse builds a successful RequestListResponse with two
// requests and the total/pending counters.
func testRequestListResponse() []byte {
	first := []byte{}
	first = testAppendString(first, 1, "接收者甲")
	first = testAppendString(first, 2, "https://img.example/receiver.png")
	first = testAppendString(first, 3, "申请者甲")
	first = testAppendString(first, 4, "https://img.example/applicant.png")
	first = testAppendString(first, 5, "测试群")
	first = testAppendString(first, 6, "https://img.example/group.png")
	first = testAppendString(first, 7, "inviter-1")
	first = testAppendVarint(first, 8, 8) // field 8 duplicates inviter_id; must be skipped
	first = testAppendInt32(first, 9, 3)
	first = testAppendInt32(first, 10, 2)
	first = testAppendString(first, 11, "g-2001")
	first = testAppendString(first, 12, "receiver-1")
	first = testAppendInt32(first, 13, -1)
	first = testAppendVarint(first, 14, 1710000500)
	first = testAppendVarint(first, 15, 15) // unknown field; must be skipped
	first = testAppendVarint(first, 16, 1710000400)
	first = testAppendString(first, 17, "2024-03-09 20:06:40")
	first = testAppendInt32(first, 18, 123)
	first = testAppendString(first, 19, "小助手")
	first = testAppendString(first, 20, "https://img.example/bot.png")
	first = testAppendVarint(first, 21, 21) // unknown field; must be skipped
	first = testAppendString(first, 22, "处理者甲")
	first = testAppendString(first, 23, "备注信息")
	first = testAppendVarint(first, 24, 1)

	second := []byte{}
	second = testAppendString(second, 3, "申请者乙")
	second = testAppendInt32(second, 10, 1)
	second = testAppendInt32(second, 18, 7)

	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(1, "success"))
	out = testAppendBytes(out, 2, first)
	out = testAppendBytes(out, 2, second)
	out = testAppendInt32(out, 3, 5)  // total
	out = testAppendInt32(out, 4, 2)  // pending
	out = testAppendVarint(out, 9, 1) // unknown top-level field
	return out
}

// writeProtoResp writes a protobuf response body.
func writeProtoResp(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(body)
}

// ---------------------------------------------------------------------------
// AddressBook

func TestAddressBook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/friend/address-book-list" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-contacts" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-protobuf" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		// AddressBookListRequest{md5=""}: field 2 must still be encoded.
		if got := parseRequestFieldBytes(t, body, 2); got == nil {
			t.Errorf("request is missing md5 field 2 (body %x)", body)
		} else if len(got) != 0 {
			t.Errorf("md5 = %q, want empty", got)
		}
		writeProtoResp(w, testAddressBookResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	groups, err := client.AddressBook(context.Background(), "tok-contacts")
	if err != nil {
		t.Fatalf("AddressBook: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}

	users := groups[0]
	if users.Name != "用户" || users.ChatType != ChatTypeUser {
		t.Fatalf("user group = %+v", users)
	}
	if len(users.Items) != 2 {
		t.Fatalf("user items = %d, want 2", len(users.Items))
	}
	u1 := users.Items[0]
	if u1.ID != "u-1001" || u1.Remark != "备注小明" || u1.Name != "小明" ||
		u1.AvatarURL != "https://img.example/u1.png" || u1.PermissionLevel != 0 || u1.NoDisturb {
		t.Fatalf("unexpected first user: %+v", u1)
	}
	if u1.DisplayName() != "备注小明" {
		t.Fatalf("display name = %q", u1.DisplayName())
	}
	u2 := users.Items[1]
	if u2.ID != "u-1002" || u2.Name != "小红" || !u2.NoDisturb {
		t.Fatalf("unexpected second user: %+v", u2)
	}
	if u2.DisplayName() != "小红" {
		t.Fatalf("display name = %q", u2.DisplayName())
	}

	joined := groups[1]
	if joined.Name != "我加入的群聊" || joined.ChatType != ChatTypeGroup {
		t.Fatalf("group section = %+v", joined)
	}
	if len(joined.Items) != 2 {
		t.Fatalf("group items = %d, want 2", len(joined.Items))
	}
	owner := joined.Items[0]
	if owner.ID != "g-2001" || owner.PermissionLevel != 100 || owner.NoDisturb {
		t.Fatalf("unexpected owner group: %+v", owner)
	}
	admin := joined.Items[1]
	if admin.PermissionLevel != 2 || !admin.NoDisturb {
		t.Fatalf("unexpected admin group: %+v", admin)
	}

	bots := groups[2]
	if bots.Name != "机器人" || bots.ChatType != ChatTypeBot {
		t.Fatalf("bot section = %+v", bots)
	}
	if len(bots.Items) != 1 {
		t.Fatalf("bot items = %d, want 1", len(bots.Items))
	}
	bot := bots.Items[0]
	if bot.ID != "bot-3001" || bot.Name != "小助手" || bot.AvatarURL != "https://img.example/bot.png" ||
		!bot.NoDisturb {
		t.Fatalf("unexpected bot: %+v", bot)
	}
	if bot.DisplayName() != "小助手备注" {
		t.Fatalf("bot display name = %q", bot.DisplayName())
	}
}

func TestAddressBookEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, testAppendBytes(nil, 1, testStatusBytes(1, "success")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	groups, err := client.AddressBook(context.Background(), "tok-contacts")
	if err != nil {
		t.Fatalf("AddressBook: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %d, want 0", len(groups))
	}
}

// A group section without items must be preserved (the TUI renders the empty
// section header) and keep its chat type.
func TestAddressBookGroupWithoutItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := testAppendBytes(nil, 1, testStatusBytes(1, "success"))
		out = testAppendBytes(out, 2, testContactGroupBytes("机器人", 3))
		writeProtoResp(w, out)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	groups, err := client.AddressBook(context.Background(), "tok-contacts")
	if err != nil {
		t.Fatalf("AddressBook: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "机器人" || groups[0].ChatType != ChatTypeBot {
		t.Fatalf("groups = %+v", groups)
	}
	if len(groups[0].Items) != 0 {
		t.Fatalf("items = %d, want 0", len(groups[0].Items))
	}
}

func TestAddressBookNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, testAppendBytes(nil, 1, testStatusBytes(2, "未登录，请登录")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.AddressBook(context.Background(), "bad-token")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestAddressBookJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-101,"msg":"未登录"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.AddressBook(context.Background(), "bad-token")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
	if !strings.Contains(err.Error(), "未登录") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddressBookMalformedProto(t *testing.T) {
	cases := map[string][]byte{
		"truncated status": {0x0a, 0x05, 0x08}, // field 1 length 5, only 2 bytes follow
		"invalid tag":      {0x00},             // field number 0 is invalid
		"empty body":       {},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeProtoResp(w, payload)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			groups, err := client.AddressBook(context.Background(), "tok-contacts")
			if err == nil {
				t.Fatalf("expected parse error, got groups %+v", groups)
			}
			if !strings.Contains(err.Error(), "解析通讯录失败") {
				t.Fatalf("err = %v, want wrapped parse failure", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FriendRequests

func TestFriendRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/friend/request-list" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-contacts" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-protobuf" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("request body = %x, want empty (RequestListRequest has no fields)", body)
		}
		writeProtoResp(w, testRequestListResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	result, err := client.FriendRequests(context.Background(), "tok-contacts")
	if err != nil {
		t.Fatalf("FriendRequests: %v", err)
	}
	if result == nil {
		t.Fatal("nil FriendRequests")
	}
	if result.Total != 5 || result.Pending != 2 {
		t.Fatalf("total/pending = %d/%d, want 5/2", result.Total, result.Pending)
	}
	if len(result.Requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(result.Requests))
	}

	first := result.Requests[0]
	want := InviteRequest{
		RequestID:      123,
		ReceiverName:   "接收者甲",
		ReceiverAvatar: "https://img.example/receiver.png",
		Name:           "申请者甲",
		Avatar:         "https://img.example/applicant.png",
		GroupName:      "测试群",
		GroupAvatar:    "https://img.example/group.png",
		InviterID:      "inviter-1",
		SourceType:     3,
		TargetType:     2,
		TargetID:       "g-2001",
		ReceiverID:     "receiver-1",
		Result:         -1,
		ProcessedAt:    1710000500,
		InviteAt:       1710000400,
		InviteAtStr:    "2024-03-09 20:06:40",
		BotName:        "小助手",
		BotAvatar:      "https://img.example/bot.png",
		ProcessorName:  "处理者甲",
		Note:           "备注信息",
		IsVIP:          true,
	}
	if first != want {
		t.Fatalf("first request = %+v, want %+v", first, want)
	}

	second := result.Requests[1]
	if second.RequestID != 7 || second.Name != "申请者乙" || second.TargetType != 1 ||
		second.ReceiverName != "" || second.IsVIP {
		t.Fatalf("second request = %+v", second)
	}
}

func TestFriendRequestsEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, testAppendBytes(nil, 1, testStatusBytes(1, "success")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	result, err := client.FriendRequests(context.Background(), "tok-contacts")
	if err != nil {
		t.Fatalf("FriendRequests: %v", err)
	}
	if result == nil {
		t.Fatal("nil FriendRequests for empty list")
	}
	if len(result.Requests) != 0 || result.Total != 0 || result.Pending != 0 {
		t.Fatalf("result = %+v, want empty", result)
	}
}

func TestFriendRequestsNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, testAppendBytes(nil, 1, testStatusBytes(2, "未登录，请登录")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.FriendRequests(context.Background(), "bad-token")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestFriendRequestsMalformedProto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// field 2 (requests) declares 10 bytes but only 1 follows.
		writeProtoResp(w, []byte{0x12, 0x0a, 0x08})
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	result, err := client.FriendRequests(context.Background(), "tok-contacts")
	if err == nil {
		t.Fatalf("expected parse error, got %+v", result)
	}
	if !strings.Contains(err.Error(), "解析申请列表失败") {
		t.Fatalf("err = %v, want wrapped parse failure", err)
	}
}

// ---------------------------------------------------------------------------
// display helpers

func TestContactDisplayName(t *testing.T) {
	cases := []struct {
		contact Contact
		want    string
	}{
		{Contact{ID: "u1", Remark: "备注", Name: "真名"}, "备注"},
		{Contact{ID: "u1", Remark: "   ", Name: "真名"}, "真名"},
		{Contact{ID: "u1"}, "u1"},
		{Contact{ID: "u1", Remark: "  备注  ", Name: "真名"}, "备注"},
	}
	for _, tc := range cases {
		if got := tc.contact.DisplayName(); got != tc.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", tc.contact, got, tc.want)
		}
	}
}
