package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
)

// contactsFocus identifies the focused pane of the contacts page.
type contactsFocus int

const (
	contactsSidebar contactsFocus = iota
	contactsList
)

// contactsLoadedMsg carries the address book of the account.
type contactsLoadedMsg struct {
	groups []api.ContactGroup
	err    error
}

// friendRequestsLoadedMsg carries the pending friend/group requests.
type friendRequestsLoadedMsg struct {
	requests *api.FriendRequests
	err      error
}

// contactRowKind distinguishes section headers from list entries.
type contactRowKind int

const (
	contactRowHeader contactRowKind = iota
	contactRowRequest
	contactRowContact
)

// contactRow is one flattened row of the contacts list.
type contactRow struct {
	kind       contactRowKind
	section    string // section key: "requests" or group name
	label      string
	count      int
	expandable bool
	expanded   bool
	request    *api.InviteRequest
	contact    *api.Contact
	chatType   api.ChatType
}

// contactsModel is the "通讯录" page.
type contactsModel struct {
	client *api.Client
	token  string
	user   *api.UserInfo

	width, height int

	focus         contactsFocus
	sidebarCursor section

	groups   []api.ContactGroup
	requests *api.FriendRequests
	expanded map[string]bool
	rows     []contactRow
	selected int
	offset   int

	loading bool
	errText string
	okText  string
	spinner spinner.Model
}

// newContactsModel creates the contacts page.
func newContactsModel(client *api.Client, token string) contactsModel {
	return contactsModel{
		client:        client,
		token:         token,
		focus:         contactsList,
		sidebarCursor: sectionContacts,
		expanded:      map[string]bool{},
		spinner:       newSpinner(),
	}
}

// withUser attaches the account profile.
func (m contactsModel) withUser(user *api.UserInfo) contactsModel {
	m.user = user
	return m
}

// resize records the terminal size.
func (m contactsModel) resize(width, height int) contactsModel {
	m.width, m.height = width, height
	return m
}

// init starts loading the address book and requests.
func (m contactsModel) init() (contactsModel, tea.Cmd) {
	m.loading = true
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick,
		loadAddressBookCmd(m.client, m.token),
		loadFriendRequestsCmd(m.client, m.token))
}

// refresh reloads the address book.
func (m contactsModel) refresh() (contactsModel, tea.Cmd) {
	m.loading = true
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick,
		loadAddressBookCmd(m.client, m.token),
		loadFriendRequestsCmd(m.client, m.token))
}

// loadAddressBookCmd fetches the contact groups.
func loadAddressBookCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		groups, err := client.AddressBook(ctx, token)
		return contactsLoadedMsg{groups: groups, err: err}
	}
}

// loadFriendRequestsCmd fetches pending friend/invite requests.
func loadFriendRequestsCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		requests, err := client.FriendRequests(ctx, token)
		return friendRequestsLoadedMsg{requests: requests, err: err}
	}
}

// Update handles messages for the contacts page.
func (m contactsModel) Update(msg tea.Msg) (contactsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case contactsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			return m.handleLoadError(msg.err, "加载通讯录失败")
		}
		m.groups = msg.groups
		m.errText = ""
		m.rebuildRows()
		return m, nil

	case friendRequestsLoadedMsg:
		if msg.err != nil {
			if api.IsSessionExpired(msg.err) {
				return m, func() tea.Msg { return expiringTokenMsg{err: msg.err} }
			}
			m.errText = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.requests = msg.requests
		m.rebuildRows()
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleMouse handles click and wheel events on the contacts page.
func (m contactsModel) handleMouse(msg tea.MouseMsg) (contactsModel, tea.Cmd) {
	x, y, ok := mouseBodyLocal(msg, m.width, m.height)
	if !ok || m.width == 0 {
		return m, nil
	}
	inner := m.innerWidth()
	bodyH := m.bodyHeight()
	listW := m.listPanelWidth(inner)
	listStart, detailStart := standardColumns(inner, listW)

	if delta := wheelDelta(msg); delta != 0 {
		if x >= listStart && x < detailStart {
			m.moveSelection(delta * 2)
		}
		return m, nil
	}
	if !isLeftClick(msg) {
		return m, nil
	}
	if x < sidebarWidth {
		if sec, hit := sectionAtRow(y); hit {
			m.sidebarCursor = sec
			if sec == sectionContacts {
				m.focus = contactsList
				return m, nil
			}
			return m, func() tea.Msg { return switchSectionMsg{to: sec} }
		}
		return m, nil
	}
	if x < listStart {
		return m, nil
	}
	if x < detailStart {
		// Header row (0) + separator (1), one line per row afterwards.
		if y < 2 {
			return m, nil
		}
		index := m.offset + (y - 2)
		if index < 0 || index >= len(m.rows) {
			return m, nil
		}
		m.selected = index
		m.focus = contactsList
		if m.rows[index].kind == contactRowHeader {
			m.toggleRow()
		}
		return m, nil
	}
	// "Enter chat" button pinned to the bottom of the detail pane.
	row := m.selectedRow()
	if row == nil || row.kind != contactRowContact {
		return m, nil
	}
	if y < bodyH-3 || y >= bodyH {
		return m, nil
	}
	chatID := row.contact.ID
	chatType := row.chatType
	name := row.contact.DisplayName()
	avatarURL := row.contact.AvatarURL
	return m, func() tea.Msg {
		return openContactChatMsg{chatID: chatID, chatType: chatType, name: name, avatarURL: avatarURL}
	}
}

// handleLoadError handles a failed load (session expiry is forwarded).
func (m contactsModel) handleLoadError(err error, prefix string) (contactsModel, tea.Cmd) {
	m.loading = false
	if api.IsSessionExpired(err) {
		return m, func() tea.Msg { return expiringTokenMsg{err: err} }
	}
	m.errText = prefix + ": " + api.FriendlyMessage(err)
	return m, nil
}

// handleKey processes key presses.
func (m contactsModel) handleKey(msg tea.KeyMsg) (contactsModel, tea.Cmd) {
	key := msg.String()

	if key == "tab" {
		if m.focus == contactsSidebar {
			m.focus = contactsList
		} else {
			m.focus = contactsSidebar
		}
		return m, nil
	}
	if key == "shift+tab" {
		return m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	}

	if m.focus == contactsSidebar {
		switch key {
		case "up":
			m.sidebarCursor = nextSectionCursor(m.sidebarCursor, -1)
			return m, nil
		case "down":
			m.sidebarCursor = nextSectionCursor(m.sidebarCursor, 1)
			return m, nil
		case "enter":
			if target := m.sidebarCursor; target != sectionContacts {
				return m, func() tea.Msg { return switchSectionMsg{to: target} }
			}
			m.focus = contactsList
			return m, nil
		case "right":
			m.focus = contactsList
			return m, nil
		case "r":
			return m.refresh()
		case "c":
			return m, func() tea.Msg { return openChatMsg{} }
		}
		return m, nil
	}

	// List focus.
	switch key {
	case "up":
		m.moveSelection(-1)
		return m, nil
	case "down":
		m.moveSelection(1)
		return m, nil
	case "pgup":
		m.moveSelection(-m.listViewportHeight())
		return m, nil
	case "pgdown":
		m.moveSelection(m.listViewportHeight())
		return m, nil
	case "home", "g":
		m.selected = 0
		m.ensureVisible()
		return m, nil
	case "end", "G":
		if len(m.rows) > 0 {
			m.selected = len(m.rows) - 1
		}
		m.ensureVisible()
		return m, nil
	case "enter", "right":
		row := m.selectedRow()
		if row == nil {
			return m, nil
		}
		if row.kind == contactRowContact {
			chatID := row.contact.ID
			chatType := row.chatType
			name := row.contact.DisplayName()
			avatarURL := row.contact.AvatarURL
			return m, func() tea.Msg {
				return openContactChatMsg{chatID: chatID, chatType: chatType, name: name, avatarURL: avatarURL}
			}
		}
		m.toggleRow()
		return m, nil
	case "left":
		if row := m.selectedRow(); row != nil && row.kind == contactRowHeader && row.expanded {
			m.expanded[row.section] = false
			m.rebuildRows()
		}
		return m, nil
	case "r":
		return m.refresh()
	case "esc":
		m.focus = contactsSidebar
		return m, nil
	}
	return m, nil
}

// selectedRow returns the selected row (or nil).
func (m contactsModel) selectedRow() *contactRow {
	if len(m.rows) == 0 {
		return nil
	}
	index := clamp(m.selected, 0, len(m.rows)-1)
	return &m.rows[index]
}

// moveSelection moves the list selection.
func (m *contactsModel) moveSelection(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.selected = clamp(m.selected+delta, 0, len(m.rows)-1)
	m.ensureVisible()
}

// ensureVisible adjusts the scroll offset so the selection is visible.
func (m *contactsModel) ensureVisible() {
	height := m.listViewportHeight()
	if height <= 0 {
		return
	}
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+height {
		m.offset = m.selected - height + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// toggleRow expands/collapses a section header or activates a contact.
func (m *contactsModel) toggleRow() {
	row := m.selectedRow()
	if row == nil {
		return
	}
	if row.kind != contactRowHeader {
		return
	}
	m.expanded[row.section] = !m.expanded[row.section]
	m.rebuildRows()
}

// rebuildRows flattens groups/requests into the selectable row list.
func (m *contactsModel) rebuildRows() {
	rows := make([]contactRow, 0, 16)
	if m.requests != nil && (m.requests.Total > 0 || m.requests.Pending > 0) {
		rows = append(rows, contactRow{
			kind:       contactRowHeader,
			section:    "requests",
			label:      "申请 / 邀请",
			count:      int(m.requests.Pending),
			expandable: true,
			expanded:   m.expanded["requests"],
		})
		if m.expanded["requests"] {
			for i := range m.requests.Requests {
				request := m.requests.Requests[i]
				rows = append(rows, contactRow{
					kind:     contactRowRequest,
					section:  "requests",
					label:    inviteRequestLabel(request),
					request:  &request,
					chatType: api.ChatType(request.TargetType),
				})
			}
		}
	}
	for _, group := range m.groups {
		name := group.Name
		if name == "" {
			name = group.ChatType.Label()
		}
		rows = append(rows, contactRow{
			kind:       contactRowHeader,
			section:    name,
			label:      name,
			count:      len(group.Items),
			expandable: true,
			expanded:   m.expanded[name],
			chatType:   group.ChatType,
		})
		if m.expanded[name] {
			for i := range group.Items {
				contact := group.Items[i]
				rows = append(rows, contactRow{
					kind:     contactRowContact,
					section:  name,
					label:    contact.DisplayName(),
					contact:  &contact,
					chatType: group.ChatType,
				})
			}
		}
	}
	m.rows = rows
	if m.selected >= len(rows) {
		m.selected = maxInt(0, len(rows)-1)
	}
	m.ensureVisible()
}

// inviteRequestLabel returns the display label of a request row.
func inviteRequestLabel(request api.InviteRequest) string {
	if request.GroupName != "" && request.Name != "" {
		return request.GroupName + " · " + request.Name
	}
	for _, candidate := range []string{request.Name, request.GroupName, request.BotName, request.InviterID, request.TargetID} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return fmt.Sprintf("请求 #%d", request.RequestID)
}

// ---------------------------------------------------------------------------
// layout

// innerWidth is the usable body width.
func (m contactsModel) innerWidth() int {
	width, _ := screenSize(m.width, m.height)
	return width - 2*chromePadX
}

// bodyHeight is the number of body rows.
func (m contactsModel) bodyHeight() int {
	_, height := screenSize(m.width, m.height)
	return maxInt(8, height-4)
}

// listPanelWidth returns the width of the contacts list column.
func (m contactsModel) listPanelWidth(inner int) int {
	width := clamp(inner/3, 24, 34)
	if inner-width-2*panelGap-26 < 26 {
		width = maxInt(18, inner-2*panelGap-26)
	}
	return width
}

// listViewportHeight returns the number of visible list rows.
func (m contactsModel) listViewportHeight() int {
	return maxInt(3, m.bodyHeight()-2)
}

// View implements tea.Model for the contacts page.
func (m contactsModel) View() string {
	width, height := screenSize(m.width, m.height)
	inner := width - 2*chromePadX
	bodyH := maxInt(8, height-4)

	listW := m.listPanelWidth(inner)
	detailW := inner - sidebarWidth - 2*panelGap - listW
	if detailW < 20 {
		detailW = maxInt(20, inner-sidebarWidth-2*panelGap-listW)
	}

	sidebar := renderSidebar(bodyH, sectionContacts, m.sidebarCursor, m.focus == contactsSidebar)
	list := m.listColumn(bodyH, listW)
	detail := m.detailColumn(bodyH, detailW)
	body := joinColumns(sidebar, list, detail)

	return screenChromeTop(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		subtitleStyle.Render("通讯录"),
		body,
		m.viewHints(),
	)
}

// listColumn renders the section/contact list.
func (m contactsModel) listColumn(bodyH, width int) []string {
	lines := make([]string, 0, bodyH)
	header := " 通讯录"
	if m.requests != nil && m.requests.Pending > 0 {
		header += fmt.Sprintf(" · %d 条待处理", m.requests.Pending)
	}
	lines = append(lines, padLine(chatTitleStyle.Render(truncate(header, width)), width))
	lines = append(lines, separator(width))

	height := maxInt(1, bodyH-3)
	offset := clamp(m.offset, 0, maxInt(0, len(m.rows)-1))
	for i := 0; i < height; i++ {
		index := offset + i
		if index >= len(m.rows) {
			lines = append(lines, padLine("", width))
			continue
		}
		lines = append(lines, padLine(m.renderRow(m.rows[index], width), width))
	}
	if len(m.rows) == 0 {
		lines = append(lines, padLine(hintStyle.Render(" 暂无联系人，按 r 刷新"), width))
	}
	lines = append(lines, padLine(m.statusLine(width), width))
	for len(lines) < bodyH {
		lines = append(lines, padLine("", width))
	}
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	return lines
}

// renderRow renders one flattened list row of the given width.
func (m contactsModel) renderRow(row contactRow, width int) string {
	selected := len(m.rows) > 0 && row == m.rows[clamp(m.selected, 0, len(m.rows)-1)]
	switch row.kind {
	case contactRowHeader:
		marker := "  "
		style := convNameStyle
		if selected {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌ ")
			style = convSelectedStyle
		}
		arrow := "▸"
		if row.expandable && row.expanded {
			arrow = "▾"
		}
		if !row.expandable {
			arrow = " "
		}
		count := ""
		if row.count > 0 {
			count = fmt.Sprintf(" %d", row.count)
		}
		text := arrow + " " + row.label + count
		return marker + style.Render(truncate(text, maxInt(4, width-3)))
	case contactRowRequest:
		marker := "    "
		style := convPreviewStyle
		if selected {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("  ▌ ")
			style = convSelectedStyle
		}
		return marker + style.Render(truncate("✉ "+row.label, maxInt(4, width-5)))
	default:
		marker := "    "
		style := convNameStyle
		if selected {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("  ▌ ")
			style = convSelectedStyle
		}
		text := row.label
		if row.contact != nil && row.contact.PermissionLevel == 100 {
			text += "（群主）"
		} else if row.contact != nil && row.contact.PermissionLevel == 2 {
			text += "（管理员）"
		}
		return marker + style.Render(truncate(text, maxInt(4, width-5)))
	}
}

// statusLine renders the loading/error/ok line.
func (m contactsModel) statusLine(width int) string {
	switch {
	case m.loading:
		return truncate(lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ", hintStyle.Render("加载通讯录…")), width)
	case m.errText != "":
		return errStyle.Render(truncate(m.errText, width))
	case m.okText != "":
		return okStyle.Render(truncate(m.okText, width))
	case len(m.groups) == 0 && m.requests == nil:
		return hintStyle.Render("")
	}
	return hintStyle.Render(truncate(fmt.Sprintf("共 %d 行 · Enter 展开/收起", len(m.rows)), width))
}

// detailColumn renders the details of the selected row.
func (m contactsModel) detailColumn(bodyH, width int) []string {
	lines := make([]string, 0, bodyH)
	row := m.selectedRow()
	if row == nil {
		lines = append(lines, "", hintStyle.Render(" 暂无数据"), "")
		lines = append(lines, hintStyle.Render(" 按 r 重新加载"))
		return padLines(lines, bodyH, width)
	}

	button := ""
	switch row.kind {
	case contactRowHeader:
		// A collapsed section has no detail page of its own; the right pane
		// stays empty until an actual entry is selected.
		if !row.expanded {
			return padLines(nil, bodyH, width)
		}
		lines = append(lines, "", hintStyle.Render(" 从下方选择条目查看主页"))
	case contactRowContact:
		contact := row.contact
		lines = append(lines, titleAccentStyle.Render(contact.DisplayName()), "")
		if contact.Remark != "" {
			lines = append(lines, m.detailRow(width, "备注", contact.Remark))
		}
		lines = append(lines, m.detailRow(width, "ID", contact.ID))
		lines = append(lines, m.detailRow(width, "类型", row.chatType.Label()))
		if row.chatType == api.ChatTypeGroup {
			level := "普通成员"
			switch contact.PermissionLevel {
			case 100:
				level = "群主"
			case 2:
				level = "管理员"
			}
			lines = append(lines, m.detailRow(width, "群内身份", level))
		}
		if contact.NoDisturb {
			lines = append(lines, m.detailRow(width, "消息提醒", "免打扰"))
		}
		if contact.AvatarURL != "" {
			lines = append(lines, "", hintStyle.Render(" 头像: "+truncate(contact.AvatarURL, maxInt(8, width-8))))
		}
		button = buttonFocusedStyle.Render("进入聊天")
	case contactRowRequest:
		request := row.request
		lines = append(lines, titleAccentStyle.Render("申请 / 邀请"), "")
		if request.Name != "" {
			lines = append(lines, m.detailRow(width, "请求方", request.Name))
		}
		if request.GroupName != "" {
			lines = append(lines, m.detailRow(width, "群聊", request.GroupName))
		}
		if request.BotName != "" {
			lines = append(lines, m.detailRow(width, "机器人", request.BotName))
		}
		if request.TargetID != "" {
			lines = append(lines, m.detailRow(width, "目标 ID", request.TargetID))
		}
		if request.Note != "" {
			lines = append(lines, m.detailRow(width, "备注", request.Note))
		}
		if request.InviteAtStr != "" {
			lines = append(lines, m.detailRow(width, "时间", request.InviteAtStr))
		}
		status := "待处理"
		switch request.Result {
		case 1:
			status = "已处理"
		}
		lines = append(lines, m.detailRow(width, "状态", status))
		lines = append(lines, "", hintStyle.Render(" 处理申请将在后续版本支持"))
	}
	if button == "" {
		return padLines(lines, bodyH, width)
	}
	// Pin the "enter chat" button to the bottom of the detail pane.
	buttonLines := strings.Split(button, "\n")
	contentH := maxInt(1, bodyH-len(buttonLines))
	out := padLines(lines, contentH, width)
	for _, line := range buttonLines {
		out = append(out, padLine("  "+line, width))
	}
	return out
}

// detailRow renders a label/value pair inside the detail pane.
func (m contactsModel) detailRow(width int, label, value string) string {
	const labelW = 10
	return "  " + labelStyle.Render(padLine(label, labelW)) +
		valueStyle.Render(truncate(value, maxInt(4, width-labelW-4)))
}

// viewHints returns the footer hint bar.
func (m contactsModel) viewHints() string {
	if m.focus == contactsSidebar {
		return sidebarHint()
	}
	if row := m.selectedRow(); row != nil && row.kind == contactRowContact {
		return "↑↓ 选择 · Enter 进入聊天 · r 刷新 · Esc 返回侧边栏 · Tab 切换焦点"
	}
	return "↑↓ 选择 · Enter 展开/收起 · r 刷新 · Esc 返回侧边栏 · Tab 切换焦点"
}

// padLines pads a list of lines to exactly height rows of the given width.
func padLines(lines []string, height, width int) []string {
	out := make([]string, 0, height)
	for _, line := range lines {
		for _, part := range strings.Split(line, "\n") {
			out = append(out, padLine(part, width))
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}
