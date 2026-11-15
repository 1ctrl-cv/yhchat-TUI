// Package logging writes yhchat-tui logs to ~/.yhchat-tui/log/ with simple size
// based rotation. Logs never contain tokens, message bodies or other secrets.
package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	dirName    = ".yhchat-tui"
	logDirName = "log"
	logName    = "yhchat-tui.log"
	maxSize    = 5 << 20 // 5 MiB
	keepFiles  = 3
)

var (
	mu       sync.Mutex
	logger   *log.Logger
	logFile  *os.File
	enabled  bool
	debugLog bool
)

// Dir returns the log directory (~/.yhchat-tui/log).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, dirName, logDirName)
}

// Init opens the log file. When debug is true, debug-level HTTP/WS details are
// also written (set YHCHAT_DEBUG=1 to enable).
func Init(debug bool) error {
	dir := Dir()
	if dir == "" {
		return fmt.Errorf("无法定位用户主目录")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	path := filepath.Join(dir, logName)
	rotate(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	logFile = file
	logger = log.New(file, "", log.LstdFlags|log.Lmicroseconds)
	enabled = true
	debugLog = debug || os.Getenv("YHCHAT_DEBUG") == "1"
	Info("日志初始化完成，文件: %s", path)
	return nil
}

// Close flushes and closes the log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	logger = nil
	enabled = false
}

// rotate keeps at most keepFiles old logs of maxSize each.
func rotate(path string) {
	if info, err := os.Stat(path); err != nil || info.Size() < maxSize {
		return
	}
	for i := keepFiles - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", path, i)
		newPath := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
		}
	}
	_ = os.Rename(path, path+".1")
}

func write(level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if !enabled || logger == nil {
		return
	}
	line := fmt.Sprintf(format, args...)
	logger.Printf("[%s] %s", level, strings.TrimRight(line, "\n"))
}

// Info logs a normal lifecycle event.
func Info(format string, args ...any) { write("INFO", format, args...) }

// Debug logs detailed diagnostics when debug logging is enabled.
func Debug(format string, args ...any) {
	mu.Lock()
	debug := debugLog
	mu.Unlock()
	if debug {
		write("DEBUG", format, args...)
	}
}

// Error logs an error.
func Error(format string, args ...any) { write("ERROR", format, args...) }

// Duration formats a duration for log lines.
func Duration(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}
