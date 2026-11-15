package api

// 媒体上传（七牛 Kodo）与他人主页 API。
//
// 本文件新增「图片/音频/文件/视频上传到七牛 Kodo + 发送图片/文件消息 + 上传并
// 修改头像 + 获取他人主页」能力，不修改包内已有文件（client.go / chat.go /
// profile.go 等）中的任何实现。
//
// 上传流程（依据 ../yhchatAPI/src/api/v1/misc.md）：
//
//  1. GET /v1/misc/qiniu-token（图片）、/v1/misc/qiniu-token-audio（音频）、
//     /v1/misc/qiniu-token2（文件）、/v1/misc/qiniu-token-video（视频）拿到
//     七牛上传凭证：{"code":1,"data":{"token":"AK:..."} }。
//  2. 解析凭证得到 AccessKey 与 bucket（文档称第二部分是 base64 的 put-policy，
//     其 scope 为 "<bucket>[:<key 前缀>]"），再通过
//     GET https://api.qiniu.com/v4/query?ak=<AK>&bucket=<bucket>
//     查询上传域名（响应 up 下的域名）。
//     注意：真实七牛上传凭证的第二段是 HMAC 签名而非 policy JSON，因此这里做
//     防御式解析；无法恢复 bucket 时直接回退到文档给出的
//     https://upload.qiniup.com，保证上传流程不会因解析失败而中断。
//  3. multipart/form-data POST 到上传域名，字段为 token、key、file；
//     key 规则与官方客户端一致：md5(文件内容) 的小写十六进制 + 文件扩展名。
//  4. 可访问 URL = 功能路由（GET /v1/misc/configure-distribution 的
//     imageUrl/fileUrl/audioUrl/videoUrl）+ "/" + key；功能路由获取失败时按
//     上传类型回退到内置域名（如 https://chat-img.jwznb.com/）。
//
// 上传过程不会打印上传凭证或文件内容；本地文件大小在上传前先做限制检查，
// 避免把超大文件读进内存。

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// UploadKind 表示一次上传所属的媒体类型，决定上传 token 接口与公开访问路由。
type UploadKind int

// 上传类型。数值顺序与 full.proto 的 ContentType 中图片/音频/文件/视频的
// 排列一致（图片=2、文件=4 等并不相同，此处仅作为独立枚举使用）。
const (
	UploadImage UploadKind = iota // 图片
	UploadAudio                   // 音频
	UploadFile                    // 文件
	UploadVideo                   // 视频
)

// Label 返回上传类型的中文名称，用于错误提示。
func (k UploadKind) Label() string {
	switch k {
	case UploadImage:
		return "图片"
	case UploadAudio:
		return "音频"
	case UploadFile:
		return "文件"
	case UploadVideo:
		return "视频"
	default:
		return "文件"
	}
}

// valid 报告 k 是否为已定义的上传类型。
func (k UploadKind) valid() bool {
	return k >= UploadImage && k <= UploadVideo
}

// tokenPath 返回该上传类型对应的上传凭证接口路径。
//
// 文档使用四个独立路径（注意文件为 qiniu-token2、音频/视频带后缀），不是统一的
// 路径加参数。
func (k UploadKind) tokenPath() string {
	switch k {
	case UploadImage:
		return "/v1/misc/qiniu-token"
	case UploadAudio:
		return "/v1/misc/qiniu-token-audio"
	case UploadVideo:
		return "/v1/misc/qiniu-token-video"
	default:
		return "/v1/misc/qiniu-token2"
	}
}

const (
	// defaultQiniuUploadBase 是上传域名查询失败时的回退上传域名（文档给出）。
	defaultQiniuUploadBase = "https://upload.qiniup.com"

	// distributionPath 是媒体公开访问（功能）路由接口。
	distributionPath = "/v1/misc/configure-distribution"

	// 以下为 configure-distribution 获取失败时按类型回退的公开访问域名。
	defaultImageRoute = "https://chat-img.jwznb.com/"
	defaultAudioRoute = "https://chat-audio1.jwznb.com/"
	defaultFileRoute  = "https://chat-file.jwznb.com/"
	defaultVideoRoute = "https://chat-video1.jwznb.com/"

	// 本地文件上传大小上限（客户端防御性限制，避免超大文件占用内存）。
	maxImageUploadBytes = int64(20 << 20)  // 图片 20 MB
	maxAudioUploadBytes = int64(100 << 20) // 音频 100 MB
	maxFileUploadBytes  = int64(100 << 20) // 文件 100 MB
	maxVideoUploadBytes = int64(200 << 20) // 视频 200 MB
)

// 以下变量只在测试中覆盖，用于让上传流程在完全离线（httptest）的情况下可测。
var (
	// qiniuQueryEndpoint 是 "bucket -> 上传域名" 查询接口，测试可指向 httptest。
	qiniuQueryEndpoint = "https://api.qiniu.com/v4/query"
	// qiniuUploadBaseOverride 非空时直接作为上传域名，跳过凭证解析与域名查询。
	qiniuUploadBaseOverride string
)

// httpClient 返回配置好的 HTTP 客户端；手工构造的 Client 没有 HTTP 时使用默认值。
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: DefaultTimeout}
}

// getJSON 发起 GET 请求并解析标准 JSON 信封。
//
// 包内已有的 postJSON 只处理 POST，而上传凭证与功能路由接口都是 GET，故在
// 本文件补充一个等价的 GET 版本。
func (c *Client) getJSON(ctx context.Context, path, token string) (*envelope, error) {
	raw, header, err := c.do(ctx, http.MethodGet, path, token, nil)
	if err != nil {
		return nil, err
	}
	env, err := parseEnvelope(raw, header)
	if err != nil {
		return nil, err
	}
	if env.Code != 1 {
		return env, &APIError{Code: env.Code, Msg: env.Msg}
	}
	return env, nil
}

// UploadToken 获取指定媒体类型的七牛上传凭证。
//
// Endpoint 见 UploadKind.tokenPath；响应体为
// {"code":1,"data":{"token":"AK:..."},"msg":"success"}。
func (c *Client) UploadToken(ctx context.Context, token string, kind UploadKind) (string, error) {
	if strings.TrimSpace(token) == "" {
		return "", &APIError{Code: -101, Msg: "未登录"}
	}
	if !kind.valid() {
		return "", fmt.Errorf("不支持的上传类型: %d", int(kind))
	}
	env, err := c.getJSON(ctx, kind.tokenPath(), token)
	if err != nil {
		return "", err
	}
	credential := decodeQiniuCredential(env.Data)
	if credential == "" {
		return "", &APIError{Msg: "服务器返回的上传凭证为空"}
	}
	return credential, nil
}

// decodeQiniuCredential 从上传凭证接口的 data 中取出 token 字符串。
//
// 正常响应是 {"token":"..."}；这里也兼容 data 直接是字符串的写法，做防御式解析。
func decodeQiniuCredential(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if token := strings.TrimSpace(payload.Token); token != "" {
			return token
		}
	}
	var token string
	if err := json.Unmarshal(raw, &token); err == nil {
		return strings.TrimSpace(token)
	}
	return ""
}

// UploadFile 把本地文件上传到七牛 Kodo，返回对象 key 与可直接访问的 URL。
//
// kind 决定上传凭证接口、大小上限和公开访问路由；localPath 必须是普通文件。
// 返回的 key 优先使用七牛响应中的 key（服务端可能改写，例如表情上传会加前缀），
// 响应缺少 key 时使用本地计算出的 md5+扩展名。
func (c *Client) UploadFile(ctx context.Context, token string, kind UploadKind, localPath string) (key, fileURL string, err error) {
	if strings.TrimSpace(token) == "" {
		return "", "", &APIError{Code: -101, Msg: "未登录"}
	}
	if !kind.valid() {
		return "", "", fmt.Errorf("不支持的上传类型: %d", int(kind))
	}
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", "", fmt.Errorf("文件路径不能为空")
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return "", "", fmt.Errorf("无法读取本地文件: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("不是普通文件: %s", localPath)
	}
	if info.Size() <= 0 {
		return "", "", fmt.Errorf("不能上传空文件")
	}
	limit := maxUploadBytes(kind)
	if info.Size() > limit {
		return "", "", fmt.Errorf("%s大小超出限制（最大 %s）", kind.Label(), formatUploadLimit(limit))
	}

	// 大小已校验，可以安全地把文件读入内存（上限即内存占用上限）。
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", "", fmt.Errorf("读取本地文件失败: %w", err)
	}
	if int64(len(data)) > limit {
		return "", "", fmt.Errorf("%s大小超出限制（最大 %s）", kind.Label(), formatUploadLimit(limit))
	}

	credential, err := c.UploadToken(ctx, token, kind)
	if err != nil {
		return "", "", err
	}

	objectKey := qiniuObjectKey(data, localPath)
	uploadedKey, err := c.putQiniuObject(ctx, credential, objectKey, filepath.Base(localPath), data)
	if err != nil {
		return "", "", err
	}
	key = uploadedKey
	return key, c.mediaFileURL(ctx, token, kind, key), nil
}

// maxUploadBytes 返回该上传类型的本地大小上限。
func maxUploadBytes(kind UploadKind) int64 {
	switch kind {
	case UploadImage:
		return maxImageUploadBytes
	case UploadAudio:
		return maxAudioUploadBytes
	case UploadVideo:
		return maxVideoUploadBytes
	default:
		return maxFileUploadBytes
	}
}

// formatUploadLimit 把字节上限格式化成 "20 MB" 这样的提示文本。
func formatUploadLimit(limit int64) string {
	return fmt.Sprintf("%d MB", limit>>20)
}

// qiniuObjectKey 按官方客户端规则生成对象 key：小写十六进制 md5 + 小写扩展名。
//
// 例如 content 的 md5 为 8a5d…c1、本地文件为 Photo.PNG，key 为
// "8a5d…c1.png"。没有扩展名时只使用 md5。
func qiniuObjectKey(data []byte, localPath string) string {
	sum := md5.Sum(data)
	key := hex.EncodeToString(sum[:])
	if ext := strings.ToLower(filepath.Ext(localPath)); ext != "" {
		key += ext
	}
	return key
}

// putQiniuObject 用 multipart/form-data 把数据直传到七牛，返回响应中的 key。
func (c *Client) putQiniuObject(ctx context.Context, credential, objectKey, fileName string, data []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("token", credential); err != nil {
		return "", fmt.Errorf("构建上传请求失败: %w", err)
	}
	if err := writer.WriteField("key", objectKey); err != nil {
		return "", fmt.Errorf("构建上传请求失败: %w", err)
	}
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("构建上传请求失败: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("构建上传请求失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("构建上传请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.qiniuUploadEndpoint(ctx, credential), &body)
	if err != nil {
		return "", fmt.Errorf("构建上传请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("上传到对象存储失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("读取上传结果失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("上传到对象存储失败: %s", qiniuErrorMessage(raw, resp.StatusCode))
	}
	var result struct {
		Key   string `json:"key"`
		Error string `json:"error"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			return "", fmt.Errorf("无法解析对象存储响应")
		}
	}
	if errMsg := strings.TrimSpace(result.Error); errMsg != "" {
		return "", fmt.Errorf("上传到对象存储失败: %s", errMsg)
	}
	if uploaded := strings.TrimSpace(result.Key); uploaded != "" {
		return uploaded, nil
	}
	if objectKey != "" {
		return objectKey, nil
	}
	return "", fmt.Errorf("对象存储响应缺少文件 key")
}

// qiniuErrorMessage 从七牛错误响应中提取错误信息（响应形如 {"error":"..."}）。
func qiniuErrorMessage(raw []byte, status int) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		if msg := strings.TrimSpace(payload.Error); msg != "" {
			return msg
		}
	}
	snippet := strings.TrimSpace(string(raw))
	if snippet != "" {
		if runes := []rune(snippet); len(runes) > 120 {
			snippet = string(runes[:120]) + "…"
		}
		return fmt.Sprintf("HTTP %d: %s", status, snippet)
	}
	return fmt.Sprintf("HTTP %d", status)
}

// qiniuUploadEndpoint 返回 multipart 直传的目标 URL（以 "/" 结尾）。
//
// 优先使用测试覆盖值；否则解析上传凭证查询上传域名；查询失败时回退到文档
// 给出的默认上传域名 https://upload.qiniup.com。
func (c *Client) qiniuUploadEndpoint(ctx context.Context, credential string) string {
	if override := strings.TrimSpace(qiniuUploadBaseOverride); override != "" {
		return strings.TrimRight(override, "/") + "/"
	}
	if base := c.resolveQiniuUploadBase(ctx, credential); base != "" {
		return strings.TrimRight(base, "/") + "/"
	}
	return defaultQiniuUploadBase + "/"
}

// qiniuPutPolicy 是上传凭证中（文档声称的）base64 put-policy 需要解析的字段。
type qiniuPutPolicy struct {
	Scope string `json:"scope"`
}

// resolveQiniuUploadBase 解析上传凭证并从七牛查询上传域名；任何失败都返回空串，
// 由调用方回退到默认上传域名。
func (c *Client) resolveQiniuUploadBase(ctx context.Context, credential string) string {
	accessKey, bucket := parseQiniuCredential(credential)
	if accessKey == "" || bucket == "" {
		return ""
	}
	endpoint := strings.TrimSpace(qiniuQueryEndpoint)
	if endpoint == "" {
		endpoint = "https://api.qiniu.com/v4/query"
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	queryURL := endpoint + separator + "ak=" + url.QueryEscape(accessKey) + "&bucket=" + url.QueryEscape(bucket)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return ""
	}
	return parseQiniuUploadHost(raw)
}

// parseQiniuCredential 从上传凭证 "AK:base64policy" 中提取 AccessKey 与 bucket。
//
// policy 的 scope 形如 "<bucket>" 或 "<bucket>:<key 前缀>"，bucket 取冒号前部分。
// base64 依次尝试 URL/标准编码的有填充/无填充变体；第二段不是合法 JSON 时
// （真实七牛凭证是 HMAC 签名）返回 accessKey 和空 bucket，调用方据此回退。
func parseQiniuCredential(credential string) (accessKey, bucket string) {
	credential = strings.TrimSpace(credential)
	parts := strings.SplitN(credential, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	accessKey = strings.TrimSpace(parts[0])
	encoded := strings.TrimSpace(parts[1])
	if accessKey == "" || encoded == "" {
		return accessKey, ""
	}
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		var policy qiniuPutPolicy
		if err := json.Unmarshal(decoded, &policy); err != nil {
			continue
		}
		scope := strings.TrimSpace(policy.Scope)
		if scope == "" {
			continue
		}
		if index := strings.Index(scope, ":"); index >= 0 {
			scope = scope[:index]
		}
		if scope = strings.TrimSpace(scope); scope != "" {
			return accessKey, scope
		}
	}
	return accessKey, ""
}

// parseQiniuUploadHost 解析 api.qiniu.com/v4/query 的响应，返回上传域名 URL。
//
// 兼容两种已知响应形状（均以 {"hosts":[...]} 包裹）：
//
//	{"hosts":[{"up":{"domains":["upload.qiniup.com","up.qiniup.com"]}}]}
//	{"hosts":[{"up":{"preferred":["upload.qiniup.com"]}}]}
//
// 域名可以是纯域名（默认补 https://）或带 http(s):// 的完整 URL（便于测试）。
func parseQiniuUploadHost(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return normalizeUploadBase(firstUploadDomain(payload))
}

// firstUploadDomain 在 /v4/query 响应中查找第一个上传域名。
//
// 只遍历文档中定义的结构（hosts/up/data 包裹与域名列表字段），不盲目遍历所有
// 键，避免把 region_id、protocol 之类的字符串误当成域名。
func firstUploadDomain(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"hosts", "up", "data"} {
			if child, ok := typed[key]; ok {
				if domain := firstUploadDomain(child); domain != "" {
					return domain
				}
			}
		}
		for _, key := range []string{"preferred", "domains", "accelerated", "alternative", "old", "domain", "host"} {
			if child, ok := typed[key]; ok {
				if domain := firstUploadDomain(child); domain != "" {
					return domain
				}
			}
		}
	case []any:
		for _, child := range typed {
			if domain := firstUploadDomain(child); domain != "" {
				return domain
			}
		}
	case string:
		return strings.TrimSpace(typed)
	}
	return ""
}

// normalizeUploadBase 把上传域名规范成不带结尾斜杠的 http(s) URL。
func normalizeUploadBase(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return strings.TrimRight(raw, "/")
}

// distributionRoutes 对应 /v1/misc/configure-distribution 的 data 对象。
type distributionRoutes struct {
	AudioURL  string `json:"audioUrl"`
	FileURL   string `json:"fileUrl"`
	ImageURL  string `json:"imageUrl"`
	ServerURL string `json:"serverUrl"`
	VideoURL  string `json:"videoUrl"`
}

// empty 报告是否没有拿到任何可用路由。
func (r *distributionRoutes) empty() bool {
	if r == nil {
		return true
	}
	return r.AudioURL == "" && r.FileURL == "" && r.ImageURL == "" && r.VideoURL == ""
}

// routeFor 返回该上传类型对应的公开访问域名（以 "/" 结尾），不可用时返回空串。
func (r *distributionRoutes) routeFor(kind UploadKind) string {
	if r == nil {
		return ""
	}
	switch kind {
	case UploadImage:
		return validMediaRoute(r.ImageURL)
	case UploadAudio:
		return validMediaRoute(r.AudioURL)
	case UploadVideo:
		return validMediaRoute(r.VideoURL)
	default:
		return validMediaRoute(r.FileURL)
	}
}

// distributionRoutes 获取媒体公开访问（功能）路由。
func (c *Client) distributionRoutes(ctx context.Context, token string) (*distributionRoutes, error) {
	env, err := c.getJSON(ctx, distributionPath, token)
	if err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return nil, &APIError{Msg: "功能路由响应为空"}
	}
	return decodeDistributionRoutes(env.Data)
}

// decodeDistributionRoutes 解析功能路由 data，兼容 data 外面再包一层 data 的写法。
func decodeDistributionRoutes(raw []byte) (*distributionRoutes, error) {
	var routes distributionRoutes
	if err := json.Unmarshal(raw, &routes); err == nil && !routes.empty() {
		return &routes, nil
	}
	var wrapper struct {
		Data *distributionRoutes `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || wrapper.Data == nil || wrapper.Data.empty() {
		return nil, &APIError{Msg: "功能路由响应格式不正确"}
	}
	return wrapper.Data, nil
}

// validMediaRoute 校验并规范化功能路由域名。
func validMediaRoute(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.TrimRight(raw, "/") + "/"
}

// defaultMediaRoute 返回该上传类型的内置回退访问域名。
func defaultMediaRoute(kind UploadKind) string {
	switch kind {
	case UploadImage:
		return defaultImageRoute
	case UploadAudio:
		return defaultAudioRoute
	case UploadVideo:
		return defaultVideoRoute
	default:
		return defaultFileRoute
	}
}

// joinMediaURL 拼接公开访问域名与对象 key。
func joinMediaURL(base, key string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if base == "" || key == "" {
		return ""
	}
	return base + "/" + key
}

// mediaFileURL 计算上传后对象的公开访问 URL。
//
// 功能路由接口失败不影响上传本身：此时按上传类型回退到内置域名。
func (c *Client) mediaFileURL(ctx context.Context, token string, kind UploadKind, key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" {
		return ""
	}
	base := ""
	if routes, err := c.distributionRoutes(ctx, token); err == nil {
		base = routes.routeFor(kind)
	}
	if base == "" {
		base = defaultMediaRoute(kind)
	}
	return joinMediaURL(base, key)
}

// ---------------------------------------------------------------------------
// 发送图片 / 文件消息

// SendImageMessage 发送一条图片消息（content_type=2，MsgContent.image_url 填
// 完整图片 URL），返回生成的消息 ID。
//
// 宽高是可选的 MsgContent.width/height 字段，本方法不读取图片尺寸所以不写入
// （proto 零值即不传）。
//
// Endpoint: POST /v1/msg/send-message (protobuf)。
func (c *Client) SendImageMessage(ctx context.Context, token, chatID string, chatType ChatType, imageURL string) (string, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return "", fmt.Errorf("图片 URL 不能为空")
	}
	content := pbAppendString(nil, contentImageURLField, imageURL)
	return c.sendMediaMessage(ctx, token, chatID, chatType, content, 2) // 2 = ContentType 图片
}

// SendFileMessage 发送一条文件消息（content_type=4，MsgContent 填
// file_name/file_url/file_size），返回生成的消息 ID。
//
// fileName 为空时从 fileURL 的路径中推断；fileSize <= 0 时不传该字段
// （服务端不校验大小）。
//
// Endpoint: POST /v1/msg/send-message (protobuf)。
func (c *Client) SendFileMessage(ctx context.Context, token, chatID string, chatType ChatType, fileName, fileURL string, fileSize int64) (string, error) {
	fileURL = strings.TrimSpace(fileURL)
	if fileURL == "" {
		return "", fmt.Errorf("文件 URL 不能为空")
	}
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		fileName = fileNameFromURL(fileURL)
	}
	if fileName == "" {
		return "", fmt.Errorf("文件名不能为空")
	}
	if fileSize < 0 {
		fileSize = 0
	}
	content := pbAppendString(nil, contentFileNameField, fileName)
	content = pbAppendString(content, contentFileURLField, fileURL)
	if fileSize > 0 {
		content = pbAppendVarint(content, contentFileSizeField, uint64(fileSize))
	}
	return c.sendMediaMessage(ctx, token, chatID, chatType, content, 4) // 4 = ContentType 文件
}

// fileNameFromURL 从 URL 路径中取文件名，URL 无效或没有文件名时返回空串。
func fileNameFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	name := path.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return name
}

// encodeSendMediaMessageRequest 编码带 MsgContent 的 SendMsgRequest：
// msg_id=2、chat_id=3、chat_type=4、content=5、content_type=6。
func encodeSendMediaMessageRequest(msgID, chatID string, chatType ChatType, content []byte, contentType int32) []byte {
	var body []byte
	body = pbAppendString(body, 2, msgID)
	body = pbAppendString(body, 3, chatID)
	body = pbAppendVarint(body, 4, uint64(chatType))
	body = pbAppendBytes(body, 5, content)
	body = pbAppendVarint(body, 6, uint64(int32ToUint32(contentType)))
	return body
}

// sendMediaMessage 发送一条 content 由调用方编码好的消息并返回消息 ID。
func (c *Client) sendMediaMessage(ctx context.Context, token, chatID string, chatType ChatType, content []byte, contentType int32) (string, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return "", fmt.Errorf("会话 ID 不能为空")
	}
	if chatType == ChatTypeUnknown {
		chatType = ChatTypeUser
	}
	msgID, err := newMessageID()
	if err != nil {
		return "", err
	}
	body := encodeSendMediaMessageRequest(msgID, chatID, chatType, content, contentType)
	raw, err := c.postProto(ctx, "/v1/msg/send-message", token, body)
	if err != nil {
		return "", err
	}
	status, err := decodeStatusResponse(raw)
	if err != nil {
		return "", fmt.Errorf("解析发送结果失败: %w", err)
	}
	if status.Code != 1 {
		return "", &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	return msgID, nil
}

// ---------------------------------------------------------------------------
// 头像上传

// UploadAndEditAvatar 上传本地图片作为头像，并调用 POST /v1/user/edit-avatar
// 使其生效，返回头像的公开访问 URL。
func (c *Client) UploadAndEditAvatar(ctx context.Context, token, localPath string) (avatarURL string, err error) {
	_, avatarURL, err = c.UploadFile(ctx, token, UploadImage, localPath)
	if err != nil {
		return "", err
	}
	if editErr := c.EditAvatar(ctx, token, avatarURL); editErr != nil {
		return "", fmt.Errorf("头像已上传但设置失败: %w", editErr)
	}
	return avatarURL, nil
}

// ---------------------------------------------------------------------------
// 他人主页

// UserProfile 是 POST /v1/user/get-user 返回的他人主页信息
// （GetUserResponse.data，../yhchatAPI/src/full.proto）。
type UserProfile struct {
	ID                  string
	Name                string
	AvatarURL           string
	RegisterTime        string // 注册时间，格式 YYYY-MM-DD hh:mm:ss
	IPGeo               string // IP 归属地（国家）
	NameID              uint64
	AvatarID            uint64
	IsVIP               bool
	VIPExpiredTimestamp uint64
	BanID               int64
	BanUntilTimestamp   int64 // 永久封禁时服务端不返回该字段（为 0）
	BanReason           string
	OnlineDay           int32
	ContinuousOnlineDay int32
	DoNotDisturb        bool
	IsBlocked           bool
	MedalNames          []string // 勋章名称列表（按服务端返回顺序）
}

// GetUserProfile 获取他人主页信息。
//
// Endpoint: POST /v1/user/get-user
// 请求体 GetUserRequest{id=2}，响应 GetUserResponse{status=1, data=2}。
// 即使没有有效 token 也可以查询（文档允许 token 随意填写），服务端错误码如
// 4402（用户不存在）、7100（用户被封禁）通过 *APIError 返回。
func (c *Client) GetUserProfile(ctx context.Context, token, userID string) (*UserProfile, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("用户 ID 不能为空")
	}
	body := pbAppendString(nil, 2, userID)
	raw, err := c.postProto(ctx, "/v1/user/get-user", token, body)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, &APIError{Msg: "服务器返回的用户信息为空"}
	}
	status, profile, err := decodeGetUserResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析用户信息失败: %w", err)
	}
	if status.Code != 1 {
		return nil, &APIError{Code: int(status.Code), Msg: status.Msg}
	}
	if profile == nil {
		return nil, &APIError{Msg: "服务器返回的用户信息为空"}
	}
	return profile, nil
}

// GetUserResponse 字段号。
const (
	getUserStatusField = 1
	getUserDataField   = 2
)

// GetUserResponse.Data 字段号。
const (
	userProfileIDField                  = 1
	userProfileNameField                = 2
	userProfileNameIDField              = 3
	userProfileAvatarURLField           = 4
	userProfileAvatarIDField            = 5
	userProfileMedalField               = 6
	userProfileRegisterTimeField        = 7
	userProfileDoNotDisturbField        = 8
	userProfileBanIDField               = 9
	userProfileBanUntilTimestampField   = 10
	userProfileOnlineDayField           = 11
	userProfileContinuousOnlineDayField = 12
	userProfileIsVIPField               = 13
	userProfileVIPExpiredTimestampField = 14
	userProfileIsBlockedField           = 16
	userProfileIPGeoField               = 20
	userProfileBanReasonField           = 21

	// MedalInfo 中只用到名称（name=2）；勋章 ID=1、sort=5 直接跳过。
	medalInfoNameField = 2
)

// decodeGetUserResponse 解析 GetUserResponse{status=1, data=2}。
func decodeGetUserResponse(b []byte) (Status, *UserProfile, error) {
	var (
		status  Status
		profile *UserProfile
	)
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return status, nil, err
		}
		b = next
		switch num {
		case getUserStatusField:
			payload, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("status: %w", err)
			}
			b = rest
			if !ok {
				continue
			}
			decoded, err := decodeStatus(payload)
			if err != nil {
				return status, nil, fmt.Errorf("status: %w", err)
			}
			status = decoded
		case getUserDataField:
			payload, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return status, nil, fmt.Errorf("data: %w", err)
			}
			b = rest
			if !ok {
				continue
			}
			decoded := &UserProfile{}
			if err := decodeUserProfileData(payload, decoded); err != nil {
				return status, nil, fmt.Errorf("data: %w", err)
			}
			profile = decoded
		default:
			rest, err := skipField(num, typ, b)
			if err != nil {
				return status, nil, err
			}
			b = rest
		}
	}
	return status, profile, nil
}

// decodeUserProfileData 解析 GetUserResponse.Data（只保留 TUI 需要的字段）。
func decodeUserProfileData(b []byte, profile *UserProfile) error {
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return err
		}
		b = next
		switch num {
		case userProfileIDField:
			if err := decodeStringField(num, typ, &b, &profile.ID); err != nil {
				return fmt.Errorf("id: %w", err)
			}
		case userProfileNameField:
			if err := decodeStringField(num, typ, &b, &profile.Name); err != nil {
				return fmt.Errorf("name: %w", err)
			}
		case userProfileNameIDField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("name_id: %w", err)
			}
			b = rest
			if ok {
				profile.NameID = value
			}
		case userProfileAvatarURLField:
			if err := decodeStringField(num, typ, &b, &profile.AvatarURL); err != nil {
				return fmt.Errorf("avatar_url: %w", err)
			}
		case userProfileAvatarIDField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("avatar_id: %w", err)
			}
			b = rest
			if ok {
				profile.AvatarID = value
			}
		case userProfileMedalField:
			payload, rest, ok, err := consumeBytesField(num, typ, b)
			if err != nil {
				return fmt.Errorf("medal: %w", err)
			}
			b = rest
			if !ok {
				continue
			}
			name, err := decodeMedalName(payload)
			if err != nil {
				return fmt.Errorf("medal: %w", err)
			}
			if name != "" {
				profile.MedalNames = append(profile.MedalNames, name)
			}
		case userProfileRegisterTimeField:
			if err := decodeStringField(num, typ, &b, &profile.RegisterTime); err != nil {
				return fmt.Errorf("register_time: %w", err)
			}
		case userProfileDoNotDisturbField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("do_not_disturb: %w", err)
			}
			b = rest
			if ok {
				profile.DoNotDisturb = value != 0
			}
		case userProfileBanIDField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("ban_id: %w", err)
			}
			b = rest
			if ok {
				profile.BanID = int64(value)
			}
		case userProfileBanUntilTimestampField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("ban_until_timestamp: %w", err)
			}
			b = rest
			if ok {
				profile.BanUntilTimestamp = int64(value)
			}
		case userProfileOnlineDayField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("online_day: %w", err)
			}
			b = rest
			if ok {
				profile.OnlineDay = int32(value)
			}
		case userProfileContinuousOnlineDayField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("continuous_online_day: %w", err)
			}
			b = rest
			if ok {
				profile.ContinuousOnlineDay = int32(value)
			}
		case userProfileIsVIPField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("is_vip: %w", err)
			}
			b = rest
			if ok {
				profile.IsVIP = value != 0
			}
		case userProfileVIPExpiredTimestampField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("vip_expired_timestamp: %w", err)
			}
			b = rest
			if ok {
				profile.VIPExpiredTimestamp = value
			}
		case userProfileIsBlockedField:
			value, rest, ok, err := consumeVarintField(num, typ, b)
			if err != nil {
				return fmt.Errorf("is_blocked: %w", err)
			}
			b = rest
			if ok {
				profile.IsBlocked = value != 0
			}
		case userProfileIPGeoField:
			if err := decodeStringField(num, typ, &b, &profile.IPGeo); err != nil {
				return fmt.Errorf("ip_geo: %w", err)
			}
		case userProfileBanReasonField:
			if err := decodeStringField(num, typ, &b, &profile.BanReason); err != nil {
				return fmt.Errorf("ban_reason: %w", err)
			}
		default:
			rest, err := skipField(num, typ, b)
			if err != nil {
				return err
			}
			b = rest
		}
	}
	return nil
}

// decodeMedalName 解析 MedalInfo 并返回勋章名称（其余字段跳过）。
func decodeMedalName(b []byte) (string, error) {
	var name string
	for len(b) > 0 {
		num, typ, next, err := consumeTag(b)
		if err != nil {
			return "", err
		}
		b = next
		if num == medalInfoNameField {
			if err := decodeStringField(num, typ, &b, &name); err != nil {
				return "", fmt.Errorf("name: %w", err)
			}
			continue
		}
		rest, err := skipField(num, typ, b)
		if err != nil {
			return "", err
		}
		b = rest
	}
	return name, nil
}
