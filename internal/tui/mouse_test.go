package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sendMouse sends one mouse event to the app.
func sendMouse(t *testing.T, app *App, x, y int, button tea.MouseButton, action tea.MouseAction) tea.Cmd {
	t.Helper()
	model, cmd := app.Update(tea.MouseMsg{X: x, Y: y, Button: button, Action: action})
	if updated, ok := model.(*App); ok {
		*app = *updated
	} else {
		t.Fatalf("unexpected model type %T", model)
	}
	return cmd
}

// clickMouse sends a left click at the given terminal coordinates.
func clickMouse(t *testing.T, app *App, x, y int) tea.Cmd {
	t.Helper()
	return sendMouse(t, app, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
}

// mouseTerminalXY converts body-local coordinates to terminal coordinates.
func mouseTerminalXY(x, y int) (int, int) { return x + chromePadX, y + 2 }

func TestMouseSidebarNavigation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	// Sidebar row 1 = 通讯录.
	x, y := mouseTerminalXY(1, 1)
	cmd := clickMouse(t, app, x, y)
	if cmd == nil {
		t.Fatal("sidebar click should emit a section switch")
	}
	applyAll(t, app, cmd)
	if app.screen != screenContacts {
		t.Fatalf("screen = %v, want contacts", app.screen)
	}

	// Sidebar row 3 = 我的.
	x, y = mouseTerminalXY(1, int(sectionProfile))
	cmd = clickMouse(t, app, x, y)
	applyAll(t, app, cmd)
	if app.screen != screenHome {
		t.Fatalf("screen = %v, want home", app.screen)
	}
}

func TestMouseChatConversationAndInput(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	// Click the second conversation row (rows start after the 2-line search).
	x, y := mouseTerminalXY(8, 2+2)
	clickMouse(t, app, x, y)
	if app.chat.selected != 1 {
		t.Fatalf("selected = %d, want 1", app.chat.selected)
	}
	if app.chat.focus != focusConversations {
		t.Fatalf("focus = %v, want conversations", app.chat.focus)
	}

	// Click the message input box at the bottom of the chat column.
	inner := app.chat.innerWidth()
	convW := app.chat.convPanelWidth(inner)
	chatStart := sidebarWidth + 2*panelGap + convW
	bodyH := app.chat.bodyHeight()
	viewportH := app.chat.messageViewportHeight()
	x, y = mouseTerminalXY(chatStart+2, 2+viewportH+1)
	clickMouse(t, app, x, y)
	if app.chat.focus != focusInput {
		t.Fatalf("focus = %v, want input", app.chat.focus)
	}
	_ = bodyH
}

func TestMouseContactsExpandAndEnterChat(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	switchToSection(t, app, sectionContacts)

	// Click the 用户 header (rows: requests header, 用户 header, ...).
	x, y := mouseTerminalXY(8, 3)
	clickMouse(t, app, x, y)
	if !app.contacts.expanded["用户"] {
		t.Fatalf("用户 should be expanded, rows=%v", app.contacts.rows)
	}

	// Click the 小明 child row.
	x, y = mouseTerminalXY(8, 4)
	clickMouse(t, app, x, y)
	if row := app.contacts.selectedRow(); row == nil || row.kind != contactRowContact {
		t.Fatalf("selected row = %+v", row)
	}

	// Click the "进入聊天" button at the bottom of the detail pane.
	inner := app.contacts.innerWidth()
	listW := app.contacts.listPanelWidth(inner)
	detailStart := sidebarWidth + 2*panelGap + listW
	bodyH := app.contacts.bodyHeight()
	x, y = mouseTerminalXY(detailStart+2, bodyH-2)
	cmd := clickMouse(t, app, x, y)
	if cmd == nil {
		t.Fatal("button click should open the chat")
	}
	applyAll(t, app, cmd)
	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
	if conv := app.chat.selectedConv(); conv == nil || conv.ChatID != testUserChatID {
		t.Fatalf("conversation = %+v", conv)
	}
}

func TestMouseCommunityTabsAndArticle(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	switchToSection(t, app, sectionCommunity)

	// Click the 全部 tab (second compact tab).
	inner := app.community.innerWidth()
	listW := app.community.listPanelWidth(inner)
	listStart := sidebarWidth + panelGap
	tabLineWidth := 0
	for range []string{"热门", "全部", "关注", "我的"} {
		tabLineWidth += 4 // two CJK cells per label
	}
	tabLineWidth += 3 // separators
	tabX := listStart + (listW-tabLineWidth)/2 + 4 + 1 + 1
	x, y := mouseTerminalXY(tabX, 0)
	cmd := clickMouse(t, app, x, y)
	if cmd == nil {
		t.Fatal("tab click should reload communities")
	}
	_, loadCmd := app.Update(waitFor[communitiesLoadedMsg](t, cmd))
	applyAll(t, app, loadCmd)
	if app.community.tab != 1 {
		t.Fatalf("tab = %d, want 1", app.community.tab)
	}

	// Click the first post row in the detail pane to open the article.
	detailStart := sidebarWidth + 2*panelGap + listW
	x, y = mouseTerminalXY(detailStart+2, 3)
	cmd = clickMouse(t, app, x, y)
	if cmd == nil {
		t.Fatal("post click should open the article")
	}
	app.Update(waitFor[postDetailMsg](t, cmd))
	if app.community.view != communityArticleView {
		t.Fatalf("view = %v, want article", app.community.view)
	}
}

func TestMouseProfileMenuAndDialog(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	// Click the 4th menu item (添加好友/群/机器人): menu starts at body row 5.
	inner := app.home.innerWidth()
	menuW := app.home.menuPanelWidth(inner)
	menuStart := sidebarWidth + panelGap
	x, y := mouseTerminalXY(menuStart+2, 5+3)
	cmd := clickMouse(t, app, x, y)
	applyAll(t, app, cmd)
	if app.home.detail != detailFormAddFriend {
		t.Fatalf("detail = %v, want add-friend form", app.home.detail)
	}

	// Click the 群聊 tab inside the dialog.
	detailStart := menuStart + menuW + panelGap
	detailW := inner - detailStart
	bodyH := app.home.bodyHeight()
	_, boxLeft, interior, contentTop := app.home.addFriendDialogGeometry(bodyH, detailW)
	interiorStart := boxLeft + 3
	tabsTotal := 4*3 + 2
	secondTabX := interiorStart + (interior-tabsTotal)/2 + 4 + 1 + 1
	x, y = mouseTerminalXY(detailStart+secondTabX, contentTop+3)
	clickMouse(t, app, x, y)
	if app.home.addFriendType != 1 {
		t.Fatalf("add friend type = %d, want group", app.home.addFriendType)
	}
	if !app.home.addFriendTabsFocus {
		t.Fatal("type tabs should have focus after a click")
	}

	// Click 取消 to close the dialog.
	cancelW := 6
	primaryW := 10
	buttonsTotal := cancelW + 2 + primaryW
	cancelX := interiorStart + (interior-buttonsTotal)/2 + cancelW/2
	x, y = mouseTerminalXY(detailStart+cancelX, contentTop+11)
	clickMouse(t, app, x, y)
	if app.home.detail != detailEmpty {
		t.Fatalf("detail = %v, want empty after cancel", app.home.detail)
	}
}

func TestMouseProfileUserDataEditButton(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	// Open 个人资料 via its menu row.
	inner := app.home.innerWidth()
	menuW := app.home.menuPanelWidth(inner)
	menuStart := sidebarWidth + panelGap
	x, y := mouseTerminalXY(menuStart+2, 5+2)
	cmd := clickMouse(t, app, x, y)
	applyAll(t, app, cmd)
	if app.home.detail != detailUserData {
		t.Fatalf("detail = %v, want user data", app.home.detail)
	}

	// Click the 修改 button on the bottom row.
	detailStart := menuStart + menuW + panelGap
	bodyH := app.home.bodyHeight()
	x, y = mouseTerminalXY(detailStart+4, bodyH-1)
	cmd = clickMouse(t, app, x, y)
	applyAll(t, app, cmd)
	if app.home.detail != detailFormUserData {
		t.Fatalf("detail = %v, want edit form", app.home.detail)
	}
}

// TestLoginMouseCanSwitchBackToSMS clicks the login-method tabs by their real
// rendered positions (they are left-aligned, not split across the row).
func TestLoginMouseCanSwitchBackToSMS(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 32})

	clickTab := func(target string) {
		t.Helper()
		width, height := screenSize(100, 32)
		inner := width - 2*chromePadX
		avail := height - 4
		flat := flattenLines(app.login.bodyLines(inner, avail))
		top := (avail - len(flat)) / 2
		emailWidth := lipgloss.Width(tabInactiveStyle.Render("邮箱密码"))
		smsWidth := lipgloss.Width(tabInactiveStyle.Render("短信验证码"))
		x := chromePadX + 2 + emailWidth/2
		if target == "sms" {
			x = chromePadX + 2 + emailWidth + 1 + smsWidth/2
		}
		applyAll(t, app, clickMouse(t, app, x, 2+top))
	}

	if app.login.method != methodEmail {
		t.Fatalf("initial method = %v, want email", app.login.method)
	}
	clickTab("sms")
	if app.login.method != methodSMS {
		t.Fatalf("after SMS tab click method = %v, want SMS", app.login.method)
	}
	clickTab("email")
	if app.login.method != methodEmail {
		t.Fatalf("after email tab click method = %v, want email", app.login.method)
	}
	// The reported bug: clicking the SMS tab again must switch back.
	clickTab("sms")
	if app.login.method != methodSMS {
		t.Fatalf("after second SMS tab click method = %v, want SMS", app.login.method)
	}
}

// wheelMouse sends one mouse-wheel notch at body-local coordinates.
func wheelMouse(t *testing.T, app *App, x, y int, up bool) tea.Cmd {
	t.Helper()
	button := tea.MouseButtonWheelDown
	if up {
		button = tea.MouseButtonWheelUp
	}
	tx, ty := mouseTerminalXY(x, y)
	return sendMouse(t, app, tx, ty, button, tea.MouseActionPress)
}

// TestWheelMovesSelectionInsteadOfScrolling verifies that wheel notches move
// the selected conversation/message/contact/menu item.
func TestWheelMovesSelectionInsteadOfScrolling(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	// Conversation list: one notch moves the selection by exactly one row.
	app.chat.selected = 0
	cmd := wheelMouse(t, app, 8, 3, false)
	applyAll(t, app, cmd)
	if app.chat.selected != 1 {
		t.Fatalf("conversation selection = %d, want 1", app.chat.selected)
	}
	wheelMouse(t, app, 8, 3, true)
	if app.chat.selected != 0 {
		t.Fatalf("conversation selection after wheel up = %d, want 0", app.chat.selected)
	}

	// Message area: wheel up/down changes the selected message.
	inner := app.chat.innerWidth()
	convW := app.chat.convPanelWidth(inner)
	chatStart := sidebarWidth + 2*panelGap + convW
	before := app.chat.selectedMsg
	wheelMouse(t, app, chatStart+2, 3, true)
	if app.chat.selectedMsg != before-1 {
		t.Fatalf("selected message = %d, want %d", app.chat.selectedMsg, before-1)
	}
	wheelMouse(t, app, chatStart+2, 3, false)
	if app.chat.selectedMsg != before {
		t.Fatalf("selected message after wheel down = %d, want %d", app.chat.selectedMsg, before)
	}

	// Contacts list.
	switchToSection(t, app, sectionContacts)
	before = app.contacts.selected
	wheelMouse(t, app, 10, 3, false)
	if app.contacts.selected != before+1 {
		t.Fatalf("contacts selection = %d, want %d", app.contacts.selected, before+1)
	}

	// Profile menu.
	openHomeScreen(t, app)
	before = app.home.menuIndex
	wheelMouse(t, app, sidebarWidth+panelGap+2, 3, false)
	if app.home.menuIndex != before+1 {
		t.Fatalf("menu selection = %d, want %d", app.home.menuIndex, before+1)
	}
}
