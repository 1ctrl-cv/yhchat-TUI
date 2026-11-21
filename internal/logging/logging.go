// Package logging writes yhchat-tui logs to ~/.yhchat-tui/log/ with size based
// rotation that also happens while the program is running. Logs never contain
// tokens, message bodies or other secrets.
//
// When the primary directory is not writable (for example a sandboxed terminal
// that can only write the workspace), logging falls back to the system temp
// directory so the application still starts and can be diagnosed.
package logging

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dirName     = ".yhchat-tui"
	logDirName  = "log"
	logName     = "yhchat-tui.log"
	defaultMax  = 5 << 20 // 5 MiB
	defaultKeep = 3       // rotated backups to keep
)

var (
	mu         sync.Mutex
	logger     *log.Logger
	writer     *rotatingFile
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

// configFromEnv returns the rotation limits, allowing YHCHAT_LOG_MAX_MB and
// YHCHAT_LOG_KEEP overrides for debugging or unusual environments.
func configFromEnv() (maxBytes int64, keep int) {
	maxBytes = defaultMax
	keep = defaultKeep
	if raw := strings.TrimSpace(os.Getenv("YHCHAT_LOG_MAX_MB")); raw != "" {
		if mb, err := strconv.Atoi(raw); err == nil && mb >= 1 && mb <= 1024 {
			maxBytes = int64(mb) << 20
		}
	}
	if raw := strings.TrimSpace(os.Getenv("YHCHAT_LOG_KEEP")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 20 {
			keep = n
		}
	}
	return maxBytes, keep
}

// Init opens the log file. When the primary directory cannot be written it
// falls back to <temp>/yhchat-tui/log. When debug is true, or YHCHAT_DEBUG=1 is
// set, detailed HTTP/WS diagnostics are written as well.
func Init(debug bool) error {
	primary := filepath.Join(Dir(), logName)
	tempPath := filepath.Join(os.TempDir(), "yhchat-tui", logDirName, logName)
	cwdPath := ""
	if cwd, err := os.Getwd(); err == nil {
		cwdPath = filepath.Join(cwd, "yhchat-tui.log")
	}
	candidates := []string{primary, tempPath}
	if cwdPath != "" {
		candidates = append(candidates, cwdPath)
	}
	maxBytes, keep := configFromEnv()

	var lastErr error
	for i, candidate := range candidates {
		opened, err := openLog(candidate, maxBytes, keep)
		if err != nil {
			lastErr = err
			continue
		}
		mu.Lock()
		logPath = candidate
		fallback = i > 0
		enabled = true
		debugLog = debug || os.Getenv("YHCHAT_DEBUG") == "1"
		writer = opened
		logger = log.New(opened, "", log.LstdFlags|log.Lmicroseconds)
		mu.Unlock()
		if i > 0 {
			initErrMsg = fmt.Sprintf("日志目录 %s 不可写，已改用 %s", Dir(), candidate)
		}
		Info("日志初始化完成，文件: %s", candidate)
		if initErrMsg != "" {
			Info("%s", initErrMsg)
		}
		return nil
	}
	return fmt.Errorf("无法创建日志文件（主目录、临时目录、当前目录均不可写）: %w", lastErr)
}

// openLog creates the directory and opens the rotating log file.
func openLog(path string, maxBytes int64, keep int) (*rotatingFile, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Rotate an already oversized file left over from a previous run.
	if info, err := os.Stat(path); err == nil && info.Size() >= maxBytes {
		rotateFiles(path, keep)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	return &rotatingFile{path: path, file: file, size: info.Size(), max: maxBytes, keep: keep}, nil
}

// Close flushes and closes the log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if writer != nil {
		_ = writer.Close()
		writer = nil
	}
	logger = nil
	enabled = false
}

// rotatingFile is an io.Writer that rotates the underlying file once it would
// exceed max bytes. Rotation happens synchronously on write, so long-running
// sessions never leave an unbounded log file.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
	max  int64
	keep int
}

// Write appends p, rotating first when the file would grow past its limit.
func (w *rotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, errors.New("日志文件已关闭")
	}
	if w.size > 0 && w.size+int64(len(p)) > w.max {
		if err := w.file.Close(); err != nil {
			return 0, err
		}
		rotateFiles(w.path, w.keep)
		file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			w.file = nil
			return 0, err
		}
		w.file = file
		w.size = 0
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the underlying file.
func (w *rotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// rotateFiles shifts yhchat-tui.log -> .1 -> .2 ... and drops the oldest
// backup so at most keep rotated files remain.
func rotateFiles(path string, keep int) {
	if keep < 1 {
		keep = 1
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, keep))
	for i := keep - 1; i >= 1; i-- {
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
