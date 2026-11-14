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
	screenChat // 主窗口：会话列表 + 聊天
	screenHome // 我的：账号信息
)

// App is the root Bubble Tea model. It owns the API client, the persisted
// config and the screen currently shown.
type App struct {
	client *api.Client
	store  *config.Store

	screen screen
	width  int
	height int

	login loginModel
	home  homeModel
	chat  mainWindowModel

	spinner spinner.Model
}

// New creates the root model.
func New(client *api.Client, store *config.Store) *App {
	app := &App{
		client:  client,
		store:   store,
		screen:  screenLogin,
		spinner: newSpinner(),
	}
	app.login = newLoginModel(client, store.Data)
	app.home = newHomeModel(client, store.Data.Token)
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
		return a, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
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

	case conversationsLoadedMsg, messagesLoadedMsg, messageSentMsg, chatUserMsg:
		if a.screen != screenChat {
			return a, nil
		}
		var cmd tea.Cmd
		a.chat, cmd = a.chat.Update(msg)
		return a, cmd

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
	switch msg.method {
	case "sms":
		a.store.Data.LastMobile = msg.account
	case "email":
		a.store.Data.LastEmail = msg.account
	}
	saveErr := a.store.Save()

	a.prepareHome(msg.user, "登录成功，欢迎回来")
	cmd := a.startChat(msg.user)
	if saveErr != nil {
		a.chat.errText = "登录成功，但保存登录状态失败: " + saveErr.Error()
	}
	return a, cmd
}

// prepareHome (re)builds the account screen for a logged in account.
func (a *App) prepareHome(user *api.UserInfo, okText string) {
	a.home = newHomeModel(a.client, a.store.Data.Token).resize(a.width, a.height)
	if user != nil {
		a.home = a.home.withUser(user)
		a.home.okText = okText
	}
}

// startChat creates the main window for the current token and starts loading
// the conversation list.
func (a *App) startChat(user *api.UserInfo) tea.Cmd {
	a.chat = newMainWindowModel(a.client, a.store.Data.Token, user).resize(a.width, a.height)
	a.screen = screenChat
	var cmd tea.Cmd
	a.chat, cmd = a.chat.init()
	if user == nil {
		cmd = tea.Batch(cmd, fetchChatUserCmd(a.client, a.store.Data.Token))
	}
	return cmd
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

// handleLogoutDone clears the token and returns to the login screen.
func (a *App) handleLogoutDone(msg logoutDoneMsg) tea.Cmd {
	clearErr := a.store.ClearToken()
	notice := "已退出登录"
	switch {
	case clearErr != nil:
		notice = "已退出登录，但清除本地登录状态失败: " + clearErr.Error()
	case msg.err != nil && !api.IsSessionExpired(msg.err):
		notice = "已退出登录（服务器注销请求失败）"
	}
	a.chat = mainWindowModel{}
	return a.resetLogin(notice)
}

// handleExpired drops an expired token and returns to login.
func (a *App) handleExpired() tea.Cmd {
	notice := "登录已过期，请重新登录"
	if clearErr := a.store.ClearToken(); clearErr != nil {
		notice += "（本地登录状态清除失败）"
	}
	a.chat = mainWindowModel{}
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
