package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSizeRotationWhileRunning verifies that the active daily file is rotated
// once it reaches the configured size, without waiting for a restart.
func TestSizeRotationWhileRunning(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.Local)
	logFile, err := newRotatingFile(dir, 160, 7, 2, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newRotatingFile: %v", err)
	}
	defer func() { _ = logFile.Close() }()

	for i := 0; i < 20; i++ {
		line := fmt.Sprintf("line %02d %s\n", i, strings.Repeat("x", 40))
		if _, err := fmt.Fprint(logFile, line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	active := activeLogPath(dir, "2026-11-21")
	if info, err := os.Stat(active); err != nil || info.Size() > 160 {
		t.Fatalf("active log size = %v (err %v), want <= 160", info, err)
	}
	for _, index := range []int{1, 2} {
		if _, err := os.Stat(backupLogPath(dir, "2026-11-21", index)); err != nil {
			t.Fatalf("backup .%d missing: %v", index, err)
		}
	}
	if _, err := os.Stat(backupLogPath(dir, "2026-11-21", 3)); err == nil {
		t.Fatal("found .3 backup although keepBackups=2")
	}
}

// TestDailyRotation verifies that writes after midnight go into a new dated file.
func TestDailyRotation(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 23, 59, 0, 0, time.Local)
	logFile, err := newRotatingFile(dir, 1<<20, 7, 3, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newRotatingFile: %v", err)
	}
	defer func() { _ = logFile.Close() }()

	if _, err := fmt.Fprintln(logFile, "day-one"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute) // crosses midnight into 2026-11-22
	if _, err := fmt.Fprintln(logFile, "day-two"); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "yhchat-tui-2026-11-21.log")
	second := filepath.Join(dir, "yhchat-tui-2026-11-22.log")
	if data, err := os.ReadFile(first); err != nil || !strings.Contains(string(data), "day-one") {
		t.Fatalf("first day log = %q (err %v)", data, err)
	}
	if data, err := os.ReadFile(second); err != nil || !strings.Contains(string(data), "day-two") {
		t.Fatalf("second day log = %q (err %v)", data, err)
	}
}

// TestRetentionCleanup verifies old daily files are removed but recent ones stay.
func TestRetentionCleanup(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.Local)
	oldDate := now.AddDate(0, 0, -10).Format(dayFormat)
	recentDate := now.AddDate(0, 0, -2).Format(dayFormat)
	oldPath := activeLogPath(dir, oldDate)
	recentPath := activeLogPath(dir, recentDate)
	for _, path := range []string{oldPath, recentPath} {
		if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logFile, err := newRotatingFile(dir, 1<<20, 7, 3, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newRotatingFile: %v", err)
	}
	defer func() { _ = logFile.Close() }()
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old log should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Fatalf("recent log should be kept: %v", err)
	}
}

// TestSizeBackupLimit verifies cleanup removes surplus numbered backups.
func TestSizeBackupLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.Local)
	date := now.Format(dayFormat)
	for _, index := range []int{6, 7} {
		if err := os.WriteFile(backupLogPath(dir, date, index), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logFile, err := newRotatingFile(dir, 1<<20, 7, 3, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newRotatingFile: %v", err)
	}
	defer func() { _ = logFile.Close() }()
	for _, index := range []int{6, 7} {
		if _, err := os.Stat(backupLogPath(dir, date, index)); !os.IsNotExist(err) {
			t.Fatalf("surplus backup .%d should be removed", index)
		}
	}
}

// TestLegacyMigration verifies the old single-file scheme is renamed.
func TestLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.Local)
	legacy := filepath.Join(dir, "yhchat-tui.log")
	if err := os.WriteFile(legacy, []byte("legacy-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mtime := now.AddDate(0, 0, -1)
	if err := os.Chtimes(legacy, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	logFile, err := newRotatingFile(dir, 1<<20, 7, 3, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newRotatingFile: %v", err)
	}
	defer func() { _ = logFile.Close() }()
	migrated := activeLogPath(dir, mtime.Format(dayFormat))
	if data, err := os.ReadFile(migrated); err != nil || !strings.Contains(string(data), "legacy-line") {
		t.Fatalf("migrated log = %q (err %v)", data, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy log should be renamed, stat err = %v", err)
	}
}

// TestParseLogName covers the dated naming scheme parser.
func TestParseLogName(t *testing.T) {
	date, backup, ok := parseLogName("yhchat-tui-2026-11-21.log")
	if !ok || date != "2026-11-21" || backup != 0 {
		t.Fatalf("active name parsed as (%q, %d, %v)", date, backup, ok)
	}
	date, backup, ok = parseLogName("yhchat-tui-2026-11-21.4.log")
	if !ok || date != "2026-11-21" || backup != 4 {
		t.Fatalf("backup name parsed as (%q, %d, %v)", date, backup, ok)
	}
	if _, _, ok := parseLogName("other.log"); ok {
		t.Fatal("unrelated file name should not parse")
	}
}
