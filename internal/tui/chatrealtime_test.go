package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yhchat-tui/internal/api"
)

// conversationByID finds one conversation in the chat model.
func conversationByID(app *App, chatID string) *api.Conversation {
	for i := range app.chat.conversations {
		if app.chat.conversations[i].ChatID == chatID {
			return &app.chat.conversations[i]
		}
	}
	return nil
}

// pushEvent builds a realtime push message event.
func pushEvent(chatID string, chatType api.ChatType, msgID, senderID, senderName, text string) wsEventMsg {
	return wsEventMsg{event: api.WSEvent{
		Kind: api.WSEventPush,
		Push: &api.PushMessage{
			MsgID:       msgID,
			Sender:      api.Sender{ID: senderID, Name: senderName},
			ChatID:      chatID,
			ChatType:    chatType,
			ContentType: api.ContentTypeText,
			Content:     api.MessageContent{Text: text},
			TimestampMs: 1710000099000,
			MsgSeq:      99,
		},
	}}
}

func TestChatRealtimePushUpdatesConversation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	// The selected conversation must stay selected when another conversation
	// jumps to the top because of a new message.
	_, cmd := app.Update(pushEvent(testUserChatID, api.ChatTypeUser, "ws-1", testUserChatID, "小明", "实时新消息"))
	if cmd != nil {
		_ = cmd // dismiss notification command (not required for this assertion)
	}
	if selected := app.chat.selectedConv(); selected == nil || selected.ChatID != testGroupChatID {
		t.Fatalf("selection moved to %+v", selected)
	}
	conv := conversationByID(app, testUserChatID)
	if conv == nil {
		t.Fatal("user conversation missing")
	}
	if conv.Unread != 1 {
		t.Fatalf("unread = %d, want 1", conv.Unread)
	}
	if conv.Preview() != "实时新消息" {
		t.Fatalf("preview = %q", conv.Preview())
	}
	if app.chat.conversations[0].ChatID != testUserChatID {
		t.Fatalf("conversation was not moved to the top")
	}
}

func TestChatRealtimePushInActiveChat(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	before := len(app.chat.messages[testGroupChatID])
	app.Update(pushEvent(testGroupChatID, api.ChatTypeGroup, "ws-2", testUserChatID, "小明", "群里的实时消息"))
	if got := len(app.chat.messages[testGroupChatID]); got != before+1 {
		t.Fatalf("messages = %d, want %d", got, before+1)
	}
	if !strings.Contains(app.View(), "群里的实时消息") {
		t.Fatalf("view missing pushed message\n%s", app.View())
	}
}

func TestChatRealtimeEditStreamRecall(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	// Edit an existing message.
	edit := wsEventMsg{event: api.WSEvent{
		Kind: api.WSEventEdit,
		Push: &api.PushMessage{
			MsgID:       "gm-1",
			ChatID:      testGroupChatID,
			Content:     api.MessageContent{Text: "改过的消息"},
			ContentType: 1,
		},
	}}
	app.Update(edit)
	if !strings.Contains(app.View(), "改过的消息") {
		t.Fatalf("edited message not rendered\n%s", app.View())
	}

	// Stream additional content into the same message.
	stream := wsEventMsg{event: api.WSEvent{
		Kind:   api.WSEventStream,
		Stream: &api.StreamMessage{MsgID: "gm-1", ChatID: testGroupChatID, Content: "（追加）"},
	}}
	app.Update(stream)
	if !strings.Contains(app.View(), "改过的消息（追加）") {
		t.Fatalf("streamed content not rendered\n%s", app.View())
	}

	// Recall the own message.
	recall := wsEventMsg{event: api.WSEvent{
		Kind:   api.WSEventRecall,
		Recall: &api.RecallMessage{ChatID: testGroupChatID, MsgIDs: []string{"gm-2"}},
	}}
	app.Update(recall)
	if !strings.Contains(app.View(), "撤回了一条消息") {
		t.Fatalf("recall tip not rendered\n%s", app.View())
	}
}

func TestChatRealtimeOwnEchoDeduplicated(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	app.chat.setFocus(focusInput)
	app.chat.input.SetValue("自测消息")
	cmd := sendKey(t, app, tea.KeyEnter)
	sent := waitFor[messageSentMsg](t, cmd)
	app.Update(sent)
	if sent.msgID == "" {
		t.Fatal("sent message id is empty")
	}

	// The WebSocket echo of our own message must not duplicate it.
	echo := pushEvent(testGroupChatID, api.ChatTypeGroup, sent.msgID, testUserID, testName, "自测消息")
	app.Update(echo)

	count := 0
	for _, message := range app.chat.messages[testGroupChatID] {
		if message.MsgID == sent.msgID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("own message appears %d times", count)
	}
}

func TestChatLoadsOlderMessages(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	// The test fixture returns fewer than one page, so pretend the server has
	// more history to exercise the paging path.
	app.chat.msgHasMore[testGroupChatID] = true

	// Move focus from the conversation list to the message history.
	sendKey(t, app, tea.KeyTab)
	if app.chat.focus != focusMessages {
		t.Fatalf("focus = %v, want messages", app.chat.focus)
	}

	// Scroll to the top; the next "up" loads the previous page.
	var loaded tea.Cmd
	for i := 0; i < 40 && loaded == nil; i++ {
		loaded = sendKey(t, app, tea.KeyUp)
	}
	if loaded == nil {
		t.Fatal("reaching the top did not trigger an older-page load")
	}
	app.Update(waitFor[olderMessagesLoadedMsg](t, loaded))

	if len(ts.olderRequests) != 1 || ts.olderRequests[0] != "gm-1" {
		t.Fatalf("older requests = %v", ts.olderRequests)
	}
	if got := len(app.chat.messages[testGroupChatID]); got != 5 {
		t.Fatalf("messages = %d, want 5", got)
	}
	if !strings.Contains(app.View(), "更早的消息一") {
		t.Fatalf("older message not rendered\n%s", app.View())
	}
}

func TestChatMarkReadOnConversationChange(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	// Select the second conversation (has no unread in the fixture, so force
	// one first via a realtime push, then open it).
	app.Update(pushEvent(testUserChatID, api.ChatTypeUser, "ws-3", testUserChatID, "小明", "提醒"))
	cmd := sendKey(t, app, tea.KeyUp)
	if cmd == nil {
		t.Fatal("selecting the conversation should load its messages")
	}
	msg := waitFor[messagesLoadedMsg](t, cmd)
	_, dismissCmd := app.Update(msg)
	if dismissCmd == nil {
		t.Fatal("opening a conversation should mark it read on the server")
	}
	for _, pending := range collectMsgs(dismissCmd) {
		app.Update(pending)
	}
	if conv := conversationByID(app, testUserChatID); conv == nil || conv.Unread != 0 {
		t.Fatalf("unread after opening = %+v", conv)
	}
	found := false
	for _, chatID := range ts.dismissed {
		if chatID == testUserChatID {
			found = true
		}
	}
	if !found {
		t.Fatalf("dismiss-notification not requested, got %v", ts.dismissed)
	}
}

func TestChatRealtimeConnectionState(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	app.chat.ws = &api.WSClient{}

	app.Update(wsEventMsg{event: api.WSEvent{Kind: api.WSEventConnected}})
	if !app.chat.wsConnected {
		t.Fatal("wsConnected not set")
	}
	view := app.View()
	if strings.Contains(view, "实时") || strings.Contains(view, "重连中") {
		t.Fatalf("connection status text should be removed from the UI\n%s", view)
	}
	if !strings.Contains(view, "● "+testName) {
		t.Fatalf("top-right connection dot missing\n%s", view)
	}
	if app.chat.okText != "" {
		t.Fatalf("okText = %q, want empty", app.chat.okText)
	}

	app.Update(wsEventMsg{event: api.WSEvent{Kind: api.WSEventDisconnected}})
	if app.chat.wsConnected {
		t.Fatal("wsConnected not cleared")
	}
	if strings.Contains(app.View(), "重连中") {
		t.Fatalf("connection status text should be removed from the UI\n%s", app.View())
	}
}

func TestChatRecallOwnMessage(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	app.chat.setFocus(focusInput)
	app.chat.input.SetValue("要撤回的消息")
	cmd := sendKey(t, app, tea.KeyEnter)
	sent := waitFor[messageSentMsg](t, cmd)
	app.Update(sent)

	app.chat.setFocus(focusMessages)
	cmd = sendKeyRunes(t, app, 'x')
	recalled := waitFor[messageRecalledMsg](t, cmd)
	app.Update(recalled)

	if len(ts.recalled) != 1 {
		t.Fatalf("recall requests = %d, want 1", len(ts.recalled))
	}
	if len(ts.recalled[0].MsgIDs) != 1 || ts.recalled[0].MsgIDs[0] != sent.msgID {
		t.Fatalf("recall record = %+v", ts.recalled[0])
	}
	if !strings.Contains(app.View(), "撤回了一条消息") {
		t.Fatalf("recall tip not rendered\n%s", app.View())
	}
}

// TestChatWheelUpAutoLoadsOlder verifies scrolling up with the mouse wheel at
// the top of the history automatically fetches the previous page.
func TestChatWheelUpAutoLoadsOlder(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	app.chat.msgHasMore[testGroupChatID] = true

	inner := app.chat.innerWidth()
	convW := app.chat.convPanelWidth(inner)
	chatStart := sidebarWidth + 2*panelGap + convW
	x, y := mouseTerminalXY(chatStart+2, 3)
	cmd := sendMouse(t, app, x, y, tea.MouseButtonWheelUp, tea.MouseActionPress)
	if cmd == nil {
		t.Fatal("wheel up at the top should prefetch older messages")
	}
	app.Update(waitFor[olderMessagesLoadedMsg](t, cmd))
	if len(ts.olderRequests) != 1 || ts.olderRequests[0] != "gm-1" {
		t.Fatalf("older requests = %v", ts.olderRequests)
	}
}
