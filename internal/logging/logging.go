// Package logging writes yhchat-tui logs to ~/.yhchat-tui/log/.
//
// Rotation happens both by day and by size, and all parameters live in the
// constants below (no environment variables are needed):
//
//   - one file per day:  yhchat-tui-2026-11-21.log
//   - same-day size rotation: yhchat-tui-2026-11-21.1.log, .2.log, ...
//   - files older than keepLogDays are deleted automatically
//
// Logs never contain tokens, message bodies or other secrets.
//
// When the primary directory is not writable (for example a sandboxed terminal
// that can only write the workspace), logging falls back to the system temp
// directory or the current directory so the application still starts.
package logging

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dirName    = ".yhchat-tui"
	logDirName = "log"
	logBase    = "yhchat-tui"

	// maxLogSize is the size at which the active daily file is rotated.
	maxLogSize = 5 << 20 // 5 MiB
	// keepLogDays is how many calendar days of logs are kept (today included).
	keepLogDays = 7
	// keepSizeBackups is how many same-day size-rotated backups are kept.
	keepSizeBackups = 5

	dayFormat = "2006-01-02"
)

var (
	mu       sync.Mutex
	logger   *log.Logger
	writer   *rotatingFile
	enabled  bool
	debugLog bool
	logPath  string
	fallback bool
)

// Dir returns the primary log directory (~/.yhchat-tui/log).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, dirName, logDirName)
}

// Path returns the active log file path (empty when logging is disabled).
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return logPath
}

// IsFallback reports whether logging is using a fallback directory.
func IsFallback() bool {
	mu.Lock()
	defer mu.Unlock()
	return fallback
}

// Init opens the log file. When the primary directory cannot be written it
// falls back to <temp>/yhchat-tui/log and then to <cwd>/.yhchat-tui-log. When
// debug is true, or YHCHAT_DEBUG=1 is set, detailed HTTP/WS diagnostics are
// written as well.
func Init(debug bool) error {
	candidates := []string{Dir()}
	candidates = append(candidates, filepath.Join(os.TempDir(), "yhchat-tui", logDirName))
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, ".yhchat-tui-log"))
	}

	var lastErr error
	for i, dir := range candidates {
		if dir == "" {
			lastErr = errors.New("无法定位用户主目录")
			continue
		}
		opened, err := openLog(dir)
		if err != nil {
			lastErr = err
			continue
		}
		mu.Lock()
		writer = opened
		logPath = opened.Path()
		fallback = i > 0
		enabled = true
		debugLog = debug || os.Getenv("YHCHAT_DEBUG") == "1"
		logger = log.New(opened, "", log.LstdFlags|log.Lmicroseconds)
		mu.Unlock()

		if i > 0 {
			Info("日志目录 %s 不可写，已改用 %s", Dir(), dir)
		}
		Info("日志初始化完成，文件: %s", opened.Path())
		return nil
	}
	return fmt.Errorf("无法创建日志文件（主目录、临时目录、当前目录均不可写）: %w", lastErr)
}

// openLog creates the log directory (including rotation of old files) and
// returns a writer for the current day.
func openLog(dir string) (*rotatingFile, error) {
	return newRotatingFile(dir, maxLogSize, keepLogDays, keepSizeBackups, time.Now)
}

// newRotatingFile is the testable constructor with explicit limits and clock.
func newRotatingFile(dir string, maxBytes int64, keepDays, keepBackups int, now func() time.Time) (*rotatingFile, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = maxLogSize
	}
	if keepDays <= 0 {
		keepDays = keepLogDays
	}
	if keepBackups <= 0 {
		keepBackups = keepSizeBackups
	}
	w := &rotatingFile{
		dir:         dir,
		max:         maxBytes,
		keepDays:    keepDays,
		keepBackups: keepBackups,
		now:         now,
	}
	migrateLegacyFiles(dir)
	if err := w.openActiveLocked(now()); err != nil {
		return nil, err
	}
	w.cleanupLocked(now())
	return w, nil
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

// rotatingFile is an io.Writer that switches to a new dated file at midnight and
// rotates the current file when it would exceed max bytes.
type rotatingFile struct {
	mu          sync.Mutex
	dir         string
	file        *os.File
	path        string
	date        string
	size        int64
	max         int64
	keepDays    int
	keepBackups int
	now         func() time.Time
}

// Path returns the active file path.
func (w *rotatingFile) Path() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.path
}

// Write appends p, switching files when the day changes and rotating before the
// active file exceeds its size limit.
func (w *rotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, errors.New("日志文件已关闭")
	}
	now := w.now()
	if date := now.Format(dayFormat); date != w.date {
		if err := w.file.Close(); err != nil {
			return 0, err
		}
		if err := w.openActiveLocked(now); err != nil {
			return 0, err
		}
		w.cleanupLocked(now)
	}
	if w.size > 0 && w.size+int64(len(p)) > w.max {
		if err := w.rotateSizeLocked(); err != nil {
			return 0, err
		}
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

// openActiveLocked opens (or appends to) the log file of the given time's day.
func (w *rotatingFile) openActiveLocked(now time.Time) error {
	date := now.Format(dayFormat)
	path := activeLogPath(w.dir, date)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.path = path
	w.date = date
	w.size = info.Size()
	return nil
}

// rotateSizeLocked moves the active file to a numbered backup and starts a new
// active file for the same day.
func (w *rotatingFile) rotateSizeLocked() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}
	_ = os.Remove(backupLogPath(w.dir, w.date, w.keepBackups))
	for i := w.keepBackups - 1; i >= 1; i-- {
		oldPath := backupLogPath(w.dir, w.date, i)
		newPath := backupLogPath(w.dir, w.date, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
		}
	}
	if err := os.Rename(activeLogPath(w.dir, w.date), backupLogPath(w.dir, w.date, 1)); err != nil {
		// If renaming fails, keep appending rather than losing log lines.
		file, openErr := os.OpenFile(activeLogPath(w.dir, w.date), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if openErr != nil {
			return openErr
		}
		w.file = file
		return nil
	}
	file, err := os.OpenFile(activeLogPath(w.dir, w.date), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w.file = file
	w.size = 0
	return nil
}

// cleanupLocked deletes daily files older than keepDays and trims same-day
// backups above keepBackups.
func (w *rotatingFile) cleanupLocked(now time.Time) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -(w.keepDays - 1))
	backupsByDate := map[string][]int{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		date, backup, ok := parseLogName(entry.Name())
		if !ok {
			continue
		}
		parsed, parseErr := time.ParseInLocation(dayFormat, date, now.Location())
		if parseErr != nil {
			continue
		}
		if parsed.Before(cutoff) {
			_ = os.Remove(filepath.Join(w.dir, entry.Name()))
			continue
		}
		if backup > 0 {
			backupsByDate[date] = append(backupsByDate[date], backup)
		}
	}
	for date, backups := range backupsByDate {
		for _, backup := range backups {
			if backup > w.keepBackups {
				_ = os.Remove(backupLogPath(w.dir, date, backup))
			}
		}
	}
}

func activeLogPath(dir, date string) string {
	return filepath.Join(dir, fmt.Sprintf("%s-%s.log", logBase, date))
}

func backupLogPath(dir, date string, index int) string {
	if index <= 0 {
		return activeLogPath(dir, date)
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s.%d.log", logBase, date, index))
}

var logNamePattern = regexp.MustCompile(`^` + logBase + `-(\d{4}-\d{2}-\d{2})(?:\.(\d+))?\.log$`)

// parseLogName extracts the date and backup index from a log file name.
func parseLogName(name string) (date string, backup int, ok bool) {
	match := logNamePattern.FindStringSubmatch(name)
	if match == nil {
		return "", 0, false
	}
	if match[2] != "" {
		index, err := strconv.Atoi(match[2])
		if err != nil {
			return "", 0, false
		}
		backup = index
	}
	return match[1], backup, true
}

// migrateLegacyFiles renames logs from the old single-file scheme
// (yhchat-tui.log / yhchat-tui.log.1) to the dated scheme, best effort.
func migrateLegacyFiles(dir string) {
	for _, legacy := range []string{logBase + ".log", logBase + ".log.1", logBase + ".log.2", logBase + ".log.3"} {
		path := filepath.Join(dir, legacy)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		date := info.ModTime().Format(dayFormat)
		if _, _, ok := parseLogName(legacy); ok {
			continue
		}
		target := activeLogPath(dir, date)
		if strings.HasSuffix(legacy, ".log.1") {
			target = backupLogPath(dir, date, 1)
		} else if strings.HasSuffix(legacy, ".log.2") {
			target = backupLogPath(dir, date, 2)
		} else if strings.HasSuffix(legacy, ".log.3") {
			target = backupLogPath(dir, date, 3)
		}
		if _, statErr := os.Stat(target); statErr == nil {
			continue
		}
		_ = os.Rename(path, target)
	}
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
