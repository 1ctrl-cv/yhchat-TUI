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

func TestProfileUserDataEdit(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	openHomeScreen(t, app)

	// Open 个人资料 (3rd item) and wait for the data to load.
	for app.home.menuIndex < 2 {
		sendKey(t, app, tea.KeyDown)
	}
	cmd := sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[profileUserDataMsg](t, cmd))
	if app.home.detail != detailUserData {
		t.Fatalf("detail = %v, want user data", app.home.detail)
	}
	if !strings.Contains(app.View(), "修改") {
		t.Fatalf("个人资料 page missing the 修改 button\n%s", app.View())
	}

	// Enter opens the edit dialog.
	cmd = sendKey(t, app, tea.KeyEnter)
	applyAll(t, app, cmd)
	if app.home.detail != detailFormUserData {
		t.Fatalf("detail = %v, want edit form", app.home.detail)
	}
	view := app.View()
	for _, want := range []string{"修改资料", "个人简介", "所在地", "地区码", "保存", "取消"} {
		if !strings.Contains(view, want) {
			t.Errorf("edit dialog missing %q\n%s", want, view)
		}
	}

	// Fill the form and save.
	app.home.fields[0].SetValue("新的个人简介")
	app.home.fields[1].SetValue("上海市 上海城区 黄浦区")
	app.home.fields[2].SetValue("310101")
	app.home.setUserDataGender(2)
	cmd = sendKey(t, app, tea.KeyEnter)
	done := waitFor[profileActionDoneMsg](t, cmd)
	_, reload := app.Update(done)
	app.Update(waitFor[profileUserDataMsg](t, reload))

	if ts.userData == nil {
		t.Fatal("save-user-data was not called")
	}
	if got := ts.userData["introduction"]; got != "新的个人简介" {
		t.Fatalf("introduction = %v", got)
	}
	if got := ts.userData["gender"]; got != float64(2) {
		t.Fatalf("gender = %v", got)
	}
	if got := ts.userData["province"]; got != "上海市" {
		t.Fatalf("province = %v", got)
	}
	if got := ts.userData["locationCode"]; got != "310101" {
		t.Fatalf("locationCode = %v", got)
	}
	view = app.View()
	for _, want := range []string{"新的个人简介", "上海市", "310101"} {
		if !strings.Contains(view, want) {
			t.Errorf("saved profile missing %q\n%s", want, view)
		}
	}
}

// TestCommunityArticleReactionsAndComments covers like/collect toggles and
// publishing both top-level comments and 楼中楼 replies.
func TestCommunityArticleReactionsAndComments(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	switchToSection(t, app, sectionCommunity)

	sendKey(t, app, tea.KeyEnter)
	cmd := sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[postDetailMsg](t, cmd))
	if app.community.view != communityArticleView {
		t.Fatalf("view = %v, want article", app.community.view)
	}
	runeKey := func(text string) tea.Cmd {
		t.Helper()
		model, keyCmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
		updated, ok := model.(*App)
		if !ok {
			t.Fatalf("unexpected model %T", model)
		}
		*app = *updated
		return keyCmd
	}

	// Like then unlike.
	app.Update(waitFor[postLikeDoneMsg](t, runeKey("l")))
	app.Update(waitFor[postLikeDoneMsg](t, runeKey("l")))
	ts.mu.Lock()
	likes := append([]int64(nil), ts.postLikes...)
	ts.mu.Unlock()
	if len(likes) != 2 || likes[0] != 31622 || likes[1] != 31622 {
		t.Fatalf("post likes = %v", likes)
	}
	if post := app.community.currentPost(); post == nil || post.IsLiked == "1" {
		t.Fatalf("post should be unliked after second toggle: %+v", post)
	}

	// Collect then uncollect.
	app.Update(waitFor[postCollectDoneMsg](t, runeKey("s")))
	app.Update(waitFor[postCollectDoneMsg](t, runeKey("s")))
	ts.mu.Lock()
	collects := append([]int64(nil), ts.postCollects...)
	ts.mu.Unlock()
	if len(collects) != 2 || collects[0] != 31622 {
		t.Fatalf("post collects = %v", collects)
	}

	// Reply to the second comment (id 2) via 楼中楼.
	runeKey("j") // move selection from the first comment to the second
	app.Update(runeKey("r"))
	if !app.community.commentMode || app.community.commentParentID != 2 {
		t.Fatalf("reply mode=%v parent=%d, want mode=true parent=2",
			app.community.commentMode, app.community.commentParentID)
	}
	runeKey("楼中楼回复")
	app.Update(waitFor[commentPostedMsg](t, sendKey(t, app, tea.KeyEnter)))
	if app.community.commentMode {
		t.Fatal("reply mode should close after publishing")
	}
	ts.mu.Lock()
	posted := append([]testPostedComment(nil), ts.postedComments...)
	ts.mu.Unlock()
	if len(posted) != 1 || posted[0].CommentID != 2 || posted[0].Content != "楼中楼回复" {
		t.Fatalf("posted reply = %+v", posted)
	}

	// Top-level comment (ParentID 0).
	app.Update(runeKey("c"))
	runeKey("顶层评论")
	app.Update(waitFor[commentPostedMsg](t, sendKey(t, app, tea.KeyEnter)))
	ts.mu.Lock()
	posted = append([]testPostedComment(nil), ts.postedComments...)
	ts.mu.Unlock()
	if len(posted) != 2 || posted[1].CommentID != 0 || posted[1].Content != "顶层评论" {
		t.Fatalf("posted comments = %+v", posted)
	}
}

// TestCommunityArticleEditorAndModeration covers the article editor
// (create/edit/delete), comment like/delete and the comment input visibility.
func TestCommunityArticleEditorAndModeration(t *testing.T) {
	ts := newTestServer(t)
	app, _ := newTestApp(t, ts)
	loginEmail(t, app)
	switchToSection(t, app, sectionCommunity)
	sendKey(t, app, tea.KeyEnter)
	cmd := sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[postDetailMsg](t, cmd))

	runeKey := func(text string) tea.Cmd {
		t.Helper()
		model, keyCmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
		updated, ok := model.(*App)
		if !ok {
			t.Fatalf("unexpected model %T", model)
		}
		*app = *updated
		return keyCmd
	}
	ctrlS := func() tea.Cmd {
		t.Helper()
		model, keyCmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
		updated, ok := model.(*App)
		if !ok {
			t.Fatalf("unexpected model %T", model)
		}
		*app = *updated
		return keyCmd
	}

	// Comment composer must be visible while typing a comment.
	app.Update(runeKey("c"))
	if view := app.View(); !strings.Contains(view, "评论: ") {
		t.Fatalf("comment input not visible:\n%s", view)
	}
	sendKey(t, app, tea.KeyEsc)

	// Comment like and delete (two-step confirmation).
	app.Update(waitFor[commentLikeDoneMsg](t, runeKey("L")))
	ts.mu.Lock()
	commentLikes := append([]int64(nil), ts.commentLikes...)
	ts.mu.Unlock()
	if len(commentLikes) != 1 || commentLikes[0] != 1 {
		t.Fatalf("comment likes = %v", commentLikes)
	}
	runeKey("D")
	app.Update(waitFor[commentDeletedMsg](t, runeKey("D")))
	ts.mu.Lock()
	deletedComments := append([]int64(nil), ts.deletedComments...)
	ts.mu.Unlock()
	if len(deletedComments) != 1 || deletedComments[0] != 1 {
		t.Fatalf("deleted comments = %v", deletedComments)
	}

	// Back to the post list and open the article editor for a new article.
	sendKey(t, app, tea.KeyEsc)
	runeKey("n")
	if app.community.editorMode != editorCreate {
		t.Fatalf("editor mode = %d, want create", app.community.editorMode)
	}
	app.community.editorTitle.SetValue("新文章标题")
	app.community.editorContent.SetValue("新文章正文")
	app.Update(waitFor[articleSavedMsg](t, ctrlS()))
	ts.mu.Lock()
	created := append([]testCreatedPost(nil), ts.createdPosts...)
	ts.mu.Unlock()
	if len(created) != 1 || created[0].Title != "新文章标题" || created[0].ContentType != 2 {
		t.Fatalf("created posts = %+v", created)
	}

	// Open the article again and edit it.
	sendKey(t, app, tea.KeyEnter)
	cmd = sendKey(t, app, tea.KeyEnter)
	app.Update(waitFor[postDetailMsg](t, cmd))
	app.Update(runeKey("e"))
	if app.community.editorMode != editorEdit {
		t.Fatalf("editor mode = %d, want edit", app.community.editorMode)
	}
	app.community.editorTitle.SetValue("修改后的标题")
	app.Update(waitFor[articleSavedMsg](t, ctrlS()))
	ts.mu.Lock()
	edited := append([]testEditedPost(nil), ts.editedPosts...)
	ts.mu.Unlock()
	if len(edited) != 1 || edited[0].PostID != 31622 || edited[0].Title != "修改后的标题" {
		t.Fatalf("edited posts = %+v", edited)
	}

	// Delete the article (two-step confirmation).
	runeKey("d") // first d arms the confirmation
	app.Update(waitFor[articleDeletedMsg](t, runeKey("d")))
	ts.mu.Lock()
	deletedPosts := append([]int64(nil), ts.deletedPosts...)
	ts.mu.Unlock()
	if len(deletedPosts) != 1 || deletedPosts[0] != 31622 {
		t.Fatalf("deleted posts = %v", deletedPosts)
	}
}
