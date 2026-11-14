package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeText sends each rune of text to the app.
func typeText(t *testing.T, app *App, text string) {
	t.Helper()
	for _, r := range text {
		sendKeyRunes(t, app, r)
	}
}

// openSectionViaSidebar switches sections using real key events: focus the
// sidebar, move the cursor to target and press Enter.
func openSectionViaSidebar(t *testing.T, app *App, target section) {
	t.Helper()
	current := app.screen
	if current != screenChat && current != screenContacts && current != screenCommunity && current != screenHome {
		t.Fatalf("cannot navigate from screen %v", current)
	}
	for i := 0; i < 8; i++ {
		if app.screenSidebarFocused() {
			break
		}
		sendKey(t, app, tea.KeyShiftTab)
	}
	if !app.screenSidebarFocused() {
		t.Fatal("sidebar never got focus")
	}
	for i := 0; i < sectionCount; i++ {
		if app.sidebarCursor() == target {
			break
		}
		// Move forward with down; wrap-around is not needed for our tests.
		sendKey(t, app, tea.KeyDown)
	}
	if app.sidebarCursor() != target {
		t.Fatalf("sidebar cursor = %v, want %v", app.sidebarCursor(), target)
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	applyAll(t, app, cmd)
}

// screenSidebarFocused reports whether the active page's sidebar has focus.
func (a *App) screenSidebarFocused() bool {
	switch a.screen {
	case screenChat:
		return a.chat.focus == focusSidebar
	case screenContacts:
		return a.contacts.focus == contactsSidebar
	case screenCommunity:
		return a.community.focus == communitySidebar
	case screenHome:
		return a.home.focus == profileSidebar
	}
	return false
}

// sidebarCursor returns the active page's sidebar cursor.
func (a *App) sidebarCursor() section {
	switch a.screen {
	case screenChat:
		return a.chat.sidebarCursor
	case screenContacts:
		return a.contacts.sidebarCursor
	case screenCommunity:
		return a.community.sidebarCursor
	case screenHome:
		return a.home.sidebarCursor
	}
	return sectionChat
}

func TestContactsPageNavigation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	openSectionViaSidebar(t, app, sectionContacts)
	if app.screen != screenContacts {
		t.Fatalf("screen = %v, want contacts", app.screen)
	}
	view := app.View()
	for _, want := range []string{"通讯录", "申请 / 邀请", "用户", "我加入的群聊", "机器人"} {
		if !strings.Contains(view, want) {
			t.Errorf("contacts view missing %q\n%s", want, view)
		}
	}

	// Expand "用户" (first row is the requests header when pending > 0).
	sendKey(t, app, tea.KeyDown)
	sendKey(t, app, tea.KeyEnter)
	view = app.View()
	if !strings.Contains(view, "小明") {
		t.Fatalf("expanding 用户 should reveal 小明\n%s", view)
	}

	// Select the contact and check the detail pane.
	sendKey(t, app, tea.KeyDown)
	view = app.View()
	if !strings.Contains(view, "1001") || !strings.Contains(view, "用户") {
		t.Fatalf("contact detail missing ID/type\n%s", view)
	}

	// Collapse again.
	sendKey(t, app, tea.KeyUp)
	sendKey(t, app, tea.KeyEnter)
	if strings.Count(app.View(), "小明") != 0 {
		t.Fatalf("collapsing 用户 should hide 小明\n%s", app.View())
	}
}

func TestCommunityPageNavigation(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)

	openSectionViaSidebar(t, app, sectionCommunity)
	if app.screen != screenCommunity {
		t.Fatalf("screen = %v, want community", app.screen)
	}
	view := app.View()
	for _, want := range []string{"社区", "热门", "全部", "关注", "我的", "云湖&功能反馈", "云湖新用户必读"} {
		if !strings.Contains(view, want) {
			t.Errorf("community view missing %q\n%s", want, view)
		}
	}

	// Enter opens the article list focus; Enter again loads the article.
	sendKey(t, app, tea.KeyEnter)
	cmd := sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[postDetailMsg](t, cmd))
	view = app.View()
	for _, want := range []string{"云湖阶段安排", "第一内测阶段", "文章信息", "小明", "来了来了"} {
		if !strings.Contains(view, want) {
			t.Errorf("article view missing %q\n%s", want, view)
		}
	}

	// Esc returns to the community detail view.
	sendKey(t, app, tea.KeyEsc)
	if app.community.view != communityListView {
		t.Fatalf("view = %v, want list", app.community.view)
	}
}

func TestCommunityTabSwitch(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	switchToSection(t, app, sectionCommunity)

	// Move focus to the tab bar and change the tab.
	sendKey(t, app, tea.KeyShiftTab)
	if app.community.focus != communityTabs {
		t.Fatalf("focus = %v, want tabs", app.community.focus)
	}
	cmd := sendKey(t, app, tea.KeyRight)
	app.Update(waitFor[communitiesLoadedMsg](t, cmd))
	if app.community.tab != 1 {
		t.Fatalf("tab = %d, want 1", app.community.tab)
	}
	if !strings.Contains(app.View(), "全部") {
		t.Fatalf("tab bar should show all tabs\n%s", app.View())
	}
}

func TestProfileEditNickname(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)
	if app.screen != screenHome {
		t.Fatalf("screen = %v, want home", app.screen)
	}

	// Menu: 修改头像(0), 修改昵称(1).
	sendKey(t, app, tea.KeyDown)
	if app.home.menuIndex != 1 {
		t.Fatalf("menu index = %d", app.home.menuIndex)
	}
	cmd := sendKey(t, app, tea.KeyEnter)
	applyAll(t, app, cmd)
	if app.home.detail != detailFormNickname {
		t.Fatalf("detail = %v, want nickname form", app.home.detail)
	}

	// Replace the value and submit.
	app.home.fields[0].SetValue("新昵称")
	cmd = sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[profileActionDoneMsg](t, cmd))
	if ts.editedNickname != "新昵称" {
		t.Fatalf("edited nickname = %q", ts.editedNickname)
	}
	if !strings.Contains(app.View(), "新昵称") {
		t.Fatalf("profile card should show the new nickname\n%s", app.View())
	}
}

func TestProfileAddFriend(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	// Menu: 添加好友/群/机器人 is the 4th item.
	for i := 0; i < 3; i++ {
		sendKey(t, app, tea.KeyDown)
	}
	cmd := sendKey(t, app, tea.KeyEnter)
	applyAll(t, app, cmd)
	if app.home.detail != detailFormAddFriend {
		t.Fatalf("detail = %v, want add-friend form", app.home.detail)
	}

	// Cycle ID -> verification -> type tabs and pick 群聊.
	sendKey(t, app, tea.KeyTab)
	sendKey(t, app, tea.KeyTab)
	if !app.home.addFriendTabsFocus {
		t.Fatalf("expected type tabs focus")
	}
	sendKey(t, app, tea.KeyRight)
	if app.home.addFriendType != 1 {
		t.Fatalf("add friend type = %d, want group", app.home.addFriendType)
	}
	sendKey(t, app, tea.KeyEnter)
	typeText(t, app, "2002")
	sendKey(t, app, tea.KeyTab)
	typeText(t, app, "你好")
	cmd = sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[profileActionDoneMsg](t, cmd))
	if len(ts.friendApplies) != 1 {
		t.Fatalf("friend applies = %d, want 1", len(ts.friendApplies))
	}
	got := ts.friendApplies[0]
	if got.ChatID != "2002" || got.ChatType != 2 || got.Remark != "你好" {
		t.Fatalf("apply = %+v", got)
	}
}

func TestProfileUserDataAndSettings(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	// 个人资料 (3rd item).
	sendKey(t, app, tea.KeyDown)
	sendKey(t, app, tea.KeyDown)
	cmd := sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[profileUserDataMsg](t, cmd))
	view := app.View()
	for _, want := range []string{"个人资料", "这是测试简介", "北京市", "110101"} {
		if !strings.Contains(view, want) {
			t.Errorf("user data view missing %q\n%s", want, view)
		}
	}

	// Esc back to the menu, then open 设置.
	sendKey(t, app, tea.KeyEsc)
	for app.home.menuIndex < len(profileMenuItems)-1 && profileMenuItems[app.home.menuIndex].Label != "设置" {
		sendKey(t, app, tea.KeyDown)
	}
	cmd = sendKey(t, app, tea.KeyEnter)
	applyAll(t, app, cmd)
	view = app.View()
	for _, want := range []string{"设置", "配置文件", "已登录", "设备 ID"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings view missing %q\n%s", want, view)
		}
	}
}

func TestContactsEnterChat(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	switchToSection(t, app, sectionContacts)

	// Expand 用户 and select 小明 (an existing conversation).
	sendKey(t, app, tea.KeyDown)
	sendKey(t, app, tea.KeyEnter)
	sendKey(t, app, tea.KeyDown)
	cmd := sendKey(t, app, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("Enter on a contact should emit an open-chat message")
	}
	applyAll(t, app, cmd)
	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
	selected := app.chat.selectedConv()
	if selected == nil || selected.ChatID != testUserChatID {
		t.Fatalf("selected conversation = %+v", selected)
	}
	if app.chat.focus != focusInput {
		t.Fatalf("focus = %v, want input", app.chat.focus)
	}
	if !strings.Contains(app.View(), "小明") {
		t.Fatalf("chat view does not show the opened contact\n%s", app.View())
	}

	// A contact without an existing conversation gets a local entry so the
	// user can start chatting immediately.
	switchToSection(t, app, sectionContacts)
	// Collapse 用户, then expand 机器人.
	sendKey(t, app, tea.KeyUp)
	sendKey(t, app, tea.KeyEnter)
	for i := 0; i < 3; i++ {
		sendKey(t, app, tea.KeyDown)
	}
	sendKey(t, app, tea.KeyEnter)
	sendKey(t, app, tea.KeyDown)
	cmd = sendKey(t, app, tea.KeyEnter)
	applyAll(t, app, cmd)
	if app.screen != screenChat {
		t.Fatalf("screen = %v, want chat", app.screen)
	}
	if conv := app.chat.selectedConv(); conv == nil || conv.ChatID != "4001" {
		t.Fatalf("synthetic conversation = %+v", conv)
	}
}

func TestProfileAboutPage(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	view := app.View()
	if strings.Contains(view, "开发文档") {
		t.Fatalf("开发文档 should be removed from the profile menu\n%s", view)
	}
	if !strings.Contains(view, "关于") {
		t.Fatalf("关于 menu item missing\n%s", view)
	}

	for app.home.menuIndex < len(profileMenuItems)-1 && profileMenuItems[app.home.menuIndex].Label != "关于" {
		sendKey(t, app, tea.KeyDown)
	}
	cmd := sendKey(t, app, tea.KeyEnter)
	applyAll(t, app, cmd)
	if app.home.detail != detailAbout {
		t.Fatalf("detail = %v, want about", app.home.detail)
	}
	view = app.View()
	for _, want := range []string{"作者", "1ctrl-cv", "项目地址", "https://github.com/1ctrl-cv/yhchat-TUI"} {
		if !strings.Contains(view, want) {
			t.Errorf("about view missing %q\n%s", want, view)
		}
	}
}

func TestAddFriendDialogLayout(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	// Menu: 添加好友/群/机器人 is the 4th item.
	for i := 0; i < 3; i++ {
		sendKey(t, app, tea.KeyDown)
	}
	cmd := sendKey(t, app, tea.KeyEnter)
	applyAll(t, app, cmd)
	view := app.View()
	for _, want := range []string{"添加好友", "输入用户、群或机器人的 ID", "好友", "群聊", "机器人", "验证消息", "发送申请", "取消"} {
		if !strings.Contains(view, want) {
			t.Errorf("add friend dialog missing %q\n%s", want, view)
		}
	}
}
