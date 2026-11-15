// Package logging writes yhchat-tui logs to ~/.yhchat-tui/log/ with simple size
// based rotation. Logs never contain tokens, message bodies or other secrets.
//
// When the primary directory is not writable (for example a sandboxed terminal
// that can only write the workspace), logging falls back to the system temp
// directory so the application still starts and can be diagnosed.
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
	mu         sync.Mutex
	logger     *log.Logger
	logFile    *os.File
	enabled    bool
	debugLog   bool
	logPath    string
	fallback   bool
	initErrMsg string
)

// Dir returns the primary log directory (~/.yhchat-tui/log).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, dirName, logDirName)
}

// Path returns the log file path in use (empty when logging is disabled).
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return logPath
}

// IsFallback reports whether logging is using the temporary fallback directory.
func IsFallback() bool {
	mu.Lock()
	defer mu.Unlock()
	return fallback
}

// Init opens the log file. When the primary directory cannot be written it
// falls back to <temp>/yhchat-tui/log. When debug is true, or YHCHAT_DEBUG=1 is
// set, detailed HTTP/WS diagnostics are written as well.
func Init(debug bool) error {
	primary := filepath.Join(Dir(), logName)
	path, fallbackUsed, err := openLog(primary)
	if err != nil {
		fallbackPath := filepath.Join(os.TempDir(), "yhchat-tui", logDirName, logName)
		path, fallbackUsed, err = openLog(fallbackPath)
		if err != nil {
			return fmt.Errorf("无法创建日志文件（%s 与临时目录均不可写）: %w", primary, err)
		}
		initErrMsg = fmt.Sprintf("日志目录 %s 不可写，已改用临时目录日志", Dir())
	}

	mu.Lock()
	logPath = path
	fallback = fallbackUsed
	enabled = true
	debugLog = debug || os.Getenv("YHCHAT_DEBUG") == "1"
	mu.Unlock()

	Info("日志初始化完成，文件: %s", path)
	if initErrMsg != "" {
		Info("%s", initErrMsg)
	}
	return nil
}

// openLog creates the directory, rotates old logs and opens the file.
func openLog(path string) (string, bool, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, err
	}
	rotate(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", false, err
	}
	mu.Lock()
	defer mu.Unlock()
	logFile = file
	logger = log.New(file, "", log.LstdFlags|log.Lmicroseconds)
	return path, strings.HasPrefix(dir, os.TempDir()), nil
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
	if !debug {
		return
	}
	write("DEBUG", format, args...)
}

// Error logs an error.
func Error(format string, args ...any) { write("ERROR", format, args...) }

// Duration formats a duration for log lines.
func Duration(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}
