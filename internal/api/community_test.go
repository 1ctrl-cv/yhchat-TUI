package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// communityTestToken is a fake token; the tests never talk to a real server.
const communityTestToken = "test-token-community"

// ---------------------------------------------------------------------------
// fixtures (shapes copied from yhchatAPI/src/api/v1/community.md)

const communityListJSON = `{
  "code": 1,
  "msg": "success",
  "data": {
    "ba": [
      {
        "id": 41,
        "name": "云湖",
        "avatar": "https://chat-img.example.com/ba41.webp",
        "delTime": 0,
        "createTime": 1665233353,
        "lastActive": 1754474179,
        "memberNum": 1053,
        "postNum": 9656,
        "groupNum": 55,
        "createTimeText": "2022-10-08 20:49:13",
        "isFollowed": "1"
      },
      {
        "id": 42,
        "name": "测试分区",
        "avatar": "https://chat-img.example.com/ba42.webp",
        "delTime": 0,
        "createTime": 1675671165,
        "lastActive": 1755075037,
        "memberNum": 7,
        "postNum": 3,
        "groupNum": 1,
        "createTimeText": "2023-02-06 15:52:45",
        "isFollowed": 0
      }
    ],
    "total": 11
  }
}`

const communityInfoJSON = `{
  "code": 1,
  "msg": "success",
  "data": {
    "ba": {
      "id": 41,
      "name": "云湖",
      "avatar": "https://chat-img.example.com/ba41.webp",
      "delTime": 0,
      "createTime": 1665233353,
      "lastActive": 1754474179,
      "memberNum": 1053,
      "postNum": 9656,
      "groupNum": 55,
      "createTimeText": "2022-10-08 20:49:13",
      "isFollowed": "1"
    }
  }
}`

const postListJSON = `{
  "code": 1,
  "msg": "success",
  "data": {
    "posts": [
      {
        "id": 31622,
        "baId": 41,
        "senderId": "7384288",
        "senderNicknameId": 123,
        "senderAvatarId": 84,
        "groupId": "554470989",
        "title": "文章标题",
        "contentType": 2,
        "content": "文章内容部分预览",
        "delTime": 0,
        "createTime": 1754382613,
        "updateTime": 0,
        "editTime": 1754382741,
        "lastActive": 1754474179,
        "likeNum": 2,
        "commentNum": 4,
        "collectNum": 6,
        "amountNum": 22.01,
        "isSticky": 0,
        "senderNickname": "文章作者",
        "senderAvatar": "https://chat-img.example.com/avatar.png",
        "createTimeText": "2025-08-05 16:30:13",
        "isLiked": "1",
        "isCollected": 0,
        "isReward": 0,
        "isVip": 1
      }
    ],
    "total": 360
  }
}`

// postDetailJSON intentionally encodes "isLiked" as a number: community.md shows
// that form for post-detail while post-list uses a string, and both must decode.
const postDetailJSON = `{
  "code": 1,
  "msg": "success",
  "data": {
    "ba": {
      "id": 41,
      "name": "云湖",
      "avatar": "https://chat-img.example.com/ba41.webp",
      "delTime": 0,
      "createTime": 1665233353,
      "lastActive": 1757794419,
      "memberNum": 1118,
      "postNum": 9752,
      "groupNum": 64,
      "createTimeText": "2022-10-08 20:49:13",
      "isFollowed": "0"
    },
    "isAdmin": 1,
    "post": {
      "id": 31622,
      "baId": 41,
      "senderId": "7384288",
      "senderNicknameId": 123,
      "senderAvatarId": 123,
      "groupId": "",
      "title": "文章标题",
      "contentType": 2,
      "content": "文章内容",
      "delTime": 0,
      "createTime": 1757755754,
      "updateTime": 0,
      "editTime": 1757755828,
      "lastActive": 1757755828,
      "likeNum": 0,
      "commentNum": 0,
      "collectNum": 0,
      "amountNum": 0,
      "isSticky": 0,
      "senderNickname": "作者名称",
      "senderAvatar": "https://chat-img.example.com/author.png",
      "createTimeText": "2025-09-13 17:29:14",
      "isLiked": 0,
      "isCollected": 0,
      "isReward": 0,
      "isVip": 0
    }
  }
}`

const commentListJSON = `{
  "code": 1,
  "msg": "success",
  "data": {
    "comments": [
      {
        "id": 123,
        "postId": 31153,
        "parentId": 0,
        "senderId": "114514",
        "content": "？",
        "createTime": 1754746982,
        "likeNum": 0,
        "repliesNum": 2,
        "amountNum": 0,
        "senderNickname": "测试评论发送者名称",
        "senderAvatar": "https://chat-img.example.com/commenter.png",
        "createTimeText": "2025-08-09 21:43:02",
        "isLiked": "0",
        "isVip": 1,
        "replies": [
          {
            "id": 125,
            "postId": 31153,
            "parentId": 123,
            "senderId": "114516",
            "content": "楼中楼回复",
            "createTime": 1754746995,
            "isLiked": 0,
            "isVip": 0
          }
        ]
      },
      {
        "id": 124,
        "postId": 31153,
        "parentId": 123,
        "senderId": "114515",
        "content": "回复内容",
        "createTime": 1754746990,
        "isLiked": 1,
        "isVip": 0
      }
    ],
    "isAdmin": 0,
    "total": 3
  }
}`

// ---------------------------------------------------------------------------
// helpers

// assertCommunityRequest checks the request shape shared by every community
// endpoint. It is safe to call from an HTTP handler goroutine (t.Errorf only).
func assertCommunityRequest(t *testing.T, r *http.Request, wantPath, wantToken string) bool {
	t.Helper()
	ok := true
	if r.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", r.Method)
		ok = false
	}
	if r.URL.Path != wantPath {
		t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		ok = false
	}
	if got := r.Header.Get("token"); got != wantToken {
		t.Errorf("token header = %q, want %q", got, wantToken)
		ok = false
	}
	if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("content-type = %q, want application/json", got)
		ok = false
	}
	return ok
}

// decodeCommunityRequestBody decodes a JSON request body inside an HTTP
// handler. handler goroutines must not call t.Fatalf, hence t.Errorf here.
func decodeCommunityRequestBody(t *testing.T, r *http.Request, dst any) bool {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("读取请求体失败: %v", err)
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Errorf("解析请求体 %q 失败: %v", string(raw), err)
		return false
	}
	return true
}

// communityEndpointCalls builds one call per community endpoint, used by the
// failure-path tests below.
func communityEndpointCalls(client *Client) map[string]func() error {
	ctx := context.Background()
	const badToken = "bad-token"
	return map[string]func() error{
		"Communities": func() error {
			_, _, err := client.Communities(ctx, badToken, 2, 1, 20)
			return err
		},
		"CommunityInfo": func() error {
			_, err := client.CommunityInfo(ctx, badToken, 41)
			return err
		},
		"Posts": func() error {
			_, _, err := client.Posts(ctx, badToken, 41, 1, 20)
			return err
		},
		"PostDetail": func() error {
			_, err := client.PostDetail(ctx, badToken, 31622)
			return err
		},

		"Comments": func() error {
			_, _, err := client.Comments(ctx, badToken, 31153, 1, 10)
			return err
		},
	}
}

// ---------------------------------------------------------------------------
// success paths

func TestCommunities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertCommunityRequest(t, r, "/v1/community/ba/following-ba-list", communityTestToken) {
			writeJSONResp(w, 200, `{"code":-1,"msg":"bad request"}`)
			return
		}
		var req communityListRequest
		if decodeCommunityRequestBody(t, r, &req) && (req.Typ != 2 || req.Size != 10 || req.Page != 3) {
			t.Errorf("unexpected request body: %+v", req)
		}
		writeJSONResp(w, 200, communityListJSON)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	communities, total, err := client.Communities(context.Background(), communityTestToken, 2, 3, 10)
	if err != nil {
		t.Fatalf("Communities: %v", err)
	}
	if total != 11 {
		t.Fatalf("total = %d, want 11", total)
	}
	if len(communities) != 2 {
		t.Fatalf("communities = %d, want 2", len(communities))
	}
	first := communities[0]
	if first.ID != 41 || first.Name != "云湖" ||
		first.Avatar != "https://chat-img.example.com/ba41.webp" ||
		first.DelTime != 0 || first.CreateTime != 1665233353 || first.LastActive != 1754474179 ||
		first.MemberNum != 1053 || first.PostNum != 9656 || first.GroupNum != 55 ||
		first.CreateTimeText != "2022-10-08 20:49:13" || first.IsFollowed != "1" {
		t.Fatalf("unexpected first community: %+v", first)
	}
	second := communities[1]
	if second.ID != 42 || second.Name != "测试分区" {
		t.Fatalf("unexpected second community: %+v", second)
	}
	// The numeric form 0 must be normalised to the string form "0".
	if second.IsFollowed != "0" {
		t.Fatalf("second isFollowed = %q, want %q", second.IsFollowed, "0")
	}
}

func TestCommunitiesPaginationNormalized(t *testing.T) {
	cases := []struct {
		name     string
		page     int
		size     int
		wantPage int
		wantSize int
	}{
		{"zero values use defaults", 0, 0, 1, communityDefaultPageSize},
		{"negative page and oversized size are clamped", -5, 999, 1, communityMaxPageSize},
		{"valid values pass through", 2, 10, 2, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			type paging struct {
				page int
				size int
			}
			requests := make(chan paging, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req communityListRequest
				if !decodeCommunityRequestBody(t, r, &req) {
					writeJSONResp(w, 200, `{"code":-1,"msg":"bad request"}`)
					return
				}
				requests <- paging{page: req.Page, size: req.Size}
				writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"ba":[],"total":0}}`)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			communities, total, err := client.Communities(context.Background(), communityTestToken, 1, tc.page, tc.size)
			if err != nil {
				t.Fatalf("Communities: %v", err)
			}
			if len(communities) != 0 || total != 0 {
				t.Fatalf("communities = %d, total = %d, want 0/0", len(communities), total)
			}
			got := <-requests
			if got.page != tc.wantPage || got.size != tc.wantSize {
				t.Fatalf("request page/size = %d/%d, want %d/%d", got.page, got.size, tc.wantPage, tc.wantSize)
			}
		})
	}
}

func TestCommunityInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertCommunityRequest(t, r, "/v1/community/ba/info", communityTestToken) {
			writeJSONResp(w, 200, `{"code":-1,"msg":"bad request"}`)
			return
		}
		var req communityIDRequest
		if decodeCommunityRequestBody(t, r, &req) && req.ID != 41 {
			t.Errorf("request id = %d, want 41", req.ID)
		}
		writeJSONResp(w, 200, communityInfoJSON)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	community, err := client.CommunityInfo(context.Background(), communityTestToken, 41)
	if err != nil {
		t.Fatalf("CommunityInfo: %v", err)
	}
	if community.ID != 41 || community.Name != "云湖" || community.MemberNum != 1053 ||
		community.PostNum != 9656 || community.GroupNum != 55 ||
		community.CreateTimeText != "2022-10-08 20:49:13" || community.IsFollowed != "1" {
		t.Fatalf("unexpected community: %+v", community)
	}
}

func TestPosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertCommunityRequest(t, r, "/v1/community/posts/post-list", communityTestToken) {
			writeJSONResp(w, 200, `{"code":-1,"msg":"bad request"}`)
			return
		}
		var req postListRequest
		if decodeCommunityRequestBody(t, r, &req) &&
			(req.Typ != 1 || req.BaID != 41 || req.Size != 20 || req.Page != 1) {
			t.Errorf("unexpected request body: %+v", req)
		}
		writeJSONResp(w, 200, postListJSON)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	posts, total, err := client.Posts(context.Background(), communityTestToken, 41, 1, 20)
	if err != nil {
		t.Fatalf("Posts: %v", err)
	}
	if total != 360 {
		t.Fatalf("total = %d, want 360", total)
	}
	if len(posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(posts))
	}
	post := posts[0]
	if post.ID != 31622 || post.BaID != 41 || post.SenderID != "7384288" ||
		post.SenderNicknameID != 123 || post.SenderAvatarID != 84 || post.GroupID != "554470989" ||
		post.Title != "文章标题" || post.ContentType != 2 || post.Content != "文章内容部分预览" ||
		post.CreateTime != 1754382613 || post.EditTime != 1754382741 || post.LastActive != 1754474179 ||
		post.LikeNum != 2 || post.CommentNum != 4 || post.CollectNum != 6 || post.AmountNum != 22.01 ||
		post.SenderNickname != "文章作者" || post.CreateTimeText != "2025-08-05 16:30:13" ||
		post.IsLiked != "1" || post.IsVip != 1 {
		t.Fatalf("unexpected post: %+v", post)
	}
}

func TestPostDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertCommunityRequest(t, r, "/v1/community/posts/post-detail", communityTestToken) {
			writeJSONResp(w, 200, `{"code":-1,"msg":"bad request"}`)
			return
		}
		var req communityIDRequest
		if decodeCommunityRequestBody(t, r, &req) && req.ID != 31622 {
			t.Errorf("request id = %d, want 31622", req.ID)
		}
		writeJSONResp(w, 200, postDetailJSON)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	detail, err := client.PostDetail(context.Background(), communityTestToken, 31622)
	if err != nil {
		t.Fatalf("PostDetail: %v", err)
	}
	if detail.IsAdmin != 1 {
		t.Fatalf("isAdmin = %d, want 1", detail.IsAdmin)
	}
	if detail.Ba.ID != 41 || detail.Ba.Name != "云湖" || detail.Ba.IsFollowed != "0" {
		t.Fatalf("unexpected ba: %+v", detail.Ba)
	}
	post := detail.Post
	if post.ID != 31622 || post.BaID != 41 || post.Title != "文章标题" ||
		post.ContentType != 2 || post.CreateTime != 1757755754 || post.EditTime != 1757755828 ||
		post.SenderNickname != "作者名称" || post.CreateTimeText != "2025-09-13 17:29:14" {
		t.Fatalf("unexpected post: %+v", post)
	}
	// The numeric form 0 must be normalised to the string form "0".
	if post.IsLiked != "0" {
		t.Fatalf("isLiked = %q, want %q", post.IsLiked, "0")
	}
}

func TestComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertCommunityRequest(t, r, "/v1/community/comment/comment-list", communityTestToken) {
			writeJSONResp(w, 200, `{"code":-1,"msg":"bad request"}`)
			return
		}
		var req commentListRequest
		if decodeCommunityRequestBody(t, r, &req) &&
			(req.PostID != 31153 || req.Size != 10 || req.Page != 1) {
			t.Errorf("unexpected request body: %+v", req)
		}
		writeJSONResp(w, 200, commentListJSON)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	comments, total, err := client.Comments(context.Background(), communityTestToken, 31153, 1, 10)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(comments))
	}
	comment := comments[0]
	if comment.ID != 123 || comment.PostID != 31153 || comment.ParentID != 0 ||
		comment.SenderID != "114514" || comment.Content != "？" ||
		comment.CreateTime != 1754746982 || comment.RepliesNum != 2 ||
		comment.SenderNickname != "测试评论发送者名称" ||
		comment.CreateTimeText != "2025-08-09 21:43:02" ||
		comment.IsLiked != "0" || comment.IsVip != 1 {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	numericLiked := comments[1]
	if numericLiked.ID != 124 || numericLiked.ParentID != 123 {
		t.Fatalf("unexpected reply comment: %+v", numericLiked)
	}
	// The numeric form 1 must be normalised to the string form "1".
	if numericLiked.IsLiked != "1" {
		t.Fatalf("numeric reply isLiked = %q, want %q", numericLiked.IsLiked, "1")
	}
	if len(comment.Replies) != 1 || comment.Replies[0].ID != 125 || comment.Replies[0].ParentID != 123 {
		t.Fatalf("nested replies = %+v", comment.Replies)
	}
}

func TestPostLikeCollectAndCreateComment(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertCommunityRequest(t, r, r.URL.Path, communityTestToken) {
			writeJSONResp(w, 200, `{"code":-1,"msg":"bad request"}`)
			return
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/community/posts/post-like", "/v1/community/posts/post-collect":
			var req communityIDRequest
			if decodeCommunityRequestBody(t, r, &req) && req.ID != 31622 {
				t.Errorf("%s id = %d, want 31622", r.URL.Path, req.ID)
			}
		case "/v1/community/comment/comment":
			var req commentCreateRequest
			if decodeCommunityRequestBody(t, r, &req) {
				if req.PostID != 31622 || req.Content == "" {
					t.Errorf("comment request = %+v", req)
				}
				switch req.CommentID {
				case 0:
					if req.Content != "顶层评论" {
						t.Errorf("top-level content = %q", req.Content)
					}
				case 123:
					if req.Content != "楼中楼" {
						t.Errorf("reply content = %q", req.Content)
					}
				default:
					t.Errorf("commentID = %d", req.CommentID)
				}
			}
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	ctx := context.Background()
	if err := client.LikePost(ctx, communityTestToken, 31622); err != nil {
		t.Fatalf("LikePost: %v", err)
	}
	if err := client.CollectPost(ctx, communityTestToken, 31622); err != nil {
		t.Fatalf("CollectPost: %v", err)
	}
	if err := client.CreateComment(ctx, communityTestToken, 31622, 0, "  顶层评论  "); err != nil {
		t.Fatalf("CreateComment top: %v", err)
	}
	if err := client.CreateComment(ctx, communityTestToken, 31622, 123, "楼中楼"); err != nil {
		t.Fatalf("CreateComment reply: %v", err)
	}
	if len(paths) != 4 {
		t.Fatalf("requests = %v", paths)
	}
	if err := client.CreateComment(ctx, communityTestToken, 31622, 0, "   "); err == nil {
		t.Fatal("empty comment should be rejected")
	}
	if err := client.LikePost(ctx, communityTestToken, 0); err == nil {
		t.Fatal("invalid post id should be rejected")
	}
}

// communityActionCalls builds one call per write endpoint (like/collect/comment).
func communityActionCalls(client *Client) map[string]func() error {
	ctx := context.Background()
	const badToken = "bad-token"
	return map[string]func() error{
		"LikePost": func() error {
			return client.LikePost(ctx, badToken, 31622)
		},
		"CollectPost": func() error {
			return client.CollectPost(ctx, badToken, 31622)
		},
		"CreateComment": func() error {
			return client.CreateComment(ctx, badToken, 31622, 0, "hi")
		},
	}
}

// ---------------------------------------------------------------------------
// failure paths

func TestCommunityNotLoggedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-101,"msg":"未登录"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	calls := communityEndpointCalls(client)
	for name, call := range communityActionCalls(client) {
		calls[name] = call
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected error")
			}
			if !IsSessionExpired(err) {
				t.Fatalf("expected session expired error, got %v", err)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != -101 {
				t.Fatalf("expected *APIError{Code:-101}, got %T: %v", err, err)
			}
		})
	}
}

func TestCommunityEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	for name, call := range communityEndpointCalls(client) {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected error for empty data")
			}
			if !strings.Contains(err.Error(), "为空") {
				t.Fatalf("expected empty-data error, got %v", err)
			}
			if !strings.Contains(FriendlyMessage(err), "为空") {
				t.Fatalf("friendly message = %q", FriendlyMessage(err))
			}
		})
	}
}

func TestCommunityMissingPayload(t *testing.T) {
	cases := []struct {
		name string
		body string
		call func(client *Client) error
		want string
	}{
		{
			name: "post detail empty object",
			body: `{"code":1,"msg":"success","data":{}}`,
			call: func(client *Client) error {
				_, err := client.PostDetail(context.Background(), communityTestToken, 31622)
				return err
			},
			want: "文章详情为空",
		},
		{
			name: "community info null ba",
			body: `{"code":1,"msg":"success","data":{"ba":null}}`,
			call: func(client *Client) error {
				_, err := client.CommunityInfo(context.Background(), communityTestToken, 41)
				return err
			},
			want: "分区信息为空",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSONResp(w, 200, tc.body)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			err := tc.call(client)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestCommunityMalformedData(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
		call func(client *Client) error
	}{
		{
			name: "comments is not a list",
			body: `{"code":1,"msg":"success","data":{"comments":"not-an-array"}}`,
			want: "解析评论列表失败",
			call: func(client *Client) error {
				_, _, err := client.Comments(context.Background(), communityTestToken, 31153, 1, 10)
				return err
			},
		},
		{
			name: "unsupported isLiked shape",
			body: `{"code":1,"msg":"success","data":{"posts":[{"id":1,"isLiked":{"bad":true}}],"total":1}}`,
			want: "解析文章列表失败",
			call: func(client *Client) error {
				_, _, err := client.Posts(context.Background(), communityTestToken, 41, 1, 20)
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSONResp(w, 200, tc.body)
			}))
			defer srv.Close()

			client := newTestClient(t)
			client.BaseURL = srv.URL
			err := tc.call(client)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestCommunityTransportErrors(t *testing.T) {
	t.Run("non JSON response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>bad gateway</html>")
		}))
		defer srv.Close()

		client := newTestClient(t)
		client.BaseURL = srv.URL
		_, _, err := client.Communities(context.Background(), communityTestToken, 2, 1, 20)
		if err == nil {
			t.Fatal("expected error")
		}
		if msg := FriendlyMessage(err); !strings.Contains(msg, "无法解析服务器响应") {
			t.Fatalf("friendly message = %q", msg)
		}
	})

	t.Run("HTTP 502", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer srv.Close()

		client := newTestClient(t)
		client.BaseURL = srv.URL
		_, err := client.CommunityInfo(context.Background(), communityTestToken, 41)
		if err == nil {
			t.Fatal("expected error")
		}
		if msg := FriendlyMessage(err); !strings.Contains(msg, "HTTP 502") {
			t.Fatalf("friendly message = %q", msg)
		}
	})

	t.Run("API error is passed through", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSONResp(w, 200, `{"code":-5,"msg":"分区不存在"}`)
		}))
		defer srv.Close()

		client := newTestClient(t)
		client.BaseURL = srv.URL
		_, _, err := client.Posts(context.Background(), communityTestToken, 999, 1, 20)
		if err == nil {
			t.Fatal("expected error")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != -5 || apiErr.Msg != "分区不存在" {
			t.Fatalf("unexpected error: %T %v", err, err)
		}
		if msg := FriendlyMessage(err); msg != "分区不存在" {
			t.Fatalf("friendly message = %q", msg)
		}
	})
}
