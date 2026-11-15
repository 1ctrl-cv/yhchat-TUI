// Package cache stores downloaded media (images/videos/avatars) under
// ~/.yhchat-tui/cache so repeated views do not re-download the same file.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	dirName     = ".yhchat-tui"
	cacheDir    = "cache"
	maxImage    = 30 << 20  // 30 MiB
	maxVideo    = 200 << 20 // 200 MiB
	httpTimeout = 60 * time.Second
)

// Dir returns the cache directory (~/.yhchat-tui/cache).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, dirName, cacheDir)
}

// EnsureDir creates the cache directory if it does not exist.
func EnsureDir() (string, error) {
	dir := Dir()
	if dir == "" {
		return "", errors.New("无法定位用户主目录")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建缓存目录失败: %w", err)
	}
	return dir, nil
}

// PathForURL returns the deterministic cache path for a remote media URL.
func PathForURL(rawURL string) (string, error) {
	dir, err := EnsureDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(rawURL))
	name := hex.EncodeToString(sum[:])[:24] + extension(rawURL)
	return filepath.Join(dir, name), nil
}

// extension keeps a short, safe suffix from the URL path.
func extension(rawURL string) string {
	clean := rawURL
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	ext := strings.ToLower(filepath.Ext(clean))
	if len(ext) < 2 || len(ext) > 8 {
		return ".bin"
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ".bin"
		}
	}
	return ext
}

// Exists reports whether the media for rawURL is already cached.
func Exists(rawURL string) (string, bool) {
	path, err := PathForURL(rawURL)
	if err != nil {
		return "", false
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
		return path, true
	}
	return path, false
}

// DownloadToCache downloads a media URL into the cache and returns the local
// path. Existing files are returned without downloading again. The content is
// written atomically; oversized responses are rejected.
func DownloadToCache(ctx context.Context, client *http.Client, rawURL string, video bool) (string, error) {
	if strings.TrimSpace(rawURL) == "" {
		return "", errors.New("媒体 URL 为空")
	}
	path, err := PathForURL(rawURL)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
		return path, nil
	}
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建下载请求失败: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载媒体失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载媒体失败: HTTP %d", resp.StatusCode)
	}
	limit := int64(maxImage)
	if video {
		limit = maxVideo
	}
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("创建缓存文件失败: %w", err)
	}
	n, copyErr := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("下载媒体失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("写入缓存失败: %w", closeErr)
	}
	if n > limit {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("媒体文件超过缓存上限（%d MB）", limit>>20)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("保存缓存失败: %w", err)
	}
	return path, nil
}
