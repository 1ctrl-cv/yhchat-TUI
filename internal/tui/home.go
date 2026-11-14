package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
)

// profileFocus identifies the focused pane of the "我的" page.
type profileFocus int

const (
	profileSidebar profileFocus = iota
	profileMenu
	profileDetail
)

// profileDetail identifies what the right-hand detail pane shows.
type profileDetailKind int

const (
	detailEmpty profileDetailKind = iota
	detailFormNickname
	detailFormAvatar
	detailFormAddFriend
	detailUserData
	detailLogoutConfirm
	detailSettings
	detailAbout
	detailPlaceholderCreateGroup
	detailPlaceholderCreateBot
	detailPlaceholderBotConsole
)

// profileMenuItem is one row of the profile menu.
type profileMenuItem struct {
	Label  string
	Detail profileDetailKind
	Hint   string
}

// profileMenuItems mirrors the official client's "我的" page (at least the
// entries that can be implemented in the terminal).
var profileMenuItems = []profileMenuItem{
	{Label: "修改头像", Detail: detailFormAvatar, Hint: "输入新的头像图片 URL"},
	{Label: "修改昵称", Detail: detailFormNickname, Hint: "输入新的昵称"},
	{Label: "个人资料", Detail: detailUserData, Hint: "查看个人资料"},
	{Label: "添加好友/群/机器人", Detail: detailFormAddFriend, Hint: "按 ID 发送添加申请"},
	{Label: "创建群聊", Detail: detailPlaceholderCreateGroup, Hint: "后续版本支持"},
	{Label: "创建机器人", Detail: detailPlaceholderCreateBot, Hint: "后续版本支持"},
	{Label: "机器人控制台", Detail: detailPlaceholderBotConsole, Hint: "后续版本支持"},
	{Label: "设置", Detail: detailSettings, Hint: "配置文件与登录状态"},
	{Label: "关于", Detail: detailAbout, Hint: "作者与项目信息"},
	{Label: "退出登录", Detail: detailLogoutConfirm, Hint: "清除本地 token"},
}

// Messages produced by profile commands.

// profileUserDataMsg carries the extended profile data.
type profileUserDataMsg struct {
	data *api.UserData
	err  error
}

// profileActionDoneMsg reports the result of an edit/apply action.
type profileActionDoneMsg struct {
	kind    profileDetailKind
	err     error
	success string
}

// homeModel is the "我的" page: profile card + menu + detail pane.
type homeModel struct {
	client *api.Client
	token  string
	user   *api.UserInfo

	width, height int

	focus         profileFocus
	sidebarCursor section

	menuIndex  int
	menuOffset int
	detail     profileDetailKind

	// form state (nickname / avatar / add-friend)
	fields     []textinput.Model
	fieldFocus int

	// add-friend dialog state
	addFriendType      int  // 0 user, 1 group, 2 bot
	addFriendTabsFocus bool // focus is on the type tabs

	userData *api.UserData

	configPathText string
	deviceIDText   string

	loading bool
	errText string
	okText  string
	spinner spinner.Model
}

// newHomeModel creates the "我的" page for the given token.
func newHomeModel(client *api.Client, token string) homeModel {
	return homeModel{
		client:        client,
		token:         token,
		focus:         profileMenu,
		sidebarCursor: sectionProfile,
		spinner:       newSpinner(),
	}
}

// withUser attaches the loaded account profile.
func (m homeModel) withUser(user *api.UserInfo) homeModel {
	m.user = user
	return m
}

// withAccountInfo attaches local config information shown in the settings page.
func (m homeModel) withAccountInfo(configPath, deviceID string) homeModel {
	m.configPathText = configPath
	m.deviceIDText = deviceID
	return m
}

// resize records the terminal size.
func (m homeModel) resize(width, height int) homeModel {
	m.width, m.height = width, height
	return m
}

// startRefresh reloads the account profile.
func (m homeModel) startRefresh() (homeModel, tea.Cmd) {
	return m, fetchHomeUserCmd(m.client, m.token)
}

// ---------------------------------------------------------------------------
// commands

// loadUserDataCmd fetches the extended personal profile.
func loadUserDataCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		data, err := client.UserData(ctx, token)
		return profileUserDataMsg{data: data, err: err}
	}
}

// editNicknameCmd changes the nickname.
func editNicknameCmd(client *api.Client, token, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return profileActionDoneMsg{kind: detailFormNickname, err: client.EditNickname(ctx, token, name), success: "昵称已更新"}
	}
}

// editAvatarCmd changes the avatar URL.
func editAvatarCmd(client *api.Client, token, avatarURL string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return profileActionDoneMsg{kind: detailFormAvatar, err: client.EditAvatar(ctx, token, avatarURL), success: "头像已更新"}
	}
}

// addFriendCmd sends an add-friend/group/bot request.
func addFriendCmd(client *api.Client, token, chatID string, chatType api.ChatType, remark string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return profileActionDoneMsg{
			kind:    detailFormAddFriend,
			err:     client.AddFriend(ctx, token, chatID, chatType, remark),
			success: "申请已发送",
		}
	}
}

// fetchHomeUserCmd loads the account profile in the background.
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

// ---------------------------------------------------------------------------
// update

// Update handles messages for the "我的" page.
func (m homeModel) Update(msg tea.Msg) (homeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

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
		}
		m.okText = "账号信息已刷新"
		return m, nil

	case profileUserDataMsg:
		m.loading = false
		if msg.err != nil {
			if api.IsSessionExpired(msg.err) {
				return m, func() tea.Msg { return expiringTokenMsg{err: msg.err} }
			}
			m.errText = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.userData = msg.data
		m.errText = ""
		return m, nil

	case profileActionDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.errText = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.errText = ""
		m.okText = msg.success
		m.detail = detailEmpty
		m.fields = nil
		m.focus = profileMenu
		m.addFriendTabsFocus = false
		m.addFriendType = 0
		return m, nil

	default:
		if m.focus == profileDetail && len(m.fields) > 0 {
			idx := m.currentFieldIndex()
			if idx >= 0 && idx < len(m.fields) {
				var cmd tea.Cmd
				m.fields[idx], cmd = m.fields[idx].Update(msg)
				return m, cmd
			}
		}
		return m, nil
	}
}

// handleKey processes key presses.
func (m homeModel) handleKey(msg tea.KeyMsg) (homeModel, tea.Cmd) {
	key := msg.String()

	if m.focus == profileDetail && m.detail != detailEmpty && len(m.fields) > 0 {
		switch key {
		case "tab":
			return m.cycleFormFocus(1)
		case "shift+tab":
			return m.cycleFormFocus(-1)
		}
	}
	switch key {
	case "tab":
		return m.handleTab()
	case "shift+tab":
		return m.handleTabBack()
	}

	switch m.focus {
	case profileSidebar:
		return m.handleSidebarKey(key)
	case profileMenu:
		return m.handleMenuKey(key)
	default:
		return m.handleDetailKey(key, msg)
	}
}

// handleSidebarKey handles sidebar navigation.
func (m homeModel) handleSidebarKey(key string) (homeModel, tea.Cmd) {
	switch key {
	case "up":
		m.sidebarCursor = nextSectionCursor(m.sidebarCursor, -1)
		return m, nil
	case "down":
		m.sidebarCursor = nextSectionCursor(m.sidebarCursor, 1)
		return m, nil
	case "enter":
		if target := m.sidebarCursor; target != sectionProfile {
			return m, func() tea.Msg { return switchSectionMsg{to: target} }
		}
		m.focus = profileMenu
		return m, nil
	case "right":
		m.focus = profileMenu
		return m, nil
	case "r":
		return m.startRefresh()
	case "c":
		return m, func() tea.Msg { return openChatMsg{} }
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// handleMenuKey handles navigation inside the menu list.
func (m homeModel) handleMenuKey(key string) (homeModel, tea.Cmd) {
	switch key {
	case "up":
		if m.menuIndex > 0 {
			m.menuIndex--
		}
		m.ensureMenuVisible()
		return m, nil
	case "down":
		if m.menuIndex < len(profileMenuItems)-1 {
			m.menuIndex++
		}
		m.ensureMenuVisible()
		return m, nil
	case "home", "g":
		m.menuIndex = 0
		m.ensureMenuVisible()
		return m, nil
	case "end", "G":
		m.menuIndex = len(profileMenuItems) - 1
		m.ensureMenuVisible()
		return m, nil
	case "enter":
		return m.openDetail()
	case "r":
		return m.startRefresh()
	case "c":
		return m, func() tea.Msg { return openChatMsg{} }
	case "l":
		return m, logoutCmd(m.client, m.token)
	case "q":
		return m, tea.Quit
	case "esc", "left":
		m.focus = profileSidebar
		return m, nil
	}
	return m, nil
}

// handleDetailKey handles the detail pane (forms and info pages).
func (m homeModel) handleDetailKey(key string, msg tea.KeyMsg) (homeModel, tea.Cmd) {
	// Form mode: text inputs receive typing.
	if len(m.fields) > 0 {
		if m.detail == detailFormAddFriend && m.addFriendTabsFocus {
			switch key {
			case "left", "up":
				m.setAddFriendType(m.addFriendType - 1)
			case "right", "down":
				m.setAddFriendType(m.addFriendType + 1)
			case "enter":
				m.addFriendTabsFocus = false
				m.fieldFocus = 0
				return m, m.applyFieldFocus()
			case "esc":
				m.fields = nil
				m.detail = detailEmpty
				m.focus = profileMenu
				m.addFriendTabsFocus = false
			}
			return m, nil
		}
		switch key {
		case "esc":
			m.fields = nil
			m.detail = detailEmpty
			m.focus = profileMenu
			m.addFriendTabsFocus = false
			return m, nil
		case "enter":
			return m.submitForm()
		case "up":
			if m.fieldFocus > 0 {
				m.fieldFocus--
			}
			return m, m.applyFieldFocus()
		case "down":
			if m.fieldFocus < len(m.fields)-1 {
				m.fieldFocus++
			}
			return m, m.applyFieldFocus()
		}
		idx := m.currentFieldIndex()
		if idx >= 0 && idx < len(m.fields) {
			var cmd tea.Cmd
			m.fields[idx], cmd = m.fields[idx].Update(msg)
			return m, cmd
		}
		return m, nil
	}

	switch key {
	case "esc":
		m.detail = detailEmpty
		m.focus = profileMenu
		return m, nil
	case "enter":
		if m.detail == detailLogoutConfirm {
			return m, logoutCmd(m.client, m.token)
		}
	case "l":
		if m.detail == detailLogoutConfirm {
			return m, logoutCmd(m.client, m.token)
		}
	}
	return m, nil
}

// handleTab cycles focus panes (sidebar → menu → detail) while in the page.
func (m homeModel) handleTab() (homeModel, tea.Cmd) {
	switch m.focus {
	case profileSidebar:
		m.focus = profileMenu
	case profileMenu:
		if m.detail != detailEmpty {
			m.focus = profileDetail
		}
	case profileDetail:
		m.focus = profileSidebar
	}
	return m, m.applyFieldFocus()
}

// handleTabBack is the reverse of handleTab.
func (m homeModel) handleTabBack() (homeModel, tea.Cmd) {
	switch m.focus {
	case profileDetail:
		m.focus = profileMenu
	case profileMenu:
		m.focus = profileSidebar
	case profileSidebar:
		if m.detail != detailEmpty {
			m.focus = profileDetail
		} else {
			m.focus = profileMenu
		}
	}
	return m, m.applyFieldFocus()
}

// openDetail opens the detail page of the selected menu item.
func (m homeModel) openDetail() (homeModel, tea.Cmd) {
	item := profileMenuItems[m.menuIndex]
	m.detail = item.Detail
	m.errText = ""
	m.okText = ""
	m.userData = nil
	m.focus = profileDetail

	switch item.Detail {
	case detailUserData:
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, loadUserDataCmd(m.client, m.token))
	case detailFormNickname:
		m.initFields([]fieldSpec{{Placeholder: "new nickname", Value: m.nickname()}})
		return m, m.applyFieldFocus()
	case detailFormAvatar:
		m.initFields([]fieldSpec{{Placeholder: "https://.../avatar.png"}})
		return m, m.applyFieldFocus()
	case detailFormAddFriend:
		m.addFriendType = 0
		m.addFriendTabsFocus = false
		m.initFields([]fieldSpec{
			{Placeholder: "user id"},
			{Placeholder: "verification message"},
		})
		m.setAddFriendIDPlaceholder()
		return m, m.applyFieldFocus()
	}
	return m, nil
}

// fieldSpec describes one text input of a profile form.
type fieldSpec struct {
	Placeholder string
	Value       string
}

// initFields creates the form text inputs.
func (m *homeModel) initFields(specs []fieldSpec) {
	m.fields = make([]textinput.Model, 0, len(specs))
	for _, spec := range specs {
		input := newTextInput(spec.Placeholder, false, 256)
		input.SetValue(spec.Value)
		m.fields = append(m.fields, input)
	}
	m.fieldFocus = 0
}

// currentFieldIndex returns the focused form field index.
func (m homeModel) currentFieldIndex() int {
	if m.fieldFocus < 0 || m.fieldFocus >= len(m.fields) {
		return -1
	}
	return m.fieldFocus
}

// applyFieldFocus focuses the active form input.
func (m *homeModel) applyFieldFocus() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.fields))
	for i := range m.fields {
		if !m.addFriendTabsFocus && i == m.fieldFocus {
			cmds = append(cmds, m.fields[i].Focus())
		} else {
			m.fields[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

// cycleFormFocus moves the focus inside an open form.
func (m homeModel) cycleFormFocus(delta int) (homeModel, tea.Cmd) {
	if m.detail != detailFormAddFriend {
		if len(m.fields) == 0 {
			return m, nil
		}
		n := len(m.fields)
		m.fieldFocus = ((m.fieldFocus+delta)%n + n) % n
		return m, m.applyFieldFocus()
	}
	// Add-friend dialog slots: 0 = type tabs, 1 = ID, 2 = verification message.
	slot := 1 + m.fieldFocus
	if m.addFriendTabsFocus {
		slot = 0
	}
	slot = ((slot+delta)%3 + 3) % 3
	m.addFriendTabsFocus = slot == 0
	m.fieldFocus = 0
	if slot > 0 {
		m.fieldFocus = slot - 1
	}
	return m, m.applyFieldFocus()
}

// setAddFriendType switches the add-friend target type (0 user / 1 group / 2 bot).
func (m *homeModel) setAddFriendType(value int) {
	m.addFriendType = clamp(value, 0, 2)
	m.setAddFriendIDPlaceholder()
}

// setAddFriendIDPlaceholder updates the ID input placeholder for the type.
func (m *homeModel) setAddFriendIDPlaceholder() {
	if len(m.fields) == 0 {
		return
	}
	placeholders := []string{"user id", "group id", "bot id"}
	m.fields[0].Placeholder = placeholders[clamp(m.addFriendType, 0, len(placeholders)-1)]
}

// submitForm validates and submits the current form.
func (m homeModel) submitForm() (homeModel, tea.Cmd) {
	switch m.detail {
	case detailFormNickname:
		name := strings.TrimSpace(m.fields[0].Value())
		if name == "" {
			m.errText = "昵称不能为空"
			return m, nil
		}
		m.loading = true
		if m.user != nil && m.user.Data != nil {
			m.user.Data.Name = name
		}
		return m, tea.Batch(m.spinner.Tick, editNicknameCmd(m.client, m.token, name))
	case detailFormAvatar:
		avatarURL := strings.TrimSpace(m.fields[0].Value())
		if avatarURL == "" {
			m.errText = "头像 URL 不能为空"
			return m, nil
		}
		m.loading = true
		if m.user != nil && m.user.Data != nil {
			m.user.Data.AvatarURL = avatarURL
		}
		return m, tea.Batch(m.spinner.Tick, editAvatarCmd(m.client, m.token, avatarURL))
	case detailFormAddFriend:
		chatID := strings.TrimSpace(m.fields[0].Value())
		verifyMessage := strings.TrimSpace(m.fields[1].Value())
		if chatID == "" {
			m.errText = "请输入 ID"
			return m, nil
		}
		chatType := []api.ChatType{api.ChatTypeUser, api.ChatTypeGroup, api.ChatTypeBot}[clamp(m.addFriendType, 0, 2)]
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, addFriendCmd(m.client, m.token, chatID, chatType, verifyMessage))
	}
	return m, nil
}

// ensureMenuVisible keeps the selected menu item inside the viewport.
func (m *homeModel) ensureMenuVisible() {
	height := m.menuViewportHeight()
	if height <= 0 {
		return
	}
	if m.menuIndex < m.menuOffset {
		m.menuOffset = m.menuIndex
	}
	if m.menuIndex >= m.menuOffset+height {
		m.menuOffset = m.menuIndex - height + 1
	}
	if m.menuOffset < 0 {
		m.menuOffset = 0
	}
}

// nickname returns the current nickname for prefilling the form.
func (m homeModel) nickname() string {
	if m.user != nil && m.user.Data != nil {
		return m.user.Data.Name
	}
	return ""
}

// ---------------------------------------------------------------------------
// layout

// innerWidth is the usable body width.
func (m homeModel) innerWidth() int {
	width, _ := screenSize(m.width, m.height)
	return width - 2*chromePadX
}

// bodyHeight is the number of body rows.
func (m homeModel) bodyHeight() int {
	_, height := screenSize(m.width, m.height)
	return maxInt(8, height-4)
}

// menuPanelWidth returns the width of the profile card + menu column.
func (m homeModel) menuPanelWidth(inner int) int {
	width := clamp(inner/3, 24, 34)
	maxWidth := inner - sidebarWidth - 2*panelGap - 26
	if width > maxWidth {
		width = maxInt(16, maxWidth)
	}
	return width
}

// cardHeight is the number of lines used by the profile card.
func (m homeModel) cardHeight() int { return 4 }

// menuViewportHeight returns how many menu rows fit below the card.
func (m homeModel) menuViewportHeight() int {
	return maxInt(3, m.bodyHeight()-m.cardHeight()-2)
}

// View implements tea.Model for the "我的" page.
func (m homeModel) View() string {
	width, height := screenSize(m.width, m.height)
	inner := width - 2*chromePadX
	bodyH := maxInt(8, height-4)

	menuW := m.menuPanelWidth(inner)
	detailW := inner - sidebarWidth - 2*panelGap - menuW
	if detailW < 20 {
		detailW = maxInt(20, inner-sidebarWidth-2*panelGap-menuW)
	}

	sidebar := renderSidebar(bodyH, sectionProfile, m.sidebarCursor, m.focus == profileSidebar)
	menu := m.menuColumn(bodyH, menuW)
	detail := m.detailColumn(bodyH, detailW)
	body := joinColumns(sidebar, menu, detail)

	return screenChromeTop(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		okStyle.Render("● 我的"),
		body,
		m.viewHints(),
	)
}

// menuColumn renders the profile card, the menu list and the status line.
func (m homeModel) menuColumn(bodyH, width int) []string {
	lines := make([]string, 0, bodyH)

	name := "未登录"
	id := "-"
	coin := "-"
	vip := "-"
	avatar := "-"
	if m.user != nil && m.user.Data != nil {
		data := m.user.Data
		if data.Name != "" {
			name = data.Name
		}
		id = data.ID
		coin = fmt.Sprintf("%.2f", data.Coin)
		vip = vipLabel(data.VIPStatus, data.IsVIP)
		if data.AvatarURL != "" {
			avatar = truncate(data.AvatarURL, maxInt(8, width-8))
		}
	}
	lines = append(lines,
		padLine(chatTitleStyle.Render(" "+truncate(name, maxInt(4, width-2))), width),
		padLine(labelStyle.Render(" ID  ")+valueStyle.Render(truncate(id, maxInt(4, width-8))), width),
		padLine(hintStyle.Render(truncate(fmt.Sprintf(" 金币 %s · VIP %s", coin, vip), maxInt(4, width-1))), width),
		padLine(labelStyle.Render(" 头像 ")+hintStyle.Render(avatar), width),
		separator(width),
	)

	// Menu viewport.
	height := maxInt(1, bodyH-len(lines)-1)
	items := make([]string, 0, len(profileMenuItems))
	for i, item := range profileMenuItems {
		marker := "  "
		style := profileMenuStyle
		if i == m.menuIndex {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌ ")
			style = profileMenuSelectedStyle
		}
		label := item.Label
		if item.Detail == detailPlaceholderCreateGroup || item.Detail == detailPlaceholderCreateBot ||
			item.Detail == detailPlaceholderBotConsole {
			label += "（待支持）"
		}
		items = append(items, marker+style.Render(truncate(label, maxInt(4, width-2))))
	}
	offset := clamp(m.menuOffset, 0, maxInt(0, len(items)-1))
	for i := 0; i < height; i++ {
		index := offset + i
		if index < len(items) {
			lines = append(lines, padLine(items[index], width))
		} else {
			lines = append(lines, padLine("", width))
		}
	}
	if len(lines) < bodyH {
		lines = append(lines, padLine(m.statusLine(width), width))
	}
	for len(lines) < bodyH {
		lines = append(lines, padLine("", width))
	}
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	return lines
}

// statusLine renders the loading/error/ok line of the page.
func (m homeModel) statusLine(width int) string {
	switch {
	case m.loading:
		return truncate(lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ", hintStyle.Render("处理中…")), width)
	case m.errText != "":
		return errStyle.Render(truncate(m.errText, width))
	case m.okText != "":
		return okStyle.Render(truncate(m.okText, width))
	}
	return ""
}

// detailColumn renders the right-hand detail pane.
func (m homeModel) detailColumn(bodyH, width int) []string {
	lines := []string{}
	switch m.detail {
	case detailEmpty:
		lines = append(lines, "", hintStyle.Render("从左侧选择一项操作"), "")
		for _, item := range profileMenuItems {
			lines = append(lines, hintStyle.Render(truncate("  · "+item.Label+"  "+item.Hint, maxInt(8, width-2))))
		}
	case detailUserData:
		lines = append(lines, titleAccentStyle.Render("个人资料"), "")
		if m.loading {
			lines = append(lines, hintStyle.Render("正在加载个人资料…"))
			break
		}
		if m.userData == nil {
			lines = append(lines, hintStyle.Render("暂无个人资料数据，按 Esc 返回后重试"))
			break
		}
		data := m.userData
		gender := "未设置"
		switch data.Gender {
		case 1:
			gender = "男"
		case 2:
			gender = "女"
		case 3:
			gender = "其他"
		}
		introduction := data.Introduction
		if introduction == "" {
			introduction = "（未填写）"
		}
		region := strings.TrimSpace(data.Province + " " + data.City + " " + data.District)
		if region == "" {
			region = "（未填写）"
		}
		rows := [][2]string{
			{"用户 ID", data.UserID},
			{"个人简介", introduction},
			{"性别", gender},
			{"所在地", region},
			{"地区码", data.LocationCode},
		}
		if m.user != nil && m.user.Data != nil {
			rows = append(rows,
				[2]string{"手机号", maskPhone(m.user.Data.Phone)},
				[2]string{"邮箱", maskEmail(m.user.Data.Email)},
				[2]string{"邀请码", m.user.Data.InvitationCode},
			)
		}
		for _, row := range rows {
			lines = append(lines, m.detailRow(width, row[0], row[1]))
		}
	case detailFormAddFriend:
		return m.addFriendDialog(bodyH, width)
	case detailFormNickname, detailFormAvatar:
		lines = append(lines, titleAccentStyle.Render(truncate(m.formTitle(), maxInt(8, width-2))), "")
		lines = append(lines, hintStyle.Render(truncate("  "+m.formHint(), maxInt(8, width-2))), "")
		for i, field := range m.fields {
			label := m.formFieldLabel(i)
			lines = append(lines, labelStyle.Render(truncate(" "+label, maxInt(8, width-2))))
			input := field
			input.Width = maxInt(4, width-6)
			style := inputStyle
			if m.focus == profileDetail && i == m.fieldFocus {
				style = inputFocusedStyle
			}
			rendered := style.Width(maxInt(8, width-2)).Render(input.View())
			lines = append(lines, strings.Split(rendered, "\n")...)
			lines = append(lines, "")
		}
		lines = append(lines, hintStyle.Render("  Enter 提交 · Tab/↑↓ 切换 · Esc 返回"))
		if m.loading {
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Left, m.spinner.View(), " 提交中…"))
		}
	case detailLogoutConfirm:
		lines = append(lines, titleAccentStyle.Render("退出登录"), "")
		lines = append(lines, hintStyle.Render(truncate("  Enter / l 确认退出并清除本地 token", maxInt(8, width-2))))
		lines = append(lines, hintStyle.Render("  Esc 取消"))
	case detailSettings:
		lines = append(lines, titleAccentStyle.Render("设置"), "")
		lines = append(lines,
			m.detailRow(width, "配置文件", m.configPath()),
			m.detailRow(width, "登录状态", m.loginState()),
			m.detailRow(width, "设备 ID", shortDeviceID(m.deviceID())),
			m.detailRow(width, "平台", m.platformLabel()),
			m.detailRow(width, "版本", Version),
		)
		lines = append(lines, hintStyle.Render(truncate("  （配置保存在用户主目录下，0600 权限）", maxInt(8, width-2))))

	case detailAbout:
		lines = append(lines, titleAccentStyle.Render("关于"), "")
		lines = append(lines, m.detailRow(width, "作者", "1ctrl-cv"), "")
		lines = append(lines,
			m.detailRow(width, "程序", "yhchat-tui"),
			m.detailRow(width, "版本", Version),
			m.detailRow(width, "构建", BuildTime),
			m.detailRow(width, "commit", Commit),
			m.detailRow(width, "引擎", "Go + Bubble Tea"),
		)
		lines = append(lines, "")
		lines = append(lines, hintStyle.Render(truncate(" 项目地址：https://github.com/1ctrl-cv/yhchat-TUI", maxInt(8, width-2))))
	case detailPlaceholderCreateGroup, detailPlaceholderCreateBot, detailPlaceholderBotConsole:
		lines = append(lines, titleAccentStyle.Render(truncate(m.placeholderTitle(), maxInt(8, width-2))), "")
		lines = append(lines, hintStyle.Render(" 该功能将在后续版本实现。"))
		lines = append(lines, hintStyle.Render(truncate(" 当前已支持：登录、聊天、通讯录、社区浏览、个人资料/昵称/头像修改。", maxInt(8, width-2))))
	}

	// Pad/trim to bodyH.
	out := make([]string, 0, bodyH)
	for _, line := range lines {
		for _, part := range strings.Split(line, "\n") {
			out = append(out, padLine(part, width))
		}
	}
	for len(out) < bodyH {
		out = append(out, "")
	}
	if len(out) > bodyH {
		out = out[:bodyH]
	}
	return out
}

// addFriendDialog renders the centered add-friend dialog.
func (m homeModel) addFriendDialog(bodyH, width int) []string {
	boxWidth := clamp(width-4, 30, 58)
	interior := maxInt(18, boxWidth-6)

	title := lipgloss.PlaceHorizontal(interior, lipgloss.Center,
		titleAccentStyle.Render("添加好友"))
	subtitle := lipgloss.PlaceHorizontal(interior, lipgloss.Center,
		hintStyle.Render("输入用户、群或机器人的 ID"))

	typeLabels := []string{"好友", "群聊", "机器人"}
	var tabParts []string
	for i, label := range typeLabels {
		style := tabInactiveStyle
		if i == m.addFriendType {
			style = tabActiveStyle
		}
		if len(tabParts) > 0 {
			tabParts = append(tabParts, " ")
		}
		tabParts = append(tabParts, style.Render(label))
	}
	tabsLine := lipgloss.JoinHorizontal(lipgloss.Top, tabParts...)
	if m.addFriendTabsFocus {
		tabsLine = labelFocusedStyle.Render("‹ ") + tabsLine + labelFocusedStyle.Render(" ›")
	}
	tabs := lipgloss.PlaceHorizontal(interior, lipgloss.Center, tabsLine)

	idLabels := []string{"用户 ID", "群聊 ID", "机器人 ID"}
	idLabel := idLabels[clamp(m.addFriendType, 0, len(idLabels)-1)]
	verifyLabel := "验证消息"

	renderField := func(index int, label string) []string {
		field := m.fields[index]
		field.Width = maxInt(8, interior-6)
		fieldStyle := lipgloss.NewStyle().
			Width(maxInt(10, interior-2)).
			Padding(0, 1).
			Background(colorInputBg)
		focused := m.focus == profileDetail && !m.addFriendTabsFocus && m.fieldFocus == index
		labelStyleToUse := labelStyle
		prefix := "  "
		if focused {
			labelStyleToUse = labelFocusedStyle
			prefix = "▸ "
		}
		out := []string{labelStyleToUse.Render(truncate(prefix+label, interior))}
		out = append(out, strings.Split(fieldStyle.Render(field.View()), "\n")...)
		return out
	}

	content := []string{title, subtitle, "", tabs, ""}
	content = append(content, renderField(0, idLabel)...)
	content = append(content, "")
	content = append(content, renderField(1, verifyLabel)...)
	content = append(content, "")

	cancel := lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1).Render("取消")
	primary := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorOnAccent).
		Background(colorAccent).
		Padding(0, 1).
		Render("发送申请")
	content = append(content, lipgloss.PlaceHorizontal(interior, lipgloss.Center,
		lipgloss.JoinHorizontal(lipgloss.Top, cancel, "  ", primary)))
	if m.loading {
		content = append(content, lipgloss.PlaceHorizontal(interior, lipgloss.Center,
			lipgloss.JoinHorizontal(lipgloss.Top, m.spinner.View(), " ", hintStyle.Render("发送中…"))))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 2).
		Render(strings.Join(content, "\n"))
	boxLines := strings.Split(box, "\n")

	top := maxInt(0, (bodyH-len(boxLines))/2)
	out := make([]string, 0, bodyH)
	for i := 0; i < top && len(out) < bodyH; i++ {
		out = append(out, padLine("", width))
	}
	for _, line := range boxLines {
		if len(out) >= bodyH {
			break
		}
		out = append(out, padLine(lipgloss.PlaceHorizontal(width, lipgloss.Center, line), width))
	}
	for len(out) < bodyH {
		out = append(out, padLine("", width))
	}
	return out
}

// detailRow renders a label/value row inside the detail pane.
func (m homeModel) detailRow(width int, label, value string) string {
	const labelW = 10
	labelText := padLine(label, labelW)
	valueText := truncate(value, maxInt(4, width-labelW-3))
	return "  " + labelStyle.Render(labelText) + valueStyle.Render(valueText)
}

// formTitle returns the title of the active form.
func (m homeModel) formTitle() string {
	switch m.detail {
	case detailFormNickname:
		return "修改昵称"
	case detailFormAvatar:
		return "修改头像"
	case detailFormAddFriend:
		return "添加好友"
	}
	return ""
}

// formHint returns the hint line of the active form.
func (m homeModel) formHint() string {
	switch m.detail {
	case detailFormNickname:
		return "输入新的昵称后按 Enter 提交"
	case detailFormAvatar:
		return "输入新的头像图片 URL（http/https）"
	case detailFormAddFriend:
		return "输入用户、群或机器人的 ID"
	}
	return ""
}

// formFieldLabel returns the label of a form field.
func (m homeModel) formFieldLabel(index int) string {
	switch m.detail {
	case detailFormNickname:
		return "昵称"
	case detailFormAvatar:
		return "头像 URL"
	case detailFormAddFriend:
		if index == 0 {
			return "ID"
		}
		return "验证消息"
	}
	return fmt.Sprintf("字段 %d", index+1)
}

// placeholderTitle returns the title of a "coming soon" detail page.
func (m homeModel) placeholderTitle() string {
	switch m.detail {
	case detailPlaceholderCreateGroup:
		return "创建群聊"
	case detailPlaceholderCreateBot:
		return "创建机器人"
	default:
		return "机器人控制台"
	}
}

// viewHints returns the footer hint bar.
func (m homeModel) viewHints() string {
	switch m.focus {
	case profileSidebar:
		return sidebarHint()
	case profileDetail:
		if len(m.fields) > 0 {
			if m.detail == detailFormAddFriend {
				return "Tab 切换字段/类型 · ←→ 切换类型 · Enter 发送申请 · Esc 取消"
			}
			return "Tab/↑↓ 切换字段 · Enter 提交 · Esc 返回菜单"
		}
		return "Enter 确认 · Esc 返回菜单 · Tab 切换焦点"
	default:
		return "↑↓ 选择功能 · Enter 打开 · c 聊天 · l 退出登录 · q 退出"
	}
}

// configPath returns the effective config file path for display.
func (m homeModel) configPath() string {
	if m.configPathText == "" {
		return "（未知）"
	}
	return m.configPathText
}

// deviceID returns the current device id for display.
func (m homeModel) deviceID() string {
	if m.deviceIDText == "" {
		return "未知"
	}
	return m.deviceIDText
}

// platformLabel returns the login platform involved by the API client.
func (m homeModel) platformLabel() string {
	if m.client != nil {
		return m.client.Platform
	}
	return "unknown"
}

// loginState describes whether a token is stored.
func (m homeModel) loginState() string {
	if m.token == "" {
		return "未登录"
	}
	return "已登录（token 已保存）"
}

// shortDeviceID hides most of the device id.
func shortDeviceID(id string) string {
	if id == "" || id == "未知" {
		return id
	}
	if len(id) > 10 {
		return id[:6] + "…" + id[len(id)-4:]
	}
	return id
}
