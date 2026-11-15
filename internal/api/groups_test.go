package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// protobuf fixtures for the group endpoints
//
// The shared testAppend*/testStatusBytes/newTestClient/writeProtoResp/
// writeJSONResp/parseRequestField* helpers already live in chat_test.go,
// contacts_test.go and client_test.go; only group-specific builders are here.

// groupsTestGroupData builds a GroupData message with every field the TUI reads.
// Unknown fields (tag_old=26, tag=27, field31=31, auto_delete_message=32,
// deny_members_upload_to_group_disk=33) are included on purpose to exercise
// unknown-field skipping.
func groupsTestGroupData() []byte {
	var b []byte
	b = testAppendString(b, 1, "g-2001")                          // group_id
	b = testAppendString(b, 2, "测试群")                             // name
	b = testAppendString(b, 3, "https://img.example/g.png")       // avatar_url
	b = testAppendVarint(b, 4, 43821)                             // avatar_id
	b = testAppendString(b, 5, "群简介")                             // introduction
	b = testAppendVarint(b, 6, 16)                                // headcount
	b = testAppendString(b, 7, "u-owner")                         // create_by
	b = testAppendVarint(b, 8, 1)                                 // direct_join
	b = testAppendInt32(b, 9, 100)                                // permisson_level
	b = testAppendVarint(b, 10, 1)                                // history_msg
	b = testAppendString(b, 11, "技术-IT/互联网")                      // category_name
	b = testAppendVarint(b, 12, 22)                               // category_id
	b = testAppendVarint(b, 13, 1)                                // private
	b = testAppendVarint(b, 14, 1)                                // do_not_disturb
	b = testAppendVarint(b, 15, 41)                               // community_id
	b = testAppendString(b, 16, "云湖社区")                           // community_name
	b = testAppendVarint(b, 17, 778)                              // ban_id
	b = testAppendVarint(b, 18, 1754113069)                       // ban_until_timestamp
	b = testAppendVarint(b, 19, 1)                                // top
	b = testAppendString(b, 20, "u-admin-1")                      // admin[0]
	b = testAppendString(b, 20, "u-admin-2")                      // admin[1]
	b = testAppendVarint(b, 21, ^uint64(0))                       // gag_until_timestamp = -1
	b = testAppendString(b, 22, "1,2,3")                          // limited_msg_type
	b = testAppendString(b, 23, "u-owner")                        // owner
	b = testAppendVarint(b, 24, 1)                                // recommandation
	b = testAppendVarint(b, 25, 1)                                // is_gag
	b = testAppendString(b, 26, "旧标签")                            // tag_old: skipped
	b = testAppendBytes(b, 27, testAppendString(nil, 1, "tag-1")) // tag: skipped
	b = testAppendString(b, 28, "我的群昵称")                          // my_group_nickname
	b = testAppendString(b, 29, "群口令")                            // group_code
	b = testAppendVarint(b, 30, 1)                                // hide_group_members
	b = testAppendVarint(b, 31, 31)                               // field31: skipped
	b = testAppendVarint(b, 32, 1)                                // auto_delete_message: skipped
	b = testAppendVarint(b, 33, 1)                                // deny_members_upload_to_group_disk: skipped
	b = testAppendString(b, 34, "封禁原因")                           // ban_reason
	return b
}

// groupsTestGroupInfoResponse builds a successful GroupInfoResponse. The bot
// list (field 3) is present but must be ignored by GroupInfo.
func groupsTestGroupInfoResponse() []byte {
	var bot []byte
	bot = testAppendString(bot, 1, "bot-1") // bot_id
	bot = testAppendString(bot, 2, "群助手")   // name

	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(1, "success")) // status
	out = testAppendBytes(out, 2, groupsTestGroupData())         // data
	out = testAppendBytes(out, 3, bot)                           // bot: not used
	out = testAppendVarint(out, 9, 9)                            // unknown top-level field
	return out
}

// groupsTestListMemberResponse builds a successful ListMemberResponse with two
// members: the group owner (permission 100, VIP, not gagged) and an admin
// (permission 2, gagged). Unknown fields are included inside UserInfo (3), User
// (6) and the response (9).
func groupsTestListMemberResponse() []byte {
	ownerInfo := []byte{}
	ownerInfo = testAppendString(ownerInfo, 1, "u-1001")                     // user_id
	ownerInfo = testAppendString(ownerInfo, 2, "小明")                         // name
	ownerInfo = testAppendString(ownerInfo, 3, "unknown-user-info-field")    // unknown
	ownerInfo = testAppendString(ownerInfo, 4, "https://img.example/u1.png") // avatar_url
	ownerInfo = testAppendVarint(ownerInfo, 6, 1)                            // is_vip

	owner := []byte{}
	owner = testAppendString(owner, 1, "g-2001")               // group_id
	owner = testAppendBytes(owner, 2, ownerInfo)               // user_info
	owner = testAppendInt32(owner, 3, 100)                     // permission_level
	owner = testAppendString(owner, 6, "unknown-member-field") // unknown

	adminInfo := []byte{}
	adminInfo = testAppendString(adminInfo, 1, "u-1002") // user_id
	adminInfo = testAppendString(adminInfo, 2, "小红")     // name

	admin := []byte{}
	admin = testAppendString(admin, 1, "g-2001")   // group_id
	admin = testAppendBytes(admin, 2, adminInfo)   // user_info
	admin = testAppendInt32(admin, 3, 2)           // permission_level
	admin = testAppendVarint(admin, 4, 1754113069) // gag_timestamp
	admin = testAppendVarint(admin, 5, 1)          // is_gag

	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(1, "success"))
	out = testAppendBytes(out, 2, owner)
	out = testAppendBytes(out, 2, admin)
	out = testAppendVarint(out, 3, 2) // total
	out = testAppendVarint(out, 9, 9) // unknown top-level field
	return out
}

// ---------------------------------------------------------------------------
// GroupInfo

func TestGroupInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/group/info" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-groups" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-protobuf" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		// GroupInfoRequest{group_id=2}; the caller value is trimmed.
		if got := string(parseRequestFieldBytes(t, body, 2)); got != "g-2001" {
			t.Errorf("GroupInfoRequest.group_id = %q", got)
		}
		writeProtoResp(w, groupsTestGroupInfoResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	info, err := client.GroupInfo(context.Background(), "tok-groups", "  g-2001  ")
	if err != nil {
		t.Fatalf("GroupInfo: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"GroupID", info.GroupID, "g-2001"},
		{"Name", info.Name, "测试群"},
		{"AvatarURL", info.AvatarURL, "https://img.example/g.png"},
		{"AvatarID", info.AvatarID, uint64(43821)},
		{"Introduction", info.Introduction, "群简介"},
		{"Headcount", info.Headcount, uint64(16)},
		{"CreateBy", info.CreateBy, "u-owner"},
		{"DirectJoin", info.DirectJoin, true},
		{"PermissionLevel", info.PermissionLevel, int32(100)},
		{"HistoryMsg", info.HistoryMsg, true},
		{"CategoryName", info.CategoryName, "技术-IT/互联网"},
		{"CategoryID", info.CategoryID, uint64(22)},
		{"Private", info.Private, true},
		{"DoNotDisturb", info.DoNotDisturb, true},
		{"CommunityID", info.CommunityID, uint64(41)},
		{"CommunityName", info.CommunityName, "云湖社区"},
		{"BanID", info.BanID, int64(778)},
		{"BanUntilTimestamp", info.BanUntilTimestamp, int64(1754113069)},
		{"Top", info.Top, true},
		{"GagUntilTimestamp", info.GagUntilTimestamp, int64(-1)},
		{"LimitedMsgType", info.LimitedMsgType, "1,2,3"},
		{"Owner", info.Owner, "u-owner"},
		{"Recommendation", info.Recommendation, true},
		{"IsGag", info.IsGag, true},
		{"MyGroupNickname", info.MyGroupNickname, "我的群昵称"},
		{"GroupCode", info.GroupCode, "群口令"},
		{"HideGroupMembers", info.HideGroupMembers, true},
		{"BanReason", info.BanReason, "封禁原因"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("GroupInfo.%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if len(info.AdminIDs) != 2 || info.AdminIDs[0] != "u-admin-1" || info.AdminIDs[1] != "u-admin-2" {
		t.Errorf("GroupInfo.AdminIDs = %v", info.AdminIDs)
	}
}

// An error response carries a status code but no data: it must surface as an
// *APIError instead of a partially filled GroupInfo.
func TestGroupInfoNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, testAppendBytes(nil, 1, testStatusBytes(2, "未登录，请登录")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	info, err := client.GroupInfo(context.Background(), "bad-token", "g-2001")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got info=%+v err=%v", info, err)
	}
}

// A JSON envelope error (missing token) is translated by postProto.
func TestGroupInfoJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-101,"msg":"未登录"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.GroupInfo(context.Background(), "bad-token", "g-2001")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
	if !strings.Contains(err.Error(), "未登录") {
		t.Fatalf("err = %v", err)
	}
}

func TestGroupInfoEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, testAppendBytes(nil, 1, testStatusBytes(1, "success")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	info, err := client.GroupInfo(context.Background(), "tok-groups", "g-2001")
	if err == nil || info != nil {
		t.Fatalf("expected empty group info error, got info=%+v err=%v", info, err)
	}
	if !strings.Contains(err.Error(), "群信息为空") {
		t.Fatalf("err = %v", err)
	}
}

func TestGroupInfoRejectsEmptyGroupID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		writeProtoResp(w, groupsTestGroupInfoResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if _, err := client.GroupInfo(context.Background(), "tok-groups", "   "); err == nil ||
		!strings.Contains(err.Error(), "群聊 ID 不能为空") {
		t.Fatalf("err = %v", err)
	}
}

func TestGroupInfoMalformedProto(t *testing.T) {
	truncatedData := testAppendBytes(nil, 1, testStatusBytes(1, "success"))
	truncatedData = append(truncatedData, 0x12, 0x05, 0x08) // data: length 5, one byte

	cases := map[string][]byte{
		"empty body":       {},
		"invalid tag":      {0x00},
		"truncated status": {0x0a, 0x05, 0x08},
		"truncated data":   truncatedData,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeProtoResp(w, payload)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			info, err := client.GroupInfo(context.Background(), "tok-groups", "g-2001")
			if err == nil || info != nil {
				t.Fatalf("expected parse error, got info=%+v err=%v", info, err)
			}
			if !strings.Contains(err.Error(), "解析群信息失败") {
				t.Fatalf("err = %v, want wrapped parse failure", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GroupMembers

func TestGroupMembers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/group/list-member" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-groups" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-protobuf" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		// ListMemberRequest{data={size=1,page=2}, group_id=3, keywords=4}.
		data := parseRequestFieldBytes(t, body, 2)
		if got := parseRequestFieldVarint(t, data, 1); got != 100 {
			t.Errorf("ListMemberRequest.data.size = %d, want 100", got)
		}
		if got := parseRequestFieldVarint(t, data, 2); got != 2 {
			t.Errorf("ListMemberRequest.data.page = %d, want 2", got)
		}
		if got := string(parseRequestFieldBytes(t, body, 3)); got != "g-2001" {
			t.Errorf("ListMemberRequest.group_id = %q", got)
		}
		if got := string(parseRequestFieldBytes(t, body, 4)); got != "小明" {
			t.Errorf("ListMemberRequest.keywords = %q, want trimmed 小明", got)
		}
		writeProtoResp(w, groupsTestListMemberResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	members, total, err := client.GroupMembers(context.Background(), "tok-groups", "g-2001", 2, 100, " 小明 ")
	if err != nil {
		t.Fatalf("GroupMembers: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(members) != 2 {
		t.Fatalf("members = %d, want 2", len(members))
	}

	owner := members[0]
	if owner.GroupID != "g-2001" || owner.UserID != "u-1001" || owner.Name != "小明" ||
		owner.AvatarURL != "https://img.example/u1.png" || !owner.IsVIP ||
		owner.PermissionLevel != 100 || owner.GagTimestamp != 0 || owner.IsGag {
		t.Fatalf("unexpected owner member: %+v", owner)
	}

	admin := members[1]
	if admin.GroupID != "g-2001" || admin.UserID != "u-1002" || admin.Name != "小红" ||
		admin.AvatarURL != "" || admin.IsVIP || admin.PermissionLevel != 2 ||
		admin.GagTimestamp != 1754113069 || !admin.IsGag {
		t.Fatalf("unexpected admin member: %+v", admin)
	}
}

// Pages are 1-based, the size defaults to 50 and is capped at 100; an empty
// keywords filter is not encoded at all.
func TestGroupMembersPageNormalization(t *testing.T) {
	cases := []struct {
		name         string
		page, size   int
		keywords     string
		wantPage     uint64
		wantSize     uint64
		wantKeywords string
	}{
		{"defaults", 0, 0, "", 1, 50, ""},
		{"negative values", -3, -1, "", 1, 50, ""},
		{"size is capped", 5, 1000, "", 5, 100, ""},
		{"valid values are kept", 3, 30, "", 3, 30, ""},
		{"explicit maximum", 1, 100, "", 1, 100, ""},
		{"keywords are passed through", 2, 20, "小红", 2, 20, "小红"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				data := parseRequestFieldBytes(t, body, 2)
				if got := parseRequestFieldVarint(t, data, 1); got != tc.wantSize {
					t.Errorf("size = %d, want %d", got, tc.wantSize)
				}
				if got := parseRequestFieldVarint(t, data, 2); got != tc.wantPage {
					t.Errorf("page = %d, want %d", got, tc.wantPage)
				}
				keywords := parseRequestFieldBytes(t, body, 4)
				if tc.wantKeywords == "" {
					if keywords != nil {
						t.Errorf("keywords field = %q, want omitted", keywords)
					}
				} else if string(keywords) != tc.wantKeywords {
					t.Errorf("keywords = %q, want %q", keywords, tc.wantKeywords)
				}
				writeProtoResp(w, groupsTestListMemberResponse())
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			if _, _, err := client.GroupMembers(context.Background(), "tok-groups", "g-2001",
				tc.page, tc.size, tc.keywords); err != nil {
				t.Fatalf("GroupMembers: %v", err)
			}
		})
	}
}

func TestGroupMembersRejectsEmptyGroupID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		writeProtoResp(w, groupsTestListMemberResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	members, total, err := client.GroupMembers(context.Background(), "tok-groups", "", 1, 10, "")
	if err == nil || members != nil || total != 0 || !strings.Contains(err.Error(), "群聊 ID 不能为空") {
		t.Fatalf("members=%+v total=%d err=%v", members, total, err)
	}
}

func TestGroupMembersNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, testAppendBytes(nil, 1, testStatusBytes(2, "未登录，请登录")))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	members, total, err := client.GroupMembers(context.Background(), "bad-token", "g-2001", 1, 10, "")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got members=%+v total=%d err=%v", members, total, err)
	}
}

func TestGroupMembersMalformedProto(t *testing.T) {
	cases := map[string][]byte{
		"empty body":      {},
		"invalid tag":     {0x00},
		"truncated user":  {0x0a, 0x03, 0x08, 0x01, 0x10, 0x01, 0x12, 0x05, 0x08},
		"truncated total": {0x0a, 0x03, 0x08, 0x01, 0x10, 0x01, 0x18},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeProtoResp(w, payload)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			members, total, err := client.GroupMembers(context.Background(), "tok-groups", "g-2001", 1, 10, "")
			if err == nil || members != nil || total != 0 {
				t.Fatalf("expected parse error, got members=%+v total=%d err=%v", members, total, err)
			}
			if !strings.Contains(err.Error(), "解析群成员列表失败") {
				t.Fatalf("err = %v, want wrapped parse failure", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// friend / group / bot add-request handling (JSON)

func TestFriendRequestActions(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		wantBody string
		call     func(context.Context, *Client) error
	}{
		{
			name:     "agree",
			path:     "/v1/friend/agree-apply",
			wantBody: `{"id":123,"agree":2}`,
			call: func(ctx context.Context, c *Client) error {
				return c.AgreeFriendRequest(ctx, "tok-groups", 123, 2)
			},
		},
		{
			name:     "ignore",
			path:     "/v1/friend/ignore-apply",
			wantBody: `{"id":456,"ignore":1}`,
			call: func(ctx context.Context, c *Client) error {
				return c.IgnoreFriendRequest(ctx, "tok-groups", 456)
			},
		},
		{
			name:     "delete request",
			path:     "/v1/friend/delete-request",
			wantBody: `{"id":789}`,
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriendRequest(ctx, "tok-groups", 789)
			},
		},
		{
			name:     "delete user friend",
			path:     "/v1/friend/delete-friend",
			wantBody: `{"chatId":"u-1001","chatType":1}`,
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriend(ctx, "tok-groups", " u-1001 ", ChatTypeUser)
			},
		},
		{
			name:     "leave group",
			path:     "/v1/friend/delete-friend",
			wantBody: `{"chatId":"g-2001","chatType":2}`,
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriend(ctx, "tok-groups", "g-2001", ChatTypeGroup)
			},
		},
		{
			name:     "delete bot",
			path:     "/v1/friend/delete-friend",
			wantBody: `{"chatId":"bot-1","chatType":3}`,
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriend(ctx, "tok-groups", "bot-1", ChatTypeBot)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s, want %s", r.Method, r.URL.Path, tc.path)
				}
				if got := r.Header.Get("token"); got != "tok-groups" {
					t.Errorf("token header = %q", got)
				}
				if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Errorf("Content-Type = %q", got)
				}
				body, _ := io.ReadAll(r.Body)
				if got := string(body); got != tc.wantBody {
					t.Errorf("body = %s, want %s", got, tc.wantBody)
				}
				writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			if err := tc.call(context.Background(), client); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
		})
	}
}

// A non-1 JSON code must be surfaced as *APIError with its message preserved.
func TestFriendRequestAPIError(t *testing.T) {
	cases := []struct {
		name        string
		payload     string
		wantCode    int
		wantMsg     string
		wantExpired bool
	}{
		{
			name:     "business error",
			payload:  `{"code":-9,"msg":"您已添加过对方"}`,
			wantCode: -9,
			wantMsg:  "您已添加过对方",
		},
		{
			name:        "expired session",
			payload:     `{"code":-101,"msg":"未登录"}`,
			wantCode:    -101,
			wantMsg:     "未登录",
			wantExpired: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSONResp(w, 200, tc.payload)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			err := client.AgreeFriendRequest(context.Background(), "tok-groups", 123, 1)
			if err == nil {
				t.Fatal("expected error")
			}
			var apiErr *APIError
			if !asAPIError(err, &apiErr) {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}
			if apiErr.Code != tc.wantCode || apiErr.Msg != tc.wantMsg {
				t.Fatalf("unexpected APIError: %+v", apiErr)
			}
			if got := IsSessionExpired(err); got != tc.wantExpired {
				t.Fatalf("IsSessionExpired = %v, want %v", got, tc.wantExpired)
			}
			if got := FriendlyMessage(err); got != tc.wantMsg {
				t.Fatalf("FriendlyMessage = %q, want %q", got, tc.wantMsg)
			}
		})
	}
}

// Invalid arguments must be rejected locally, before any HTTP request is made.
func TestFriendRequestValidation(t *testing.T) {
	cases := []struct {
		name string
		call func(context.Context, *Client) error
		want string
	}{
		{
			name: "agree missing id",
			call: func(ctx context.Context, c *Client) error {
				return c.AgreeFriendRequest(ctx, "tok-groups", 0, 1)
			},
			want: "申请 ID 无效",
		},
		{
			name: "agree invalid choice low",
			call: func(ctx context.Context, c *Client) error {
				return c.AgreeFriendRequest(ctx, "tok-groups", 123, 0)
			},
			want: "处理类型必须是",
		},
		{
			name: "agree invalid choice high",
			call: func(ctx context.Context, c *Client) error {
				return c.AgreeFriendRequest(ctx, "tok-groups", 123, 5)
			},
			want: "处理类型必须是",
		},
		{
			name: "ignore missing id",
			call: func(ctx context.Context, c *Client) error {
				return c.IgnoreFriendRequest(ctx, "tok-groups", -1)
			},
			want: "申请 ID 无效",
		},
		{
			name: "delete request missing id",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriendRequest(ctx, "tok-groups", 0)
			},
			want: "申请 ID 无效",
		},
		{
			name: "delete friend empty id",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriend(ctx, "tok-groups", "  ", ChatTypeUser)
			},
			want: "对象 ID 不能为空",
		},
		{
			name: "delete friend unknown type",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriend(ctx, "tok-groups", "u-1001", ChatTypeUnknown)
			},
			want: "对象类型必须是",
		},
		{
			name: "delete friend invalid type",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteFriend(ctx, "tok-groups", "u-1001", ChatType(9))
			},
			want: "对象类型必须是",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			err := tc.call(context.Background(), client)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
