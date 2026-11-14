package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// loginToChat logs in and returns the app positioned in the chat window with
// the conversation list and the first conversation's history loaded.
func loginToChat(t *testing.T, app *App) {
	t.Helper()
	loginEmail(t, app)
	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
}

func TestMainWindowInitialState(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	if len(app.chat.conversations) != 2 {
		t.Fatalf("conversations = %d, want 2", len(app.chat.conversations))
	}
	if app.chat.selected != 0 {
		t.Fatalf("selected = %d, want 0", app.chat.selected)
	}
	if app.chat.focus != focusConversations {
		t.Fatalf("focus = %v, want conversations", app.chat.focus)
	}
	if messages := app.chat.messages[testGroupChatID]; len(messages) != 3 {
		t.Fatalf("group messages = %d, want 3", len(messages))
	}
	view := app.View()
	for _, want := range []string{"测试群", "小明", "大家好", "我也在", "撤回了一条消息", "群聊"} {
		if !strings.Contains(view, want) {
			t.Errorf("chat view missing %q\n%s", want, view)
		}
	}
}

func TestMainWindowSelectConversation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	cmd := sendKey(t, app, tea.KeyDown)
	if app.chat.selected != 1 {
		t.Fatalf("selected = %d, want 1", app.chat.selected)
	}
	if cmd == nil {
		t.Fatal("selecting a conversation should load its messages")
	}
	msg := waitFor[messagesLoadedMsg](t, cmd)
	if msg.err != nil {
		t.Fatalf("load messages: %v", msg.err)
	}
	if msg.chatID != testUserChatID {
		t.Fatalf("chat id = %q, want %q", msg.chatID, testUserChatID)
	}
	app.Update(msg)

	view := app.View()
	for _, want := range []string{"小明", "晚上见", "好的，明天见"} {
		if !strings.Contains(view, want) {
			t.Errorf("user chat view missing %q\n%s", want, view)
		}
	}
}

func TestMainWindowSendMessage(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	// Enter opens the selected conversation and focuses the input.
	cmd := sendKey(t, app, tea.KeyEnter)
	if app.chat.focus != focusInput {
		t.Fatalf("focus = %v, want input", app.chat.focus)
	}
	if cmd == nil {
		t.Fatal("focusing the input should start the cursor blink")
	}

	sendRunes(t, app, "你好呀")
	if got := app.chat.input.Value(); got != "你好呀" {
		t.Fatalf("input value = %q", got)
	}

	cmd = sendKey(t, app, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("enter in the input should send the message")
	}
	if !app.chat.sending {
		t.Fatal("sending flag not set")
	}
	msg := waitFor[messageSentMsg](t, cmd)
	if msg.err != nil {
		t.Fatalf("send message: %v", msg.err)
	}
	app.Update(msg)

	ts.mu.Lock()
	sent := append([]sentTestMessage(nil), ts.sentMessages...)
	ts.mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(sent))
	}
	if sent[0].ChatID != testGroupChatID || sent[0].ChatType != 2 || sent[0].Text != "你好呀" {
		t.Fatalf("unexpected sent message: %+v", sent[0])
	}
	if len(sent[0].MsgID) != 32 {
		t.Fatalf("msg id = %q, want 32 hex chars", sent[0].MsgID)
	}
	if app.chat.sending || app.chat.input.Value() != "" {
		t.Fatalf("sending state not cleared (sending=%v value=%q)", app.chat.sending, app.chat.input.Value())
	}
	view := app.View()
	if !strings.Contains(view, "你好呀") {
		t.Errorf("chat view missing sent message\n%s", view)
	}
	if !strings.Contains(view, "消息已发送") {
		t.Errorf("chat view missing send confirmation\n%s", view)
	}
}

func TestMainWindowSendMessageFailure(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	ts.SetSendError("消息发送太快了")

	sendKey(t, app, tea.KeyEnter) // focus input
	sendRunes(t, app, "再试一次")
	cmd := sendKey(t, app, tea.KeyEnter)
	msg := waitFor[messageSentMsg](t, cmd)
	if msg.err == nil {
		t.Fatal("expected send error")
	}
	app.Update(msg)

	if app.chat.sending {
		t.Fatal("sending flag not cleared after error")
	}
	if got := app.chat.input.Value(); got != "再试一次" {
		t.Fatalf("input value restored = %q, want 再试一次", got)
	}
	if !strings.Contains(app.chat.errText, "消息发送太快了") {
		t.Fatalf("errText = %q", app.chat.errText)
	}
}

func TestMainWindowSearchFilter(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	// Move focus to the search box.
	sendKey(t, app, tea.KeyShiftTab)
	if app.chat.focus != focusSearch {
		t.Fatalf("focus = %v, want search", app.chat.focus)
	}
	sendRunes(t, app, "小明")
	if len(app.chat.filtered) != 1 {
		t.Fatalf("filtered = %d, want 1", len(app.chat.filtered))
	}
	if app.chat.filtered[0].DisplayName() != "小明" {
		t.Fatalf("filtered[0] = %q", app.chat.filtered[0].DisplayName())
	}
	if view := app.View(); !strings.Contains(view, "小明") || strings.Contains(view, "测试群") {
		t.Errorf("unexpected filtered view\n%s", view)
	}

	sendKey(t, app, tea.KeyEsc)
	if app.chat.focus != focusConversations {
		t.Fatalf("focus = %v, want conversations", app.chat.focus)
	}
	if len(app.chat.filtered) != 2 {
		t.Fatalf("filtered after clear = %d, want 2", len(app.chat.filtered))
	}
}

func TestMainWindowSidebarToHomeAndBack(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	// conversations -> search -> sidebar
	sendKey(t, app, tea.KeyShiftTab)
	sendKey(t, app, tea.KeyShiftTab)
	if app.chat.focus != focusSidebar {
		t.Fatalf("focus = %v, want sidebar", app.chat.focus)
	}

	// Move the sidebar cursor to "我的" and open it.
	for app.chat.sidebarCursor != sectionProfile {
		sendKey(t, app, tea.KeyDown)
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("opening the profile page should emit a message")
	}
	app.Update(cmd())
	if app.screen != screenHome {
		t.Fatalf("screen = %v, want home", app.screen)
	}

	// "c" returns to the chat window.
	cmd = sendKeyRunes(t, app, 'c')
	if cmd == nil {
		t.Fatal("c should return to chat")
	}
	app.Update(cmd())
	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
}

func TestMainWindowFocusCycle(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	wantOrder := []chatFocus{focusMessages, focusInput, focusSidebar, focusSearch, focusConversations}
	for i, want := range wantOrder {
		sendKey(t, app, tea.KeyTab)
		if app.chat.focus != want {
			t.Fatalf("tab %d: focus = %v, want %v", i+1, app.chat.focus, want)
		}
		// The hint bar follows the focus.
		if hint := app.View(); !strings.Contains(hint, "Tab 切换焦点") {
			t.Errorf("hint bar missing for focus %v", want)
		}
	}
}

func TestMainWindowSessionExpired(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginToChat(t, app)

	// Break the token, then refresh the conversation list.
	app.chat.token = "expired-token"
	cmd := sendKeyRunes(t, app, 'r')
	if cmd == nil {
		t.Fatal("r should start a refresh")
	}
	msg := waitFor[conversationsLoadedMsg](t, cmd)
	if msg.err == nil {
		t.Fatal("expected refresh to fail")
	}
	_, next := app.Update(msg)
	if next == nil {
		t.Fatal("expired session should emit expiringTokenMsg")
	}
	if _, ok := next().(expiringTokenMsg); !ok {
		t.Fatalf("unexpected follow-up message %T", next())
	}
}
