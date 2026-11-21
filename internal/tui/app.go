package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/config"
)

// screen identifies the active view.
type screen int

const (
	screenLogin screen = iota
	screenSplash
	screenChat      // 主窗口：会话列表 + 聊天
	screenContacts  // 通讯录：好友 / 群聊 / 机器人
	screenCommunity // 社区：分区 / 文章
	screenHome      // 我的：个人资料与设置
)

// App is the root Bubble Tea model. It owns the API client, the persisted
// config and the screen currently shown.
type App struct {
	client *api.Client
	store  *config.Store

	screen screen
	width  int
	height int

	login     loginModel
	home      homeModel
	chat      mainWindowModel
	contacts  contactsModel
	community communityModel

	spinner spinner.Model

	// Realtime (WebSocket) connection, owned by the App and shared with chat.
	ws        *api.WSClient
	wsEnabled bool

	// addingAccount is true while the login screen is adding another account.
	addingAccount bool
}

// New creates the root model.
func New(client *api.Client, store *config.Store) *App {
	app := &App{
		client:    client,
		store:     store,
		screen:    screenLogin,
		spinner:   newSpinner(),
		wsEnabled: true,
	}
	app.login = newLoginModel(client, store.Data)
	app.home = newHomeModel(client, store.Data.Token)
	app.contacts = newContactsModel(client, store.Data.Token)
	app.community = newCommunityModel(client, store.Data.Token)
	return app
}

// newSpinner creates the shared loading spinner.
func newSpinner() spinner.Model {
	return spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(colorAccent)),
	)
}

// autoLoginCmd validates a stored token by fetching the profile.
func autoLoginCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		user, err := client.UserInfo(ctx, token)
		return autoLoginMsg{user: user, err: err}
	}
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	if a.store.Data.Token != "" {
		a.screen = screenSplash
		return tea.Batch(a.spinner.Tick, autoLoginCmd(a.client, a.store.Data.Token))
	}
	a.screen = screenLogin
	var cmd tea.Cmd
	a.login, cmd = a.login.start()
	return cmd
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.login = a.login.resize(msg.Width, msg.Height)
		a.home = a.home.resize(msg.Width, msg.Height)
		a.chat = a.chat.resize(msg.Width, msg.Height)
		a.contacts = a.contacts.resize(msg.Width, msg.Height)
		a.community = a.community.resize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		return a.route(msg)

	case tea.MouseMsg:
		return a.route(msg)

	case autoLoginMsg:
		return a.handleAutoLogin(msg)

	case loginSucceededMsg:
		return a.handleLoginSuccess(msg)

	case logoutDoneMsg:
		return a, a.handleLogoutDone(msg)

	case expiringTokenMsg:
		return a, a.handleExpired()

	case openHomeMsg:
		return a, a.showHome()

	case openChatMsg:
		return a, a.showChat()

	case switchAccountMsg:
		return a, a.handleSwitchAccount(msg.token)

	case addAccountMsg:
		a.addingAccount = true
		a.login = newLoginModel(a.client, a.store.Data).resize(a.width, a.height)
		a.login.notice = "登录要添加的账号"
		var loginCmd tea.Cmd
		a.screen = screenLogin
		a.login, loginCmd = a.login.start()
		return a, loginCmd

	case deleteAccountMsg:
		return a, a.handleDeleteAccount(msg.token)

	case openContactChatMsg:
		return a, a.openContactChat(msg)

	case openArticleMsg:
		return a, a.openArticle(msg.postID)

	case switchSectionMsg:
		return a, a.switchSection(msg.to)

	case contactsLoadedMsg, friendRequestsLoadedMsg:
		if a.screen != screenContacts {
			return a, nil
		}
		var cmd tea.Cmd
		a.contacts, cmd = a.contacts.Update(msg)
		return a, cmd

	case communitiesLoadedMsg, communityInfoMsg, postsLoadedMsg, postDetailMsg:
		if a.screen != screenCommunity {
			return a, nil
		}
		var cmd tea.Cmd
		a.community, cmd = a.community.Update(msg)
		return a, cmd

	case profileUserDataMsg, profileActionDoneMsg:
		if a.screen != screenHome {
			return a, nil
		}
		var cmd tea.Cmd
		a.home, cmd = a.home.Update(msg)
		return a, cmd

	case captchaLoadedMsg, smsSentMsg, loginFailedMsg:
		if a.screen != screenLogin {
			return a, nil
		}
		var cmd tea.Cmd
		a.login, cmd = a.login.Update(msg)
		return a, cmd

	case homeUserMsg:
		if a.screen != screenHome {
			return a, nil
		}
		var cmd tea.Cmd
		a.home, cmd = a.home.Update(msg)
		return a, cmd

	case conversationsLoadedMsg, messagesLoadedMsg, olderMessagesLoadedMsg,
		messageSentMsg, chatUserMsg, notificationsDismissedMsg:
		if a.screen != screenChat {
			return a, nil
		}
		var cmd tea.Cmd
		a.chat, cmd = a.chat.Update(msg)
		return a, cmd

	case wsEventMsg:
		if msg.closed {
			a.ws = nil
			a.chat.ws = nil
			a.chat.wsConnected = false
			return a, nil
		}
		var cmd tea.Cmd
		a.chat, cmd = a.chat.handleWSEvent(msg.event)
		if a.ws == nil {
			return a, cmd
		}
		return a, tea.Batch(cmd, waitWSEventCmd(a.ws))

	default:
		return a.route(msg)
	}
}

// route forwards generic messages (spinner ticks, cursor blinks, …) to the
// active screen.
func (a *App) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch a.screen {
	case screenLogin:
		var cmd tea.Cmd
		a.login, cmd = a.login.Update(msg)
		return a, cmd
	case screenChat:
		var cmd tea.Cmd
		a.chat, cmd = a.chat.Update(msg)
		return a, cmd
	case screenHome:
		var cmd tea.Cmd
		a.home, cmd = a.home.Update(msg)
		return a, cmd
	case screenContacts:
		var cmd tea.Cmd
		a.contacts, cmd = a.contacts.Update(msg)
		return a, cmd
	case screenCommunity:
		var cmd tea.Cmd
		a.community, cmd = a.community.Update(msg)
		return a, cmd
	case screenSplash:
		var cmd tea.Cmd
		a.spinner, cmd = a.spinner.Update(msg)
		return a, cmd
	}
	return a, nil
}

// handleAutoLogin processes the startup token validation result.
func (a *App) handleAutoLogin(msg autoLoginMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil && msg.user != nil {
		if account, ok := a.store.ActiveAccount(); ok {
			account.Name = msg.user.Data.Name
			account.UserID = msg.user.Data.ID
			if account.DeviceID == "" {
				account.DeviceID = a.store.Data.DeviceID
			}
			a.store.UpsertAccount(account)
			_ = a.store.Save()
		}
		a.prepareHome(msg.user, "已自动登录")
		cmd := a.startChat(msg.user)
		a.login = newLoginModel(a.client, a.store.Data)
		return a, cmd
	}

	notice := "无法验证登录状态：" + api.FriendlyMessage(msg.err)
	if api.IsSessionExpired(msg.err) || msg.err == nil {
		notice = "登录已过期，请重新登录"
		if clearErr := a.store.ClearToken(); clearErr != nil {
			notice += "（本地登录状态清除失败）"
		}
	}
	cmd := a.resetLogin(notice)
	return a, cmd
}

// handleLoginSuccess persists the token and opens the chat window.
func (a *App) handleLoginSuccess(msg loginSucceededMsg) (tea.Model, tea.Cmd) {
	a.store.Data.Token = msg.token
	a.store.Data.LoginMethod = msg.method
	if msg.method == "email" {
		// Email remains a convenience hint; phone numbers are never stored.
		a.store.Data.LastEmail = msg.account
	}
	saveErr := a.store.Save()

	account := config.Account{
		Token:       msg.token,
		DeviceID:    a.store.Data.DeviceID,
		LoginMethod: msg.method,
	}
	if msg.method == "email" {
		account.Email = msg.account
	}
	if msg.user != nil && msg.user.Data != nil {
		account.Name = msg.user.Data.Name
		account.UserID = msg.user.Data.ID
	}
	a.store.UpsertAccount(account)
	_ = a.store.Save()
	a.addingAccount = false

	a.prepareHome(msg.user, "登录成功，欢迎回来")
	cmd := a.startChat(msg.user)
	if saveErr != nil {
		a.chat.errText = "登录成功，但保存登录状态失败: " + saveErr.Error()
	}
	return a, cmd
}

// prepareHome (re)builds the account screen for a logged in account.
func (a *App) prepareHome(user *api.UserInfo, okText string) {
	a.home = newHomeModel(a.client, a.store.Data.Token).resize(a.width, a.height).
		withAccountInfo(a.store.Path, a.store.Data.DeviceID)
	if user != nil {
		a.home = a.home.withUser(user)
		a.home.okText = okText
	}
	a.home = a.home.withAccounts(a.store.AccountList())
	a.contacts = newContactsModel(a.client, a.store.Data.Token).resize(a.width, a.height)
	a.community = newCommunityModel(a.client, a.store.Data.Token).resize(a.width, a.height)
}

// startChat creates the main window for the current token and starts loading
// the conversation list.
func (a *App) startChat(user *api.UserInfo) tea.Cmd {
	a.stopWS()
	a.chat = newMainWindowModel(a.client, a.store.Data.Token, user).resize(a.width, a.height)
	a.screen = screenChat
	var cmd tea.Cmd
	a.chat, cmd = a.chat.init()
	if user == nil {
		cmd = tea.Batch(cmd, fetchChatUserCmd(a.client, a.store.Data.Token))
	}
	return tea.Batch(cmd, a.startWS())
}

// startWS opens the realtime connection for the logged-in account.
func (a *App) startWS() tea.Cmd {
	if !a.wsEnabled || a.store.Data.Token == "" || a.chat.user == nil || a.chat.user.Data == nil {
		return nil
	}
	ws := api.NewWSClient(a.client.BaseURL, api.WSLoginInfo{
		UserID:   a.chat.user.Data.ID,
		Token:    a.store.Data.Token,
		Platform: a.client.Platform,
		DeviceID: a.client.DeviceID,
	})
	a.ws = ws
	a.chat.ws = ws
	ws.Start()
	return waitWSEventCmd(ws)
}

// stopWS closes the realtime connection.
func (a *App) stopWS() {
	if a.ws != nil {
		a.ws.Close()
		a.ws = nil
	}
	a.chat.ws = nil
	a.chat.wsConnected = false
}

// showHome switches to the account ("我的") screen.
func (a *App) showHome() tea.Cmd {
	if a.store.Data.Token == "" {
		return nil
	}
	if a.home.user == nil && a.chat.user != nil {
		a.home = a.home.withUser(a.chat.user)
	}
	a.screen = screenHome
	return nil
}

// showChat switches back to the chat window, refreshing the conversation list.
func (a *App) showChat() tea.Cmd {
	if a.store.Data.Token == "" {
		return nil
	}
	if a.chat.token == "" || a.chat.token != a.store.Data.Token {
		return a.startChat(a.chat.user)
	}
	a.screen = screenChat
	return loadConversationsCmd(a.client, a.chat.token)
}

// switchSection switches to another top-level sidebar section, loading its data
// on first use.
func (a *App) switchSection(to section) tea.Cmd {
	if a.store.Data.Token == "" {
		return nil
	}
	switch to {
	case sectionChat:
		return a.showChat()
	case sectionContacts:
		if a.contacts.token != a.store.Data.Token {
			a.contacts = newContactsModel(a.client, a.store.Data.Token).
				resize(a.width, a.height).
				withUser(a.chat.user)
		} else if a.contacts.user == nil && a.chat.user != nil {
			a.contacts = a.contacts.withUser(a.chat.user)
		}
		a.screen = screenContacts
		if len(a.contacts.rows) == 0 && !a.contacts.loading {
			updated, cmd := a.contacts.init()
			a.contacts = updated
			return cmd
		}
		return nil
	case sectionCommunity:
		if a.community.token != a.store.Data.Token {
			a.community = newCommunityModel(a.client, a.store.Data.Token).
				resize(a.width, a.height).
				withUser(a.chat.user)
		} else if a.community.user == nil && a.chat.user != nil {
			a.community = a.community.withUser(a.chat.user)
		}
		a.screen = screenCommunity
		if len(a.community.communities) == 0 && !a.community.loading {
			updated, cmd := a.community.init()
			a.community = updated
			return cmd
		}
		return nil
	case sectionProfile:
		return a.showHome()
	}
	return nil
}

// openContactChat switches to the chat window and opens the requested contact.
func (a *App) openContactChat(msg openContactChatMsg) tea.Cmd {
	if a.store.Data.Token == "" || msg.chatID == "" {
		return nil
	}
	var cmds []tea.Cmd
	if a.chat.token != a.store.Data.Token {
		cmds = append(cmds, a.startChat(a.chat.user))
	}
	a.screen = screenChat
	updated, cmd := a.chat.openConversation(msg.chatID, msg.chatType, msg.name, msg.avatarURL)
	a.chat = updated
	cmds = append(cmds, cmd)
	return tea.Batch(cmds...)
}

// handleLogoutDone forgets the current account and falls back to another saved
// account when one exists.
func (a *App) handleLogoutDone(msg logoutDoneMsg) tea.Cmd {
	current := a.store.Data.Token
	a.store.RemoveAccount(current)
	clearErr := a.store.ClearToken()
	notice := "已退出登录"
	switch {
	case clearErr != nil:
		notice = "已退出登录，但清除本地登录状态失败: " + clearErr.Error()
	case msg.err != nil && !api.IsSessionExpired(msg.err):
		notice = "已退出登录（服务器注销请求失败）"
	}
	a.stopWS()
	a.chat = mainWindowModel{}
	a.contacts = newContactsModel(a.client, "")
	a.community = newCommunityModel(a.client, "")
	if accounts := a.store.AccountList(); len(accounts) > 0 {
		a.store.SwitchAccount(accounts[0].Token)
		_ = a.store.Save()
		a.screen = screenSplash
		return tea.Batch(a.spinner.Tick, autoLoginCmd(a.client, a.store.Data.Token))
	}
	return a.resetLogin(notice)
}

// openArticle switches to the community page and loads one article by ID.
func (a *App) openArticle(postID int64) tea.Cmd {
	if postID <= 0 {
		return nil
	}
	a.screen = screenCommunity
	a.community = a.community.resize(a.width, a.height)
	var cmds []tea.Cmd
	if len(a.community.communities) == 0 {
		var initCmd tea.Cmd
		a.community, initCmd = a.community.init()
		if initCmd != nil {
			cmds = append(cmds, initCmd)
		}
	}
	var loadCmd tea.Cmd
	a.community, loadCmd = a.community.openArticle(postID)
	if loadCmd != nil {
		cmds = append(cmds, loadCmd)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// handleSwitchAccount switches the active login and re-authenticates.
func (a *App) handleSwitchAccount(token string) tea.Cmd {
	if token == "" || token == a.store.Data.Token {
		return nil
	}
	if !a.store.SwitchAccount(token) {
		return nil
	}
	_ = a.store.Save()
	a.stopWS()
	a.chat = mainWindowModel{}
	a.screen = screenSplash
	return tea.Batch(a.spinner.Tick, autoLoginCmd(a.client, a.store.Data.Token))
}

// handleDeleteAccount forgets one account, switching to another one if needed.
func (a *App) handleDeleteAccount(token string) tea.Cmd {
	if token == "" {
		return nil
	}
	current := a.store.Data.Token
	a.store.RemoveAccount(token)
	accounts := a.store.AccountList()
	_ = a.store.Save()
	a.home = a.home.withAccounts(accounts)
	if token != current {
		return nil
	}
	if len(accounts) > 0 {
		return a.handleSwitchAccount(accounts[0].Token)
	}
	_ = a.store.ClearToken()
	a.stopWS()
	a.chat = mainWindowModel{}
	a.contacts = newContactsModel(a.client, "")
	a.community = newCommunityModel(a.client, "")
	return a.resetLogin("已删除当前账号")
}

// handleExpired drops an expired token and returns to login.
func (a *App) handleExpired() tea.Cmd {
	a.store.RemoveAccount(a.store.Data.Token)
	notice := "登录已过期，请重新登录"
	if clearErr := a.store.ClearToken(); clearErr != nil {
		notice += "（本地登录状态清除失败）"
	}
	a.stopWS()
	a.chat = mainWindowModel{}
	a.contacts = newContactsModel(a.client, "")
	a.community = newCommunityModel(a.client, "")
	if accounts := a.store.AccountList(); len(accounts) > 0 {
		a.store.SwitchAccount(accounts[0].Token)
		_ = a.store.Save()
		a.screen = screenSplash
		return tea.Batch(a.spinner.Tick, autoLoginCmd(a.client, a.store.Data.Token))
	}
	return a.resetLogin(notice)
}

// resetLogin rebuilds the login screen with an optional notice.
func (a *App) resetLogin(notice string) tea.Cmd {
	a.login = newLoginModel(a.client, a.store.Data)
	a.login.notice = notice
	a.screen = screenLogin
	updated, cmd := a.login.start()
	a.login = updated
	return cmd
}

// View implements tea.Model.
func (a *App) View() string {
	switch a.screen {
	case screenSplash:
		return a.viewSplash()
	case screenChat:
		return a.chat.View()
	case screenContacts:
		return a.contacts.View()
	case screenCommunity:
		return a.community.View()
	case screenHome:
		return a.home.View()
	default:
		return a.login.View()
	}
}

// viewSplash shows the startup token validation screen. It uses the same
// full-terminal chrome as the other screens.
func (a *App) viewSplash() string {
	width, height := screenSize(a.width, a.height)
	inner := width - 2*chromePadX
	loading := lipgloss.JoinHorizontal(lipgloss.Center,
		a.spinner.View(), " ", hintStyle.Render("正在验证登录状态…"))
	body := []string{lipgloss.PlaceHorizontal(inner, lipgloss.Center, loading)}
	return screenChrome(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		subtitleStyle.Render("Yunhu Chat · TUI"),
		body,
		"Ctrl+C 退出",
	)
}
