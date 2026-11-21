package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotatingWriterRotatesWhileRunning verifies that a single long-running
// session rotates the log once it reaches the configured size.
func TestRotatingWriterRotatesWhileRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	logFile, err := openLog(path, 160, 2)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}
	defer func() { _ = logFile.Close() }()

	for i := 0; i < 20; i++ {
		line := fmt.Sprintf("line %02d %s\n", i, strings.Repeat("x", 40))
		if _, err := fmt.Fprint(logFile, line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Size() > 160 {
		t.Fatalf("active log size = %v (err %v), want <= 160", info, err)
	}
	for _, suffix := range []string{".1", ".2"} {
		if _, err := os.Stat(path + suffix); err != nil {
			t.Fatalf("rotated file %s missing: %v", suffix, err)
		}
	}
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Fatal("found .3 backup although keep=2")
	}
}

// TestRotatingWriterRotatesOversizedStartupFile checks that a huge log left
// over from a previous run is rotated when logging starts.
func TestRotatingWriterRotatesOversizedStartupFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("old\n", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	logFile, err := openLog(path, 128, 3)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}
	defer func() { _ = logFile.Close() }()
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("new log size = %v (err %v), want 0", info, err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("legacy log was not rotated: %v", err)
	}
}

// TestConfigFromEnvOverrides verifies the environment overrides used by Init.
func TestConfigFromEnvOverrides(t *testing.T) {
	t.Setenv("YHCHAT_LOG_MAX_MB", "12")
	t.Setenv("YHCHAT_LOG_KEEP", "7")
	maxBytes, keep := configFromEnv()
	if maxBytes != 12<<20 || keep != 7 {
		t.Fatalf("config = (%d, %d), want (%d, 7)", maxBytes, keep, 12<<20)
	}
	t.Setenv("YHCHAT_LOG_MAX_MB", "0")
	t.Setenv("YHCHAT_LOG_KEEP", "999")
	maxBytes, keep = configFromEnv()
	if maxBytes != defaultMax || keep != defaultKeep {
		t.Fatalf("invalid env should be ignored, got (%d, %d)", maxBytes, keep)
	}
}

// TestRotateFilesNoExistingBackups covers the first rotation.
func TestRotateFilesNoExistingBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	rotateFiles(path, 3)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("active log should be moved, stat err = %v", err)
	}
	if got, err := os.ReadFile(path + ".1"); err != nil || string(got) != "content" {
		t.Fatalf("backup = %q (err %v)", got, err)
	}
}
