// Package config persists yhchat-tui settings (login token, device id and
// convenience preferences) in the platform's per-user config directory.
//
// Locations:
//   - Windows: %AppData%\yhchat-tui\config.json
//   - Linux:   $XDG_CONFIG_HOME/yhchat-tui/config.json (~/.config/... by default)
//   - macOS:   ~/Library/Application Support/yhchat-tui/config.json
//
// Set YHCHAT_CONFIG_DIR to override the directory (useful for testing).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AppName is used to build the config directory name.
const AppName = "yhchat-tui"

// Config is the on-disk configuration.
type Config struct {
	Token    string `json:"token,omitempty"`
	DeviceID string `json:"deviceId,omitempty"`

	// Preferences remembered between runs.
	LoginMethod string `json:"loginMethod,omitempty"` // "email" or "sms"
	LastEmail   string `json:"lastEmail,omitempty"`
	LastMobile  string `json:"lastMobile,omitempty"`
}

// Store owns the config file location and its loaded contents.
type Store struct {
	Path string
	Data Config
}

// DefaultPath returns the config file path for the current user.
func DefaultPath() (string, error) {
	if dir := os.Getenv("YHCHAT_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("定位用户配置目录失败: %w", err)
	}
	return filepath.Join(base, AppName, "config.json"), nil
}

// Load reads the config file. A missing file yields an empty config. A corrupt
// file is moved aside (config.json.bad) and treated as empty, so the TUI can
// always start. The device id is generated on first use.
func Load(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	s := &Store{Path: path}
	var warn error

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// first run
	case err != nil:
		return nil, fmt.Errorf("读取配置失败: %w", err)
	default:
		if len(raw) > 0 {
			if jsErr := json.Unmarshal(raw, &s.Data); jsErr != nil {
				_ = os.WriteFile(path+".bad", raw, 0o600)
				s.Data = Config{}
				warn = fmt.Errorf("配置文件已损坏，已备份为 %s.bad: %w", path, jsErr)
			}
		}
	}

	// Stable device id across restarts; also used by SMS login.
	if s.Data.DeviceID == "" {
		id, genErr := newDeviceID()
		if genErr != nil {
			return nil, genErr
		}
		s.Data.DeviceID = id
		if saveErr := s.Save(); saveErr != nil {
			return nil, saveErr
		}
	}
	return s, warn
}

// Save atomically writes the config file (0600) creating parent directories.
func (s *Store) Save() error {
	if s.Path == "" {
		return errors.New("config: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	raw, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存配置失败: %w", err)
	}
	return nil
}

// ClearToken removes the stored login token and saves the file.
func (s *Store) ClearToken() error {
	s.Data.Token = ""
	return s.Save()
}

// newDeviceID returns a random 32 hex-character device identifier accepted by
// the Yunhu API (it only requires a stable, custom string).
func newDeviceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("生成设备 ID 失败: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
