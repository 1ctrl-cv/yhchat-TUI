package tui

import (
	"context"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
)

// homeModel is the post-login screen showing the account profile.
type homeModel struct {
	client *api.Client
	token  string

	user *api.UserInfo

	width, height int

	loading bool
	errText string
	okText  string
	spinner spinner.Model
}

// newHomeModel creates the home screen for the given token.
func newHomeModel(client *api.Client, token string) homeModel {
	return homeModel{client: client, token: token, spinner: newSpinner()}
}

// withUser attaches a loaded profile.
func (m homeModel) withUser(user *api.UserInfo) homeModel {
	m.user = user
	m.loading = false
	m.errText = ""
	return m
}

// resize records the terminal size.
func (m homeModel) resize(width, height int) homeModel {
	m.width, m.height = width, height
	return m
}

// startRefresh begins loading the profile from the server.
func (m homeModel) startRefresh() (homeModel, tea.Cmd) {
	m.loading = true
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick, fetchHomeUserCmd(m.client, m.token))
}

// fetchHomeUserCmd loads the profile in the background.
func fetchHomeUserCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		user, err := client.UserInfo(ctx, token)
		return homeUserMsg{user: user, err: err}
	}
}

// logoutCmd invalidates the token on the server (best effort).
func logoutCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return logoutDoneMsg{err: client.Logout(ctx, token)}
	}
}

// Update implements the home screen behaviour.
func (m homeModel) Update(msg tea.Msg) (homeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if m.loading {
			return m, nil
		}
		switch msg.String() {
		case "r":
			return m.startRefresh()
		case "c":
			return m, func() tea.Msg { return openChatMsg{} }
		case "l":
			return m, logoutCmd(m.client, m.token)
		case "q":
			return m, tea.Quit
		}
		return m, nil

	case homeUserMsg:
		m.loading = false
		if msg.err != nil {
			if api.IsSessionExpired(msg.err) {
				return m, func() tea.Msg { return expiringTokenMsg{err: msg.err} }
			}
			m.errText = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.errText = ""
		if msg.user != nil {
			m.user = msg.user
			m.okText = "账号信息已刷新"
		}
		return m, nil
	}
	return m, nil
}

// View implements tea.Model for the home screen. It fills the whole terminal:
// header, account overview, and a bottom hint bar (see screenChrome).
func (m homeModel) View() string {
	width, height := screenSize(m.width, m.height)
	inner := width - 2*chromePadX
	body := m.bodyLines(inner, height-4)

	headerRight := okStyle.Render("● 已登录")
	if m.user != nil && m.user.Data != nil && m.user.Data.Name != "" {
		headerRight = okStyle.Render("● 已登录 · " + truncate(m.user.Data.Name, 24))
	}
	return screenChromeTop(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		headerRight,
		body,
		"c 聊天 · r 刷新 · l 退出登录 · q 退出",
	)
}

// bodyLines renders the account overview, dropping decorations first on short
// terminals.
func (m homeModel) bodyLines(inner, avail int) []string {
	lines := []string{"", titleAccentStyle.Render("账号信息"), ""}
	lines = append(lines, m.viewInfoRows(inner)...)
	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("登录成功。聊天与消息功能将在后续版本实现。"))
	lines = append(lines, "")
	lines = append(lines, m.viewStatusLine(inner))
	if layoutHeight(lines) > avail {
		lines = []string{titleAccentStyle.Render("账号信息")}
		lines = append(lines, m.viewInfoRows(inner)...)
		lines = append(lines, m.viewStatusLine(inner))
	}
	return lines
}

// viewInfoRows renders the profile rows.
func (m homeModel) viewInfoRows(inner int) []string {
	if m.user == nil || m.user.Data == nil {
		return []string{hintStyle.Render("（账号信息加载中…）")}
	}
	d := m.user.Data

	phone := "未绑定"
	if d.Phone != "" {
		phone = maskPhone(d.Phone)
	}
	email := "未绑定"
	if d.Email != "" {
		email = maskEmail(d.Email)
	}
	coin := strconv.FormatFloat(d.Coin, 'f', 2, 64)
	vip := vipLabel(d.VIPStatus, d.IsVIP)
	if d.VIPStatus == 1 && d.VIPExpiredTimestamp > 0 {
		vip += "（至 " + time.Unix(d.VIPExpiredTimestamp, 0).Format("2006-01-02") + "）"
	}
	invite := d.InvitationCode
	if invite == "" {
		invite = "-"
	}

	rows := [][2]string{
		{"昵称", d.Name},
		{"用户 ID", d.ID},
		{"手机号", phone},
		{"邮箱", email},
		{"金币", coin},
		{"VIP", vip},
		{"邀请码", invite},
	}

	const labelW = 12
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		label := padLine(row[0], labelW)
		value := row[1]
		if value == "" {
			value = "-"
		}
		value = truncate(value, inner-labelW-4)
		lines = append(lines, "  "+labelStyle.Render(label)+"  "+valueStyle.Render(value))
	}
	return lines
}

// viewStatusLine renders the one-line status message (loading/error/notice).
func (m homeModel) viewStatusLine(inner int) string {
	var line string
	switch {
	case m.loading:
		line = lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ", hintStyle.Render("正在刷新账号信息…"))
	case m.errText != "":
		line = errStyle.Render(truncate(m.errText, inner))
	case m.okText != "":
		line = okStyle.Render(truncate(m.okText, inner))
	}
	return lipgloss.PlaceHorizontal(inner, lipgloss.Center, line)
}
