package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/config"
)

// snapshotter writes/compares plain-text renderings of the TUI views. Run
// `go test ./internal/tui -run TestViewSnapshots -update` (UPDATE_SNAPSHOTS=1)
// to refresh them after intentional layout changes.
type snapshotter struct {
	t      *testing.T
	update bool
}

func newSnapshotter(t *testing.T) *snapshotter {
	t.Helper()
	return &snapshotter{t: t, update: os.Getenv("UPDATE_SNAPSHOTS") == "1"}
}

func (s *snapshotter) check(name, content string) {
	s.t.Helper()
	path := filepath.Join("testdata", "snapshots", name+".txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatalf("mkdir snapshots: %v", err)
	}
	got := content + "\n"
	if s.update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			s.t.Fatalf("write snapshot %s: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		// First run: create the snapshot so it can be reviewed and committed.
		s.t.Logf("snapshot %s created (%d bytes)", name, len(content))
		if writeErr := os.WriteFile(path, []byte(got), 0o644); writeErr != nil {
			s.t.Fatalf("create snapshot %s: %v", name, writeErr)
		}
		return
	}
	// Normalize CRLF so a Windows checkout with core.autocrlf=true still
	// compares equal (see .gitattributes).
	wantText := strings.ReplaceAll(string(want), "\r\n", "\n")
	if wantText != got {
		s.t.Errorf("snapshot %s mismatch\n--- want ---\n%s\n--- got ---\n%s", name, wantText, got)
	}
}

func TestViewSnapshots(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	snap := newSnapshotter(t)

	snap.check("login_email", app.View())

	// SMS tab with the captcha image shown.
	sendKey(t, app, tea.KeyCtrlT)
	cmd := sendKey(t, app, tea.KeyRight)
	app.Update(waitFor[captchaLoadedMsg](t, cmd))
	snap.check("login_sms_captcha", app.View())

	// SMS tab after the SMS code was sent.
	setSMSForm(app, "13800001234", "abcd", "")
	cmd = sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[smsSentMsg](t, cmd))
	snap.check("login_sms_code", app.View())

	// Home screen after login.
	setSMSForm(app, "13800001234", "abcd", testSMSCode)
	cmd = sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[loginSucceededMsg](t, cmd))
	snap.check("home", app.View())

	// Startup splash while validating a stored token.
	store := &config.Store{
		Path: t.TempDir() + "/config.json",
		Data: config.Config{DeviceID: testDeviceID, Token: testToken},
	}
	client := api.New(testDeviceID)
	client.BaseURL = ts.srv.URL
	splashApp := New(client, store)
	splashApp.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	splashApp.Init()
	snap.check("splash", splashApp.View())

	// Inline error state.
	loginApp, _ := newTestApp(t, ts)
	setEmailForm(loginApp, testEmail, "wrong-password")
	cmd = sendKey(t, loginApp, tea.KeyEnter)
	loginApp.Update(waitFor[loginFailedMsg](t, cmd))
	snap.check("login_error", loginApp.View())
}
