package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/config"
)

// sendRunes feeds a literal string to the app (one key message per rune).
func sendRunes(t *testing.T, app *App, text string) {
	t.Helper()
	for _, r := range text {
		model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if updated, ok := model.(*App); ok {
			*app = *updated
		}
	}
}

func TestEmailLoginFlow(t *testing.T) {
	ts := newTestServer(t)
	app, store := newTestApp(t, ts)

	if app.screen != screenLogin {
		t.Fatalf("initial screen = %v, want login", app.screen)
	}

	loginEmail(t, app)

	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
	if app.chat.user == nil || app.chat.user.Data == nil {
		t.Fatalf("chat profile missing: %+v", app.chat.user)
	}
	if app.chat.user.Data.Name != testName {
		t.Fatalf("name = %q", app.chat.user.Data.Name)
	}
	if len(app.chat.conversations) != 2 {
		t.Fatalf("conversations = %d, want 2", len(app.chat.conversations))
	}
	if store.Data.Token != testToken {
		t.Fatalf("stored token = %q", store.Data.Token)
	}
	if store.Data.LoginMethod != "email" || store.Data.LastEmail != testEmail {
		t.Fatalf("stored prefs = %+v", store.Data)
	}
	if _, err := os.Stat(store.Path); err != nil {
		t.Fatalf("config file not persisted: %v", err)
	}

	// The login milestone now lands on the chat window; the account page is
	// reachable from the sidebar ("我的").
	chatView := app.View()
	for _, want := range []string{"测试群", "小明", "大家好"} {
		if !strings.Contains(chatView, want) {
			t.Errorf("chat view missing %q\n%s", want, chatView)
		}
	}

	openHomeScreen(t, app)
	view := app.View()
	for _, want := range []string{testName, testUserID, "已登录", "138****1234", "d***@yunhu.chat", "YH-TEST-0001"} {
		if !strings.Contains(view, want) {
			t.Errorf("home view missing %q\n%s", want, view)
		}
	}
}

func TestEmailLoginWrongPassword(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)

	setEmailForm(app, testEmail, "wrong-password")
	cmd := sendKey(t, app, tea.KeyEnter)
	msg := waitFor[loginFailedMsg](t, cmd)
	app.Update(msg)

	if app.screen != screenLogin {
		t.Fatalf("screen = %v, want login", app.screen)
	}
	if app.login.busy {
		t.Fatal("busy flag not cleared")
	}
	if !strings.Contains(app.login.errText, "密码错误") {
		t.Fatalf("errText = %q", app.login.errText)
	}
	if storeTokenPresent(app) {
		t.Fatal("token should not be stored after failed login")
	}
}

func TestEmailValidation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)

	setEmailForm(app, "not-an-email", "")
	sendKey(t, app, tea.KeyEnter)
	if app.login.busy {
		t.Error("invalid input must not start a login request")
	}
	if !strings.Contains(app.login.errText, "邮箱") {
		t.Fatalf("errText = %q", app.login.errText)
	}

	setEmailForm(app, testEmail, "")
	sendKey(t, app, tea.KeyEnter)
	if app.login.busy {
		t.Error("missing password must not start a login request")
	}
	if !strings.Contains(app.login.errText, "密码") {
		t.Fatalf("errText = %q", app.login.errText)
	}
}

func TestSMSLoginFlow(t *testing.T) {
	ts := newTestServer(t)
	app, store := newTestApp(t, ts)

	loginSMS(t, app)

	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
	if store.Data.Token != testToken || store.Data.LoginMethod != "sms" {
		t.Fatalf("stored config = %+v", store.Data)
	}
	if store.Data.LastMobile != "13800001234" {
		t.Fatalf("last mobile = %q", store.Data.LastMobile)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.sentSMS {
		t.Fatal("SMS request was never sent")
	}
	if ts.lastSendSMSPayload["mobile"] != "13800001234" ||
		ts.lastSendSMSPayload["code"] != "abcd" ||
		ts.lastSendSMSPayload["id"] != "cap-1" {
		t.Fatalf("unexpected SMS payload: %+v", ts.lastSendSMSPayload)
	}
}

func TestSMSFlowValidation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)

	// Invalid phone blocks sending the SMS code.
	app.login.method = methodSMS
	app.login.stage = smsStageCaptcha
	app.login.captcha = &api.ImageCaptcha{ID: "cap-1", DataURL: "data:image/png;base64,AA=="}
	app.login.focus = focusImageCode
	setSMSForm(app, "123", "abcd", "")
	sendKey(t, app, tea.KeyEnter)
	if app.login.busy {
		t.Error("invalid phone must not send an SMS request")
	}
	if !strings.Contains(app.login.errText, "手机号") {
		t.Fatalf("errText = %q", app.login.errText)
	}

	// Empty image captcha blocks sending the SMS.
	setSMSForm(app, "13800001234", "", "")
	app.login.stage = smsStageCaptcha
	app.login.focus = focusImageCode
	sendKey(t, app, tea.KeyEnter)
	if app.login.busy {
		t.Error("missing captcha text must not send the SMS request")
	}
	if !strings.Contains(app.login.errText, "图形验证码") && !strings.Contains(app.login.errText, "图片") {
		t.Fatalf("errText = %q", app.login.errText)
	}
}

func TestSMSEscGoesBack(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)

	sendKey(t, app, tea.KeyCtrlT)
	cmd := sendKey(t, app, tea.KeyRight)
	app.Update(waitFor[captchaLoadedMsg](t, cmd))

	setSMSForm(app, "13800001234", "abcd", "")
	cmd = sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[smsSentMsg](t, cmd))
	if app.login.stage != smsStageCode {
		t.Fatalf("stage = %v, want code", app.login.stage)
	}

	sendKey(t, app, tea.KeyEsc)
	if app.login.stage != smsStageCaptcha {
		t.Fatalf("stage after first esc = %v, want captcha", app.login.stage)
	}
	sendKey(t, app, tea.KeyEsc)
	if app.login.stage != smsStagePhone {
		t.Fatalf("stage after second esc = %v, want phone", app.login.stage)
	}
	if app.login.captcha != nil || app.login.capImg != nil {
		t.Fatal("captcha should be cleared")
	}
}

func TestCaptchaRefreshAndSave(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)

	sendKey(t, app, tea.KeyCtrlT)
	cmd := sendKey(t, app, tea.KeyRight)
	app.Update(waitFor[captchaLoadedMsg](t, cmd))
	if app.login.capImg == nil {
		t.Fatal("captcha image missing")
	}

	// Ctrl+R fetches a fresh captcha.
	cmd = sendKey(t, app, tea.KeyCtrlR)
	if !app.login.busy {
		t.Fatal("expected busy state while refreshing captcha")
	}
	app.Update(waitFor[captchaLoadedMsg](t, cmd))
	if app.login.capImg == nil {
		t.Fatal("captcha image missing after refresh")
	}

	// Ctrl+S saves the image to a temp PNG.
	cmd = sendKey(t, app, tea.KeyCtrlS)
	if cmd != nil {
		t.Log("save captcha returned a command (unexpected but harmless)")
	}
	const prefix = "验证码图片已保存: "
	if !strings.HasPrefix(app.login.okText, prefix) {
		t.Fatalf("okText = %q", app.login.okText)
	}
	path := strings.TrimPrefix(app.login.okText, prefix)
	t.Cleanup(func() { _ = os.Remove(path) })
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("saved captcha file missing/empty: %v", err)
	}
}

func TestAutoLoginWithStoredToken(t *testing.T) {
	ts := newTestServer(t)
	store := &config.Store{
		Path: t.TempDir() + "/config.json",
		Data: config.Config{DeviceID: testDeviceID, Token: testToken},
	}
	client := api.New(testDeviceID)
	client.BaseURL = ts.srv.URL
	client.Platform = "windows"
	app := New(client, store)

	cmd := app.Init()
	if app.screen != screenSplash {
		t.Fatalf("screen = %v, want splash", app.screen)
	}
	msg := waitFor[autoLoginMsg](t, cmd)
	if msg.err != nil {
		t.Fatalf("auto login error: %v", msg.err)
	}
	_, loadCmd := app.Update(msg)
	if loadCmd == nil {
		t.Fatal("auto login did not start loading conversations")
	}
	app.Update(waitFor[conversationsLoadedMsg](t, loadCmd))

	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
	if app.chat.user == nil || app.chat.user.Data.Name != testName {
		t.Fatalf("profile missing: %+v", app.chat.user)
	}
}

func TestExpiredTokenReturnsToLogin(t *testing.T) {
	ts := newTestServer(t)
	store := &config.Store{
		Path: t.TempDir() + "/config.json",
		Data: config.Config{DeviceID: testDeviceID, Token: "expired-token"},
	}
	client := api.New(testDeviceID)
	client.BaseURL = ts.srv.URL
	app := New(client, store)

	cmd := app.Init()
	msg := waitFor[autoLoginMsg](t, cmd)
	if msg.err == nil {
		t.Fatal("expected auto login failure")
	}
	app.Update(msg)

	if app.screen != screenLogin {
		t.Fatalf("screen = %v, want login", app.screen)
	}
	if store.Data.Token != "" {
		t.Fatalf("token not cleared: %q", store.Data.Token)
	}
	if !strings.Contains(app.login.notice, "登录已过期") {
		t.Fatalf("notice = %q", app.login.notice)
	}
}

func TestLogoutFlow(t *testing.T) {
	ts := newTestServer(t)
	app, store := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	// Press "l" on the home screen.
	cmd := sendKeyRunes(t, app, 'l')
	msg := waitFor[logoutDoneMsg](t, cmd)
	if msg.err != nil {
		t.Fatalf("logout error: %v", msg.err)
	}
	app.Update(msg)

	ts.mu.Lock()
	loggedOut := ts.loggedOut
	ts.mu.Unlock()
	if !loggedOut {
		t.Fatal("logout request not sent")
	}
	if app.screen != screenLogin {
		t.Fatalf("screen = %v, want login", app.screen)
	}
	if store.Data.Token != "" {
		t.Fatalf("token not cleared: %q", store.Data.Token)
	}
	if !strings.Contains(app.login.notice, "已退出登录") {
		t.Fatalf("notice = %q", app.login.notice)
	}
}

func TestRefreshOnHome(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	cmd := sendKeyRunes(t, app, 'r')
	msg := waitFor[homeUserMsg](t, cmd)
	app.Update(msg)
	if app.home.loading {
		t.Fatal("loading flag not cleared")
	}
	if app.home.errText != "" {
		t.Fatalf("unexpected error: %q", app.home.errText)
	}
	if app.home.user == nil {
		t.Fatal("profile lost after refresh")
	}
}

func TestQuitKeys(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)

	// Ctrl+C on the login screen must quit.
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c did not return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command = %T, want tea.QuitMsg", cmd())
	}
	if updated, ok := model.(*App); ok {
		_ = updated
	}

	// "q" quits from the home screen.
	loginEmail(t, app)
	openHomeScreen(t, app)
	cmd = sendKeyRunes(t, app, 'q')
	if cmd == nil {
		t.Fatal("q did not return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("q command = %T, want tea.QuitMsg", cmd())
	}
}

func TestTabBarNavigation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)

	sendKey(t, app, tea.KeyCtrlT)
	if app.login.focus != focusTabs {
		t.Fatalf("focus = %v, want tabs", app.login.focus)
	}
	cmd := sendKey(t, app, tea.KeyRight)
	if app.login.method != methodSMS {
		t.Fatalf("method = %v, want SMS", app.login.method)
	}
	if cmd == nil {
		t.Fatal("switching to SMS should fetch a captcha")
	}
	app.Update(waitFor[captchaLoadedMsg](t, cmd))

	// Enter on the tab bar jumps to the first field.
	sendKey(t, app, tea.KeyCtrlT)
	sendKey(t, app, tea.KeyEnter)
	if app.login.focus != focusPhone {
		t.Fatalf("focus = %v, want phone", app.login.focus)
	}

	// Switch back to email.
	sendKey(t, app, tea.KeyCtrlT)
	sendKey(t, app, tea.KeyLeft)
	if app.login.method != methodEmail {
		t.Fatalf("method = %v, want email", app.login.method)
	}
}

// TestViewFitsTerminal verifies that every screen fits common terminal sizes
// (no vertical overflow that would scroll the alt screen, no over-wide lines).
func TestViewFitsTerminal(t *testing.T) {
	ts := newTestServer(t)
	sizes := []struct{ w, h int }{
		{100, 32}, {80, 24}, {120, 40}, {80, 20}, {60, 24}, {100, 24},
	}
	for _, size := range sizes {
		app, _ := newTestApp(t, ts)
		app.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})

		check := func(label, view string) {
			t.Helper()
			lines := strings.Split(view, "\n")
			if len(lines) > size.h {
				t.Errorf("%dx%d %s: view height %d > %d", size.w, size.h, label, len(lines), size.h)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w > size.w {
					t.Errorf("%dx%d %s: line %d width %d > %d: %q", size.w, size.h, label, i, w, size.w, line)
				}
			}
		}

		check("login_email", app.View())

		// SMS captcha view is the tallest screen.
		sendKey(t, app, tea.KeyCtrlT)
		cmd := sendKey(t, app, tea.KeyRight)
		app.Update(waitFor[captchaLoadedMsg](t, cmd))
		check("login_sms_captcha", app.View())

		// Chat window and home screen.
		prefsApp, _ := newTestApp(t, ts)
		prefsApp.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		loginEmail(t, prefsApp)
		check("chat", prefsApp.View())
		openHomeScreen(t, prefsApp)
		check("home", prefsApp.View())
	}
}

// sendKeyRunes sends a single rune key.
func sendKeyRunes(t *testing.T, app *App, r rune) tea.Cmd {
	t.Helper()
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	if updated, ok := model.(*App); ok {
		*app = *updated
	}
	return cmd
}

// storeTokenPresent reports whether a token is persisted in the config store.
func storeTokenPresent(app *App) bool {
	return app.store.Data.Token != ""
}
