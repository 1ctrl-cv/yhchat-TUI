package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yhchat-tui/internal/api"
)

// injectArticleMessage appends a content_type=6 article message to the selected
// conversation and selects it in the message viewport.
func injectArticleMessage(t *testing.T, app *App, postID string) {
	t.Helper()
	conv := app.chat.selectedConv()
	if conv == nil {
		t.Fatal("no selected conversation")
	}
	app.chat.messages[conv.ChatID] = append(app.chat.messages[conv.ChatID], api.Message{
		MsgID:       "article-msg-1",
		Direction:   "left",
		ContentType: 6,
		Content:     api.MessageContent{PostID: postID, PostTitle: "云湖新用户必读", PostContent: "第一行正文\n第二行正文"},
		TimestampMs: 1710000123000,
		Sender:      api.Sender{ID: "1001", Name: "小明"},
	})
	app.chat.selectedMsg = len(app.chat.messages[conv.ChatID]) - 1
	app.chat.msgFollow = true
	app.chat.setFocus(focusMessages)
	app.chat.invalidateMessageLines()
}

// TestChatArticleMessageKeyboardOpen presses v on a selected article message
// and expects the community article detail to open.
func TestChatArticleMessageKeyboardOpen(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	injectArticleMessage(t, app, "31622")

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	updated, ok := model.(*App)
	if !ok {
		t.Fatalf("unexpected model %T", model)
	}
	*app = *updated
	openMsg := waitFor[openArticleMsg](t, cmd)
	if openMsg.postID != 31622 {
		t.Fatalf("article id = %d, want 31622", openMsg.postID)
	}
	model, cmd = app.Update(openMsg)
	*app = *model.(*App)
	app.Update(waitFor[postDetailMsg](t, cmd))
	if app.screen != screenCommunity {
		t.Fatalf("screen = %v, want community", app.screen)
	}
	if app.community.view != communityArticleView {
		t.Fatalf("community view = %v, want article", app.community.view)
	}
	if app.community.postDetail == nil || app.community.postDetail.Post.ID != 31622 {
		t.Fatalf("post detail = %+v", app.community.postDetail)
	}
}

// TestChatArticleMessageMouseOpen clicks on the article card and expects the
// same navigation as the keyboard shortcut.
func TestChatArticleMessageMouseOpen(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	injectArticleMessage(t, app, "31622")

	conv := app.chat.selectedConv()
	lines := app.chat.messageLines()
	height := app.chat.messageViewportHeight()
	offset := maxInt(0, len(lines)-height)
	start := app.chat.cacheStarts[len(app.chat.messages[conv.ChatID])-1]
	row := start - offset
	if row < 0 || row >= height {
		row = height - 1
	}
	inner := app.chat.innerWidth()
	convW := app.chat.convPanelWidth(inner)
	chatStart := sidebarWidth + 2*panelGap + convW
	x, y := mouseTerminalXY(chatStart+2, 2+row)
	cmd := clickMouse(t, app, x, y)
	if cmd == nil {
		t.Fatalf("clicking the article card returned no command (row=%d)", row)
	}
	openMsg := waitFor[openArticleMsg](t, cmd)
	if openMsg.postID != 31622 {
		t.Fatalf("article id = %d, want 31622", openMsg.postID)
	}
	model, cmd := app.Update(openMsg)
	*app = *model.(*App)
	app.Update(waitFor[postDetailMsg](t, cmd))
	if app.screen != screenCommunity || app.community.view != communityArticleView {
		t.Fatalf("screen=%v view=%v, want community article", app.screen, app.community.view)
	}
}
