package api

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// test helpers

// setQiniuQueryEndpoint points the bucket query at an httptest server and
// restores the production value when the test finishes.
func setQiniuQueryEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	old := qiniuQueryEndpoint
	qiniuQueryEndpoint = endpoint
	t.Cleanup(func() { qiniuQueryEndpoint = old })
}

// setQiniuUploadBaseOverride forces the upload host and restores it afterwards.
func setQiniuUploadBaseOverride(t *testing.T, base string) {
	t.Helper()
	old := qiniuUploadBaseOverride
	qiniuUploadBaseOverride = base
	t.Cleanup(func() { qiniuUploadBaseOverride = old })
}

// qiniuTestPolicyToken builds an upload credential whose second part is a base64
// put-policy JSON (the shape the upload pipeline tries to parse).
func qiniuTestPolicyToken(t *testing.T, accessKey, bucket string) string {
	t.Helper()
	policy, err := json.Marshal(map[string]string{"scope": bucket + ":media/", "deadline": "1999999999"})
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	return accessKey + ":" + base64.RawURLEncoding.EncodeToString(policy)
}

// testUploadFile creates a temp file with the given content and name suffix.
func testUploadFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// expectedObjectKey mirrors the documented key rule: lowercase md5 + lowercase
// extension.
func expectedObjectKey(content []byte, name string) string {
	sum := md5.Sum(content)
	return hex.EncodeToString(sum[:]) + strings.ToLower(filepath.Ext(name))
}

// readMultipartUpload parses a qiniu multipart form upload.
func readMultipartUpload(t *testing.T, r *http.Request) (fields map[string]string, fileContent []byte) {
	t.Helper()
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		t.Errorf("parse multipart: %v", err)
		return nil, nil
	}
	fields = map[string]string{
		"token": r.FormValue("token"),
		"key":   r.FormValue("key"),
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		t.Errorf("multipart file: %v", err)
		return fields, nil
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(file)
	if err != nil {
		t.Errorf("read multipart file: %v", err)
	}
	return fields, content
}

// ---------------------------------------------------------------------------
// upload token

func TestUploadTokenKinds(t *testing.T) {
	wantPaths := map[UploadKind]string{
		UploadImage: "/v1/misc/qiniu-token",
		UploadAudio: "/v1/misc/qiniu-token-audio",
		UploadFile:  "/v1/misc/qiniu-token2",
		UploadVideo: "/v1/misc/qiniu-token-video",
	}
	// 让服务器把请求路径当作上传凭证返回，从而在无共享变量的情况下验证每个
	// 上传类型命中了正确的接口路径。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("token"); got != "tok-1" {
			t.Errorf("token header = %q, want tok-1", got)
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"token":"`+r.URL.Path+`"}}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	for kind, wantPath := range wantPaths {
		credential, err := client.UploadToken(context.Background(), "tok-1", kind)
		if err != nil {
			t.Fatalf("%s UploadToken: %v", kind.Label(), err)
		}
		if credential != wantPath {
			t.Fatalf("%s credential (request path) = %q, want %q", kind.Label(), credential, wantPath)
		}
	}
}

func TestUploadTokenErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-101,"msg":"未登录"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.UploadToken(context.Background(), "bad-token", UploadImage)
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}

	if _, err := client.UploadToken(context.Background(), "", UploadImage); err == nil {
		t.Fatal("expected error for empty account token")
	}
	if _, err := client.UploadToken(context.Background(), "tok-1", UploadKind(9)); err == nil {
		t.Fatal("expected error for unknown upload kind")
	}
}

func TestUploadTokenEmptyCredential(t *testing.T) {
	// data 直接是字符串时也要能取出凭证。
	stringSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":"ak-2:sign"}`)
	}))
	defer stringSrv.Close()

	client := newTestClient(t)
	client.BaseURL = stringSrv.URL
	credential, err := client.UploadToken(context.Background(), "tok-1", UploadImage)
	if err != nil || credential != "ak-2:sign" {
		t.Fatalf("string data credential = %q, err = %v", credential, err)
	}

	// data 为空对象 → 明确报错。
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{}}`)
	}))
	defer emptySrv.Close()

	client = newTestClient(t)
	client.BaseURL = emptySrv.URL
	if _, err := client.UploadToken(context.Background(), "tok-1", UploadImage); err == nil {
		t.Fatal("expected error for empty upload credential")
	}
}

// ---------------------------------------------------------------------------
// upload pipeline

func TestUploadFile(t *testing.T) {
	content := []byte("hello image bytes")
	name := "Photo.PNG"
	wantKey := expectedObjectKey(content, name)
	localPath := testUploadFile(t, name, content)
	credential := qiniuTestPolicyToken(t, "test-ak", "test-bucket")

	var qiniuSrv *httptest.Server
	qiniuSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4/query":
			if got := r.URL.Query().Get("ak"); got != "test-ak" {
				t.Errorf("query ak = %q", got)
			}
			if got := r.URL.Query().Get("bucket"); got != "test-bucket" {
				t.Errorf("query bucket = %q", got)
			}
			// 域名带 scheme 以便 httptest 的 http 服务器可达；真实响应为纯域名。
			_, _ = io.WriteString(w, `{"hosts":[{"region":"z0","ttl":86400,"up":{"domains":["`+qiniuSrv.URL+`"],"old":["http://old.invalid"]}}]}`)
		case "/":
			if r.Method != http.MethodPost {
				t.Errorf("upload method = %s, want POST", r.Method)
			}
			fields, fileContent := readMultipartUpload(t, r)
			if fields["token"] != credential {
				t.Errorf("upload token field = %q", fields["token"])
			}
			if fields["key"] != wantKey {
				t.Errorf("upload key field = %q, want %q", fields["key"], wantKey)
			}
			if string(fileContent) != string(content) {
				t.Errorf("uploaded content = %q", string(fileContent))
			}
			_, _ = io.WriteString(w, `{"key":"`+wantKey+`","hash":"FkSXEmuHphV9We5U1ChAFX7qxfLb","fsize":`+
				strconv.Itoa(len(content))+`}`)
		default:
			t.Errorf("unexpected qiniu path %s", r.URL.Path)
		}
	}))
	defer qiniuSrv.Close()
	setQiniuQueryEndpoint(t, qiniuSrv.URL+"/v4/query")
	setQiniuUploadBaseOverride(t, "")

	var distributionCalls atomic.Int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/misc/qiniu-token":
			writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"token":"`+credential+`"}}`)
		case "/v1/misc/configure-distribution":
			distributionCalls.Add(1)
			writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{
				"imageUrl":"https://media.example.test/img/","fileUrl":"https://media.example.test/file/",
				"audioUrl":"https://media.example.test/audio/","videoUrl":"https://media.example.test/video/"}}`)
		default:
			t.Errorf("unexpected api path %s", r.URL.Path)
		}
	}))
	defer apiSrv.Close()

	client := newTestClient(t)
	client.BaseURL = apiSrv.URL
	key, fileURL, err := client.UploadFile(context.Background(), "tok-1", UploadImage, localPath)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if key != wantKey {
		t.Fatalf("key = %q, want %q", key, wantKey)
	}
	wantURL := "https://media.example.test/img/" + wantKey
	if fileURL != wantURL {
		t.Fatalf("fileURL = %q, want %q", fileURL, wantURL)
	}
	if calls := distributionCalls.Load(); calls != 1 {
		t.Fatalf("distribution calls = %d, want 1", calls)
	}
}

func TestUploadFileUsesServerReturnedKey(t *testing.T) {
	content := []byte("sticker zip")
	localPath := testUploadFile(t, "pack.zip", content)

	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readMultipartUpload(t, r)
		_, _ = io.WriteString(w, `{"key":"sticker/rewritten.zip","hash":"h","fsize":11}`)
	}))
	defer uploadSrv.Close()
	setQiniuUploadBaseOverride(t, uploadSrv.URL)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/misc/qiniu-token2":
			writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"token":"ak:sign"}}`)
		case "/v1/misc/configure-distribution":
			writeJSONResp(w, 200, `{"code":-1,"msg":"不可用"}`)
		default:
			t.Errorf("unexpected api path %s", r.URL.Path)
		}
	}))
	defer apiSrv.Close()

	client := newTestClient(t)
	client.BaseURL = apiSrv.URL
	key, fileURL, err := client.UploadFile(context.Background(), "tok-1", UploadFile, localPath)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if key != "sticker/rewritten.zip" {
		t.Fatalf("key = %q, want server returned key", key)
	}
	// 功能路由失败 → 回退到内置文件域名。
	wantURL := defaultFileRoute + "sticker/rewritten.zip"
	if fileURL != wantURL {
		t.Fatalf("fileURL = %q, want %q", fileURL, wantURL)
	}
}

func TestUploadFileValidation(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.png")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	tests := []struct {
		name      string
		token     string
		kind      UploadKind
		path      string
		errSubstr string
	}{
		{"未登录", "", UploadImage, emptyPath, "未登录"},
		{"未知类型", "tok-1", UploadKind(7), emptyPath, "上传类型"},
		{"空路径", "tok-1", UploadImage, "  ", "文件路径不能为空"},
		{"文件不存在", "tok-1", UploadImage, filepath.Join(dir, "missing.png"), "无法读取本地文件"},
		{"目录", "tok-1", UploadImage, dir, "不是普通文件"},
		{"空文件", "tok-1", UploadImage, emptyPath, "不能上传空文件"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := client.UploadFile(ctx, tc.token, tc.kind, tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("error = %v, want substring %q", err, tc.errSubstr)
			}
		})
	}
}

func TestUploadFileRejectsOversizedImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := file.Truncate(maxImageUploadBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	client := newTestClient(t)
	// 大小检查在发起任何 HTTP 请求之前完成，因此不需要测试服务器。
	_, _, err = client.UploadFile(context.Background(), "tok-1", UploadImage, path)
	if err == nil || !strings.Contains(err.Error(), "大小超出限制") {
		t.Fatalf("error = %v, want size limit error", err)
	}
	if !strings.Contains(err.Error(), "20 MB") {
		t.Fatalf("error = %v, want 20 MB in message", err)
	}
}

func TestUploadAndEditAvatar(t *testing.T) {
	content := []byte("avatar png")
	name := "avatar.png"
	wantKey := expectedObjectKey(content, name)
	localPath := testUploadFile(t, name, content)

	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readMultipartUpload(t, r)
		_, _ = io.WriteString(w, `{"key":"`+wantKey+`","hash":"h","fsize":10}`)
	}))
	defer uploadSrv.Close()
	setQiniuUploadBaseOverride(t, uploadSrv.URL)

	wantAvatarURL := "https://cdn.example.test/avatar/" + wantKey
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/misc/qiniu-token":
			writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"token":"ak:sign"}}`)
		case "/v1/misc/configure-distribution":
			writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"imageUrl":"https://cdn.example.test/avatar/"}}`)
		case "/v1/user/edit-avatar":
			body, _ := io.ReadAll(r.Body)
			if got := string(parseRequestFieldBytes(t, body, 2)); got != wantAvatarURL {
				t.Errorf("edit-avatar url = %q, want %q", got, wantAvatarURL)
			}
			writeProtoResp(w, testStatusResponse(1, "success"))
		default:
			t.Errorf("unexpected api path %s", r.URL.Path)
		}
	}))
	defer apiSrv.Close()

	client := newTestClient(t)
	client.BaseURL = apiSrv.URL
	avatarURL, err := client.UploadAndEditAvatar(context.Background(), "tok-1", localPath)
	if err != nil {
		t.Fatalf("UploadAndEditAvatar: %v", err)
	}
	if avatarURL != wantAvatarURL {
		t.Fatalf("avatarURL = %q, want %q", avatarURL, wantAvatarURL)
	}
}

// ---------------------------------------------------------------------------
// qiniu credential / upload host parsing

func TestParseQiniuCredential(t *testing.T) {
	policy := `{"scope":"bucket-1:prefix/x/"}`
	padded := base64.StdEncoding.EncodeToString([]byte(policy))
	rawURL := base64.RawURLEncoding.EncodeToString([]byte(policy))

	cases := []struct {
		name       string
		credential string
		wantAK     string
		wantBucket string
	}{
		{"raw url base64", "ak-1:" + rawURL, "ak-1", "bucket-1"},
		{"padded std base64", "ak-1:" + padded, "ak-1", "bucket-1"},
		{"scope 只有 bucket", "ak-2:" + base64.RawURLEncoding.EncodeToString([]byte(`{"scope":"b2"}`)), "ak-2", "b2"},
		{"第二段不是 policy", "ak-3:c2lnbmF0dXJl", "ak-3", ""},
		{"没有冒号", "garbage", "", ""},
		{"空凭证", "   ", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ak, bucket := parseQiniuCredential(tc.credential)
			if ak != tc.wantAK || bucket != tc.wantBucket {
				t.Fatalf("parseQiniuCredential(%q) = (%q, %q), want (%q, %q)",
					tc.credential, ak, bucket, tc.wantAK, tc.wantBucket)
			}
		})
	}
}

func TestParseQiniuUploadHost(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"旧格式 domains", `{"hosts":[{"region":"z1","ttl":86400,"up":{"domains":["upload-z1.qiniup.com","up-z1.qiniup.com"],"old":["upload-z1.qbox.me"]}}]}`, "https://upload-z1.qiniup.com"},
		{"新格式 preferred", `{"hosts":[{"region_id":"z0","ttl":86400,"up":{"preferred":["upload.qiniup.com"]}}]}`, "https://upload.qiniup.com"},
		{"带 scheme", `{"hosts":[{"up":{"domains":["http://127.0.0.1:9000"]}}]}`, "http://127.0.0.1:9000"},
		{"嵌套 data", `{"data":{"hosts":[{"up":{"domains":["up.qiniup.com"]}}]}}`, "https://up.qiniup.com"},
		{"没有域名", `{"hosts":[]}`, ""},
		{"非法 JSON", `not json`, ""},
		{"空响应", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseQiniuUploadHost([]byte(tc.raw)); got != tc.want {
				t.Fatalf("parseQiniuUploadHost(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestResolveQiniuUploadBaseFallsBack(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	// 查询接口失败 → 返回空串，由调用方回退默认域名。
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	setQiniuQueryEndpoint(t, failing.URL)
	credential := qiniuTestPolicyToken(t, "test-ak", "test-bucket")
	if base := client.resolveQiniuUploadBase(ctx, credential); base != "" {
		t.Fatalf("base = %q, want empty on query failure", base)
	}

	// 凭证无法解析出 bucket → 不查询，直接返回空串。
	notPolicy := "ak-1:" + base64.RawURLEncoding.EncodeToString([]byte("not a json policy"))
	if base := client.resolveQiniuUploadBase(ctx, notPolicy); base != "" {
		t.Fatalf("base = %q, want empty for unparseable credential", base)
	}

	// 解析失败时端点必须回退到文档给出的默认上传域名。
	if endpoint := client.qiniuUploadEndpoint(ctx, notPolicy); endpoint != defaultQiniuUploadBase+"/" {
		t.Fatalf("endpoint = %q, want %q", endpoint, defaultQiniuUploadBase+"/")
	}
}

func TestResolveQiniuUploadBaseFromQuery(t *testing.T) {
	client := newTestClient(t)
	querySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bucket") != "b1" {
			t.Errorf("bucket = %q", r.URL.Query().Get("bucket"))
		}
		_, _ = io.WriteString(w, `{"hosts":[{"up":{"domains":["upload.example.test"]}}]}`)
	}))
	defer querySrv.Close()
	setQiniuQueryEndpoint(t, querySrv.URL)

	credential := qiniuTestPolicyToken(t, "ak-1", "b1")
	if base := client.resolveQiniuUploadBase(context.Background(), credential); base != "https://upload.example.test" {
		t.Fatalf("base = %q, want https://upload.example.test", base)
	}
}

// ---------------------------------------------------------------------------
// send image / file messages

func TestSendImageMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/msg/send-message" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("token"); got != "tok-1" {
			t.Errorf("token header = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		msgID := string(parseRequestFieldBytes(t, body, 2))
		if decoded, err := hex.DecodeString(msgID); err != nil || len(decoded) != 16 {
			t.Errorf("msg_id = %q, want 32 hex chars", msgID)
		}
		if got := string(parseRequestFieldBytes(t, body, 3)); got != "1001" {
			t.Errorf("chat id = %q", got)
		}
		if got := parseRequestFieldVarint(t, body, 4); got != 1 {
			t.Errorf("chat type = %d", got)
		}
		content := parseRequestFieldBytes(t, body, 5)
		if got := string(parseRequestFieldBytes(t, content, 3)); got != "https://media.example.test/img/a.png" {
			t.Errorf("image url = %q", got)
		}
		if got := parseRequestFieldVarint(t, body, 6); got != 2 {
			t.Errorf("content type = %d, want 2", got)
		}
		writeProtoResp(w, testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	msgID, err := client.SendImageMessage(context.Background(), "tok-1", "1001", ChatTypeUser,
		"  https://media.example.test/img/a.png  ")
	if err != nil {
		t.Fatalf("SendImageMessage: %v", err)
	}
	if msgID == "" || len(msgID) != 32 {
		t.Fatalf("msgID = %q, want 32 hex chars", msgID)
	}
	if _, err := client.SendImageMessage(context.Background(), "tok-1", "1001", ChatTypeUser, "   "); err == nil {
		t.Fatal("expected error for empty image url")
	}
}

func TestSendFileMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if got := parseRequestFieldVarint(t, body, 4); got != 2 {
			t.Errorf("chat type = %d, want 2 (ChatTypeGroup)", got)
		}
		content := parseRequestFieldBytes(t, body, 5)
		if got := string(parseRequestFieldBytes(t, content, 4)); got != "报告.pdf" {
			t.Errorf("file name = %q", got)
		}
		if got := string(parseRequestFieldBytes(t, content, 5)); got != "https://media.example.test/file/report.pdf" {
			t.Errorf("file url = %q", got)
		}
		if got := parseRequestFieldVarint(t, content, 18); got != 2048 {
			t.Errorf("file size = %d, want 2048", got)
		}
		if got := parseRequestFieldVarint(t, body, 6); got != 4 {
			t.Errorf("content type = %d, want 4", got)
		}
		writeProtoResp(w, testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if _, err := client.SendFileMessage(context.Background(), "tok-1", "2002", ChatTypeGroup,
		"报告.pdf", "https://media.example.test/file/report.pdf", 2048); err != nil {
		t.Fatalf("SendFileMessage: %v", err)
	}

	if _, err := client.SendFileMessage(context.Background(), "tok-1", "2002", ChatTypeGroup,
		"", "   ", 10); err == nil {
		t.Fatal("expected error for empty file url")
	}
}

func TestSendFileMessageDerivesNameAndDefaultsChatType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if got := parseRequestFieldVarint(t, body, 4); got != 1 {
			t.Errorf("chat type = %d, want 1 (default user)", got)
		}
		content := parseRequestFieldBytes(t, body, 5)
		if got := string(parseRequestFieldBytes(t, content, 4)); got != "report.pdf" {
			t.Errorf("derived file name = %q", got)
		}
		// fileSize=0 时不写 file_size 字段。
		if got := parseRequestFieldVarint(t, content, 18); got != 0 {
			t.Errorf("file size = %d, want omitted", got)
		}
		writeProtoResp(w, testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if _, err := client.SendFileMessage(context.Background(), "tok-1", "1001", ChatTypeUnknown,
		"", "https://media.example.test/file/report.pdf?sign=1", -1); err != nil {
		t.Fatalf("SendFileMessage: %v", err)
	}
}

// ---------------------------------------------------------------------------
// get-user

// testGetUserResponse builds a successful GetUserResponse with a full profile.
func testGetUserResponse() []byte {
	var data []byte
	data = testAppendString(data, 1, "10086")
	data = testAppendString(data, 2, "小明")
	data = testAppendVarint(data, 3, 12345) // name_id
	data = testAppendString(data, 4, "https://example.com/avatar.png")
	data = testAppendVarint(data, 5, 888) // avatar_id

	medal1 := testAppendVarint(nil, 1, 7) // medal id
	medal1 = testAppendString(medal1, 2, "元老")
	medal1 = testAppendVarint(medal1, 5, 1) // sort
	data = testAppendBytes(data, 6, medal1)

	medal2 := testAppendVarint(nil, 1, 9)
	medal2 = testAppendString(medal2, 2, "活跃达人")
	data = testAppendBytes(data, 6, medal2)

	data = testAppendString(data, 7, "2024-01-02 03:04:05")
	data = testAppendVarint(data, 8, 1) // do_not_disturb
	data = testAppendVarint(data, 9, 42)
	data = testAppendVarint(data, 10, 1710000000)
	data = testAppendVarint(data, 11, 100) // online_day
	data = testAppendVarint(data, 12, 5)   // continuous_online_day
	data = testAppendVarint(data, 13, 1)   // is_vip
	data = testAppendVarint(data, 14, 1900000000)
	data = testAppendVarint(data, 16, 1) // is_blocked
	data = testAppendString(data, 20, "中国")
	data = testAppendString(data, 21, "违规封禁")

	var out []byte
	out = testAppendBytes(out, 1, testStatusBytes(1, "success"))
	out = testAppendBytes(out, 2, data)
	return out
}

func TestGetUserProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/user/get-user" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if got := string(parseRequestFieldBytes(t, body, 2)); got != "10086" {
			t.Errorf("requested user id = %q", got)
		}
		writeProtoResp(w, testGetUserResponse())
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	profile, err := client.GetUserProfile(context.Background(), "tok-1", " 10086 ")
	if err != nil {
		t.Fatalf("GetUserProfile: %v", err)
	}
	if profile.ID != "10086" || profile.Name != "小明" || profile.AvatarURL != "https://example.com/avatar.png" ||
		profile.NameID != 12345 || profile.AvatarID != 888 {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if profile.RegisterTime != "2024-01-02 03:04:05" || profile.IPGeo != "中国" {
		t.Fatalf("unexpected time/geo: %+v", profile)
	}
	if !profile.IsVIP || profile.VIPExpiredTimestamp != 1900000000 {
		t.Fatalf("unexpected vip fields: %+v", profile)
	}
	if profile.BanID != 42 || profile.BanUntilTimestamp != 1710000000 || profile.BanReason != "违规封禁" {
		t.Fatalf("unexpected ban fields: %+v", profile)
	}
	if profile.OnlineDay != 100 || profile.ContinuousOnlineDay != 5 {
		t.Fatalf("unexpected online day: %+v", profile)
	}
	if !profile.DoNotDisturb || !profile.IsBlocked {
		t.Fatalf("unexpected bool fields: %+v", profile)
	}
	if len(profile.MedalNames) != 2 || profile.MedalNames[0] != "元老" || profile.MedalNames[1] != "活跃达人" {
		t.Fatalf("medal names = %v", profile.MedalNames)
	}
}

func TestGetUserProfileErrors(t *testing.T) {
	// 业务错误码（4402 用户不存在）要转成 *APIError。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out []byte
		out = testAppendBytes(out, 1, testStatusBytes(4402, "该用户不存在"))
		writeProtoResp(w, out)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	_, err := client.GetUserProfile(context.Background(), "tok-1", "404")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 4402 || apiErr.Msg != "该用户不存在" {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}

	if _, err := client.GetUserProfile(context.Background(), "tok-1", "   "); err == nil {
		t.Fatal("expected error for empty user id")
	}
}

func TestGetUserProfileEmptyAndMalformed(t *testing.T) {
	// 只有 status、没有 data。
	emptyDataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out []byte
		out = testAppendBytes(out, 1, testStatusBytes(1, "success"))
		writeProtoResp(w, out)
	}))
	defer emptyDataSrv.Close()
	client := newTestClient(t)
	client.BaseURL = emptyDataSrv.URL
	if _, err := client.GetUserProfile(context.Background(), "tok-1", "10086"); err == nil {
		t.Fatal("expected error for missing profile data")
	}

	// 畸形 protobuf。
	malformedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProtoResp(w, []byte{0xff, 0xff, 0xff})
	}))
	defer malformedSrv.Close()
	client = newTestClient(t)
	client.BaseURL = malformedSrv.URL
	if _, err := client.GetUserProfile(context.Background(), "tok-1", "10086"); err == nil {
		t.Fatal("expected error for malformed protobuf")
	}
}

// ---------------------------------------------------------------------------
// misc helpers

func TestUploadKindLabel(t *testing.T) {
	want := map[UploadKind]string{
		UploadImage: "图片",
		UploadAudio: "音频",
		UploadFile:  "文件",
		UploadVideo: "视频",
	}
	for kind, label := range want {
		if got := kind.Label(); got != label {
			t.Fatalf("%d Label = %q, want %q", int(kind), got, label)
		}
	}
}

func TestMediaURLHelpers(t *testing.T) {
	if got := joinMediaURL("https://cdn.example.test/", "/abc.png"); got != "https://cdn.example.test/abc.png" {
		t.Fatalf("joinMediaURL = %q", got)
	}
	if got := joinMediaURL("", "abc.png"); got != "" {
		t.Fatalf("joinMediaURL with empty base = %q", got)
	}
	if got := validMediaRoute("https://cdn.example.test"); got != "https://cdn.example.test/" {
		t.Fatalf("validMediaRoute = %q", got)
	}
	if got := validMediaRoute("cdn.example.test"); got != "" {
		t.Fatalf("validMediaRoute without scheme = %q", got)
	}
	for kind, want := range map[UploadKind]string{
		UploadImage: defaultImageRoute,
		UploadAudio: defaultAudioRoute,
		UploadFile:  defaultFileRoute,
		UploadVideo: defaultVideoRoute,
	} {
		if got := defaultMediaRoute(kind); got != want {
			t.Fatalf("%s default route = %q, want %q", kind.Label(), got, want)
		}
	}
}

func TestFileNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://media.example.test/file/report.pdf":        "report.pdf",
		"https://media.example.test/file/report.pdf?sign=1": "report.pdf",
		"https://media.example.test/file/":                  "file",
		"https://media.example.test":                        "",
		"/static/no-extension":                              "no-extension",
		"http://[::1":                                       "",
	}
	for raw, want := range cases {
		if got := fileNameFromURL(raw); got != want {
			t.Fatalf("fileNameFromURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
