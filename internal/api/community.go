package api

// Community (社区) API layer.
//
// The community endpoints are plain JSON (unlike most of the chat API, which
// speaks protobuf) and are authenticated with the "token" request header:
//
//	POST /v1/community/ba/following-ba-list   分区列表
//	POST /v1/community/ba/info                分区信息
//	POST /v1/community/posts/post-list        文章列表
//	POST /v1/community/posts/post-detail      文章详情
//	POST /v1/community/comment/comment-list   文章评论列表
//
// Requests and responses go through postJSON, so the shared {code,msg,data}
// envelope is handled there: a non-1 code is returned as *APIError before the
// endpoint-specific data is decoded here.
//
// Reference: yhchatAPI/src/api/v1/community.md.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// communityDefaultPageSize is used when the caller passes a non-positive
	// page size. Pages are 1-based for every community endpoint.
	communityDefaultPageSize = 20
	// communityMaxPageSize caps page sizes; community.md documents 20 as the
	// usual value and the endpoints reject oversized pages.
	communityMaxPageSize = 50
	// communityPostListTyp is the "typical" typ value for the post list
	// endpoint (community.md: "typ": 1, // 典型值).
	communityPostListTyp = int32(1)
)

// Community is one article section (分区) of the community.
type Community struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Avatar         string `json:"avatar"`
	DelTime        int64  `json:"delTime"`
	CreateTime     int64  `json:"createTime"`
	LastActive     int64  `json:"lastActive"`
	MemberNum      int64  `json:"memberNum"`
	PostNum        int64  `json:"postNum"`
	GroupNum       int64  `json:"groupNum"`
	CreateTimeText string `json:"createTimeText"`
	IsFollowed     string `json:"isFollowed"` // "1" 已关注 / "0" 未关注
}

// UnmarshalJSON decodes a Community with the same flag tolerance as Post and
// Comment: a 0/1 flag documented as a string is also accepted in its numeric
// JSON form.
func (c *Community) UnmarshalJSON(data []byte) error {
	type plain Community
	aux := struct {
		*plain
		IsFollowed json.RawMessage `json:"isFollowed"`
	}{plain: (*plain)(c)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	followed, err := flexibleFlagString(aux.IsFollowed)
	if err != nil {
		return fmt.Errorf("isFollowed: %w", err)
	}
	c.IsFollowed = followed
	return nil
}

// Post is one community article.
type Post struct {
	ID               int64   `json:"id"`
	BaID             int64   `json:"baId"`
	SenderID         string  `json:"senderId"`
	SenderNicknameID int64   `json:"senderNicknameId"`
	SenderAvatarID   int64   `json:"senderAvatarId"`
	GroupID          string  `json:"groupId"`
	Title            string  `json:"title"`
	ContentType      int32   `json:"contentType"` // 1 文本, 2 markdown
	Content          string  `json:"content"`
	DelTime          int64   `json:"delTime"`
	CreateTime       int64   `json:"createTime"`
	UpdateTime       int64   `json:"updateTime"`
	EditTime         int64   `json:"editTime"`
	LastActive       int64   `json:"lastActive"`
	LikeNum          int64   `json:"likeNum"`
	CommentNum       int64   `json:"commentNum"`
	CollectNum       int64   `json:"collectNum"`
	AmountNum        float64 `json:"amountNum"` // 打赏金额，真实接口会返回小数（如 22.01）
	IsSticky         int64   `json:"isSticky"`  // 置顶时间戳，0 表示未置顶
	SenderNickname   string  `json:"senderNickname"`
	SenderAvatar     string  `json:"senderAvatar"`
	CreateTimeText   string  `json:"createTimeText"`
	IsLiked          string  `json:"isLiked"`
	IsCollected      int32   `json:"isCollected"`
	IsReward         int32   `json:"isReward"`
	IsVip            int32   `json:"isVip"`
}

// UnmarshalJSON decodes a Post, tolerating both shapes the API uses for the
// isLiked flag: community.md shows it as a JSON string ("0") in post-list but
// as a JSON number (0) in post-detail. The exported field keeps the string
// form; a numeric value is normalised to its decimal representation.
func (p *Post) UnmarshalJSON(data []byte) error {
	type plain Post // a distinct type, so this method is not called recursively
	aux := struct {
		*plain
		IsLiked json.RawMessage `json:"isLiked"`
	}{plain: (*plain)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	liked, err := flexibleFlagString(aux.IsLiked)
	if err != nil {
		return fmt.Errorf("isLiked: %w", err)
	}
	p.IsLiked = liked
	return nil
}

// PostDetail is the payload of the post-detail endpoint: the article itself,
// its section and whether the current user moderates that section.
type PostDetail struct {
	Ba      Community
	IsAdmin int32
	Post    Post
}

// Comment is one comment below a community article.
type Comment struct {
	ID             int64   `json:"id"`
	PostID         int64   `json:"postId"`
	ParentID       int64   `json:"parentId"`
	SenderID       string  `json:"senderId"`
	Content        string  `json:"content"`
	CreateTime     int64   `json:"createTime"`
	LikeNum        int64   `json:"likeNum"`
	RepliesNum     int64   `json:"repliesNum"`
	AmountNum      float64 `json:"amountNum"` // 打赏金额，可能是小数
	SenderNickname string  `json:"senderNickname"`
	SenderAvatar   string  `json:"senderAvatar"`
	CreateTimeText string  `json:"createTimeText"`
	IsLiked        string  `json:"isLiked"`
	IsVip          int32   `json:"isVip"`
	// Replies holds nested replies (楼中楼) returned inline by comment-list.
	Replies []Comment `json:"replies"`
}

// UnmarshalJSON decodes a Comment with the same isLiked tolerance as Post: the
// flag is documented as a string, but the server is inconsistent about the
// string/number encoding for identical flag fields.
func (cm *Comment) UnmarshalJSON(data []byte) error {
	type plain Comment
	aux := struct {
		*plain
		IsLiked json.RawMessage `json:"isLiked"`
	}{plain: (*plain)(cm)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	liked, err := flexibleFlagString(aux.IsLiked)
	if err != nil {
		return fmt.Errorf("isLiked: %w", err)
	}
	cm.IsLiked = liked
	return nil
}

// Communities fetches a page of community sections (分区) and the total number
// of sections available for the selected list.
//
// typ selects the list: 1 关注, 2 热门, 3 我的, 4 全部.
//
// Endpoint: POST /v1/community/ba/following-ba-list (JSON).
func (c *Client) Communities(ctx context.Context, token string, typ int32, page, size int) ([]Community, int, error) {
	page, size = normalizeCommunityPage(page, size)
	env, err := c.postJSON(ctx, "/v1/community/ba/following-ba-list", token, communityListRequest{
		Typ:  typ,
		Size: size,
		Page: page,
	})
	if err != nil {
		return nil, 0, err
	}
	var data struct {
		Ba    []Community `json:"ba"`
		Total int         `json:"total"`
	}
	if err := decodeCommunityData(env, "分区列表", &data); err != nil {
		return nil, 0, err
	}
	return data.Ba, data.Total, nil
}

// CommunityInfo fetches one community section by id, including the current
// user's follow state (Community.IsFollowed).
//
// Endpoint: POST /v1/community/ba/info (JSON).
func (c *Client) CommunityInfo(ctx context.Context, token string, id int64) (*Community, error) {
	env, err := c.postJSON(ctx, "/v1/community/ba/info", token, communityIDRequest{ID: id})
	if err != nil {
		return nil, err
	}
	var data struct {
		Ba *Community `json:"ba"`
	}
	if err := decodeCommunityData(env, "分区信息", &data); err != nil {
		return nil, err
	}
	if data.Ba == nil {
		return nil, &APIError{Msg: "服务器返回的分区信息为空"}
	}
	return data.Ba, nil
}

// Posts fetches a page of articles of one community section and the total
// number of articles in it.
//
// Endpoint: POST /v1/community/posts/post-list (JSON).
func (c *Client) Posts(ctx context.Context, token string, baID int64, page, size int) ([]Post, int, error) {
	page, size = normalizeCommunityPage(page, size)
	env, err := c.postJSON(ctx, "/v1/community/posts/post-list", token, postListRequest{
		Typ:  communityPostListTyp,
		BaID: baID,
		Size: size,
		Page: page,
	})
	if err != nil {
		return nil, 0, err
	}
	var data struct {
		Posts []Post `json:"posts"`
		Total int    `json:"total"`
	}
	if err := decodeCommunityData(env, "文章列表", &data); err != nil {
		return nil, 0, err
	}
	return data.Posts, data.Total, nil
}

// LikePost toggles the like state of one community article. The server
// endpoint is a toggle: calling it again un-likes the article.
//
// Endpoint: POST /v1/community/posts/post-like (JSON body {"id": postID}).
func (c *Client) LikePost(ctx context.Context, token string, postID int64) error {
	if postID <= 0 {
		return fmt.Errorf("文章 ID 无效")
	}
	_, err := c.postJSON(ctx, "/v1/community/posts/post-like", token, communityIDRequest{ID: postID})
	return err
}

// CollectPost toggles the collected state of one community article.
//
// Endpoint: POST /v1/community/posts/post-collect (JSON body {"id": postID}).
func (c *Client) CollectPost(ctx context.Context, token string, postID int64) error {
	if postID <= 0 {
		return fmt.Errorf("文章 ID 无效")
	}
	_, err := c.postJSON(ctx, "/v1/community/posts/post-collect", token, communityIDRequest{ID: postID})
	return err
}

// CreateComment posts a comment or a nested reply (楼中楼) on an article.
// commentID is 0 for a top-level comment; for a reply it is the ID of the
// comment being replied to.
//
// Endpoint: POST /v1/community/comment/comment (JSON).
func (c *Client) CreateComment(ctx context.Context, token string, postID, commentID int64, content string) error {
	if postID <= 0 {
		return fmt.Errorf("文章 ID 无效")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("评论内容不能为空")
	}
	_, err := c.postJSON(ctx, "/v1/community/comment/comment", token, commentCreateRequest{
		PostID:    postID,
		CommentID: commentID,
		Content:   content,
	})
	return err
}

// PostDetail fetches one article with its section and moderation flag.
//
// Endpoint: POST /v1/community/posts/post-detail (JSON).
func (c *Client) PostDetail(ctx context.Context, token string, postID int64) (*PostDetail, error) {
	env, err := c.postJSON(ctx, "/v1/community/posts/post-detail", token, communityIDRequest{ID: postID})
	if err != nil {
		return nil, err
	}
	var detail PostDetail
	if err := decodeCommunityData(env, "文章详情", &detail); err != nil {
		return nil, err
	}
	if detail.Post.ID == 0 && detail.Ba.ID == 0 {
		return nil, &APIError{Msg: "服务器返回的文章详情为空"}
	}
	return &detail, nil
}

// Comments fetches a page of comments of one article and the total number of
// comments on it.
//
// Endpoint: POST /v1/community/comment/comment-list (JSON).
func (c *Client) Comments(ctx context.Context, token string, postID int64, page, size int) ([]Comment, int, error) {
	page, size = normalizeCommunityPage(page, size)
	env, err := c.postJSON(ctx, "/v1/community/comment/comment-list", token, commentListRequest{
		PostID: postID,
		Size:   size,
		Page:   page,
	})
	if err != nil {
		return nil, 0, err
	}
	var data struct {
		Comments []Comment `json:"comments"`
		Total    int       `json:"total"`
	}
	if err := decodeCommunityData(env, "评论列表", &data); err != nil {
		return nil, 0, err
	}
	return data.Comments, data.Total, nil
}

// ---------------------------------------------------------------------------
// request payloads and decoding helpers

// communityListRequest is the body of POST /v1/community/ba/following-ba-list.
type communityListRequest struct {
	Typ  int32 `json:"typ"`
	Size int   `json:"size"`
	Page int   `json:"page"`
}

// communityIDRequest is the body of the endpoints that only take an id
// (ba/info and posts/post-detail).
type communityIDRequest struct {
	ID int64 `json:"id"`
}

// postListRequest is the body of POST /v1/community/posts/post-list.
type postListRequest struct {
	Typ  int32 `json:"typ"`
	BaID int64 `json:"baId"`
	Size int   `json:"size"`
	Page int   `json:"page"`
}

// commentListRequest is the body of POST /v1/community/comment/comment-list.
type commentListRequest struct {
	PostID int64 `json:"postId"`
	Size   int   `json:"size"`
	Page   int   `json:"page"`
}

// commentCreateRequest is the body of POST /v1/community/comment/comment.
type commentCreateRequest struct {
	PostID    int64  `json:"postId"`
	CommentID int64  `json:"commentId"`
	Content   string `json:"content"`
}

// normalizeCommunityPage clamps paging arguments to what the community
// endpoints accept: pages are 1-based, the size defaults to 20 and is capped
// at 50.
func normalizeCommunityPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case size <= 0:
		size = communityDefaultPageSize
	case size > communityMaxPageSize:
		size = communityMaxPageSize
	}
	return page, size
}

// decodeCommunityData unmarshals the endpoint-specific part of the response
// envelope. The community endpoints always return an object on success, so a
// missing, null or empty data field is reported as an error instead of
// silently yielding a zero value.
func decodeCommunityData(env *envelope, what string, dst any) error {
	raw := bytes.TrimSpace(env.Data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return &APIError{Msg: "服务器返回的" + what + "为空"}
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("解析%s失败: %w", what, err)
	}
	return nil
}

// flexibleFlagString decodes a 0/1-style flag that the server encodes either
// as a JSON string ("0"/"1") or as a JSON number (0/1) into its string form.
// A missing/null flag decodes to "".
func flexibleFlagString(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	var n json.Number
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return "", fmt.Errorf("既不是字符串也不是数字: %w", err)
	}
	return n.String(), nil
}
