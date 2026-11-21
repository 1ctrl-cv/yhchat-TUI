package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/cache"
)

// chatOverlay identifies a modal-ish overlay inside the chat window.
type chatOverlay int

const (
	overlayNone chatOverlay = iota
	overlayForward
	overlaySearch
	overlayGroupInfo
	overlayUserProfile
	overlayImageSend
)

// Messages produced by overlay commands.

type forwardDoneMsg struct {
	target string
	err    error
}

type searchDoneMsg struct {
	results []api.SearchResult
	err     error
}

type groupInfoLoadedMsg struct {
	info    *api.GroupInfo
	members []api.GroupMember
	total   int
	page    int
	err     error
}

type userProfileLoadedMsg struct {
	profile *api.UserProfile
	err     error
}

type imageSentMsg struct {
	msgID string
	err   error
}

// forwardCommand forwards one message to a single conversation.
func forwardCommand(client *api.Client, token string, msg api.Message, from api.ChatType, target api.Conversation) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		err := client.ForwardMessage(ctx, token, msg.MsgID, from,
			[]api.ForwardTarget{{ChatID: target.ChatID, ChatType: target.ChatType}})
		return forwardDoneMsg{target: target.DisplayName(), err: err}
	}
}

// searchMessagesCommand searches the history of the active conversation.
func searchMessagesCommand(client *api.Client, token, chatID string, chatType api.ChatType, word string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		results, err := client.SearchMessages(ctx, token, chatID, chatType, word, 30, 0)
		return searchDoneMsg{results: results, err: err}
	}
}

// loadGroupCommand fetches group info plus the first member page.
func loadGroupCommand(client *api.Client, token, groupID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		info, err := client.GroupInfo(ctx, token, groupID)
		if err != nil {
			return groupInfoLoadedMsg{err: err}
		}
		members, total, memberErr := client.GroupMembers(ctx, token, groupID, 1, 50, "")
		return groupInfoLoadedMsg{info: info, members: members, total: total, page: 1, err: memberErr}
	}
}

// loadGroupPageCommand fetches one member page.
func loadGroupPageCommand(client *api.Client, token, groupID string, page int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		members, total, err := client.GroupMembers(ctx, token, groupID, page, 50, "")
		return groupInfoLoadedMsg{info: nil, members: members, total: total, page: page, err: err}
	}
}

// loadUserProfileCommand fetches another user's profile page.
func loadUserProfileCommand(client *api.Client, token, userID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		profile, err := client.GetUserProfile(ctx, token, userID)
		return userProfileLoadedMsg{profile: profile, err: err}
	}
}

// uploadAndSendImageCommand uploads a local image and sends it as a message.
func uploadAndSendImageCommand(client *api.Client, token, chatID string, chatType api.ChatType, localPath string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*requestTimeout)
		defer cancel()
		if strings.HasPrefix(localPath, "http://") || strings.HasPrefix(localPath, "https://") {
			msgID, err := client.SendImageMessage(ctx, token, chatID, chatType, localPath)
			return imageSentMsg{msgID: msgID, err: err}
		}
		if info, err := os.Stat(localPath); err != nil || info.IsDir() {
			return imageSentMsg{err: fmt.Errorf("图片文件不存在: %s", localPath)}
		}
		_, fileURL, err := client.UploadFile(ctx, token, api.UploadImage, localPath)
		if err != nil {
			return imageSentMsg{err: err}
		}
		msgID, err := client.SendImageMessage(ctx, token, chatID, chatType, fileURL)
		return imageSentMsg{msgID: msgID, err: err}
	}
}

// openImagePathInput normalizes common quoting/whitespace around a file path.
func normalizePathInput(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			value = filepath.Join(home, value[2:])
		}
	}
	return value
}

// initOverlayInput creates the reusable overlay text input.
func newOverlayInput(placeholder string) textinput.Model {
	input := newTextInput(placeholder, false, 512)
	input.Placeholder = placeholder
	return input
}

// ---------------------------------------------------------------------------
// overlay lifecycle

// openOverlay switches to an overlay and resets its state.
func (m *mainWindowModel) openOverlay(overlay chatOverlay) {
	m.overlay = overlay
	m.overlayIndex = 0
	m.overlayOffset = 0
	m.overlayErr = ""
	m.overlayLoading = false
	m.overlayInput.SetValue("")
	m.overlayInput.Focus()
}

// closeOverlay leaves the overlay without touching the chat state.
func (m *mainWindowModel) closeOverlay() tea.Cmd {
	m.overlay = overlayNone
	m.overlayLoading = false
	m.overlayErr = ""
	m.overlayInput.Blur()
	return nil
}

// overlayAck routes one overlay message into the chat model. It is called from
// the main Update dispatch.
func (m mainWindowModel) handleOverlayMessage(msg tea.Msg) (mainWindowModel, tea.Cmd) {
	switch msg := msg.(type) {
	case forwardDoneMsg:
		m.overlayLoading = false
		if msg.err != nil {
			m.overlayErr = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.overlay = overlayNone
		m.okText = "已转发给 " + msg.target
		m.errText = ""
		return m, nil

	case searchDoneMsg:
		m.overlayLoading = false
		if msg.err != nil {
			if api.IsSessionExpired(msg.err) {
				return m, func() tea.Msg { return expiringTokenMsg{err: msg.err} }
			}
			m.overlayErr = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.searchResults = msg.results
		m.overlayIndex = 0
		if len(msg.results) == 0 {
			m.overlayErr = "没有搜索到匹配的消息"
		}
		return m, nil

	case groupInfoLoadedMsg:
		m.overlayLoading = false
		if msg.err != nil {
			if api.IsSessionExpired(msg.err) {
				return m, func() tea.Msg { return expiringTokenMsg{err: msg.err} }
			}
			m.overlayErr = api.FriendlyMessage(msg.err)
			return m, nil
		}
		if msg.info != nil {
			m.groupInfo = msg.info
			// The real list-member response reports total=0; fall back to the
			// group headcount from group info so paging/counts still work.
			if msg.total <= 0 && m.groupTotal <= 0 && msg.info.Headcount > 0 {
				m.groupTotal = int(msg.info.Headcount)
			}
		}
		if msg.total > 0 {
			m.groupTotal = msg.total
		}
		m.groupMembers = msg.members
		m.groupPage = msg.page
		m.overlayIndex = 0
		return m, m.applyOverlayInputFocus()

	case userProfileLoadedMsg:
		m.overlayLoading = false
		if msg.err != nil {
			if api.IsSessionExpired(msg.err) {
				return m, func() tea.Msg { return expiringTokenMsg{err: msg.err} }
			}
			m.overlayErr = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.userProfile = msg.profile
		return m, nil

	case imageSentMsg:
		m.overlayLoading = false
		if msg.err != nil {
			m.overlayErr = api.FriendlyMessage(msg.err)
			return m, nil
		}
		m.overlay = overlayNone
		m.okText = "图片已发送"
		m.errText = ""
		return m, m.appendLocalSentMessage(msg.msgID, "[图片]", api.MessageContent{}, 2)
	}
	return m, nil
}

// applyOverlayInputFocus focuses the overlay input when relevant.
func (m *mainWindowModel) applyOverlayInputFocus() tea.Cmd {
	switch m.overlay {
	case overlaySearch, overlayImageSend:
		return m.overlayInput.Focus()
	}
	return nil
}

// ---------------------------------------------------------------------------
// overlay keys

// handleOverlayKey handles key presses while an overlay is open.
func (m mainWindowModel) handleOverlayKey(msg tea.KeyMsg) (mainWindowModel, tea.Cmd) {
	key := msg.String()
	switch m.overlay {
	case overlayForward:
		switch key {
		case "esc":
			return m, m.closeOverlay()
		case "up":
			if m.overlayIndex > 0 {
				m.overlayIndex--
			}
			return m, nil
		case "down":
			if m.overlayIndex < len(m.forwardTargets)-1 {
				m.overlayIndex++
			}
			return m, nil
		case "enter":
			if m.overlayIndex < 0 || m.overlayIndex >= len(m.forwardTargets) {
				return m, nil
			}
			message := m.selectedMessage()
			conv := m.selectedConv()
			if message == nil || conv == nil {
				m.overlayErr = "没有可转发的消息"
				return m, nil
			}
			m.overlayLoading = true
			return m, forwardCommand(m.client, m.token, *message, conv.ChatType, m.forwardTargets[m.overlayIndex])
		}
		return m, nil

	case overlaySearch:
		switch key {
		case "esc":
			return m, m.closeOverlay()
		case "enter":
			word := strings.TrimSpace(m.overlayInput.Value())
			conv := m.selectedConv()
			if word == "" || conv == nil {
				return m, nil
			}
			m.overlayLoading = true
			m.overlayErr = ""
			return m, searchMessagesCommand(m.client, m.token, conv.ChatID, conv.ChatType, word)
		case "up":
			if m.overlayIndex > 0 {
				m.overlayIndex--
			}
			return m, nil
		case "down":
			if m.overlayIndex < len(m.searchResults)-1 {
				m.overlayIndex++
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.overlayInput, cmd = m.overlayInput.Update(msg)
		return m, cmd

	case overlayImageSend:
		switch key {
		case "esc":
			return m, m.closeOverlay()
		case "enter":
			conv := m.selectedConv()
			path := normalizePathInput(m.overlayInput.Value())
			if conv == nil || path == "" {
				return m, nil
			}
			m.overlayLoading = true
			m.overlayErr = ""
			return m, uploadAndSendImageCommand(m.client, m.token, conv.ChatID, conv.ChatType, path)
		}
		var cmd tea.Cmd
		m.overlayInput, cmd = m.overlayInput.Update(msg)
		return m, cmd

	case overlayGroupInfo:
		switch key {
		case "esc":
			return m, m.closeOverlay()
		case "up":
			if m.overlayIndex > 0 {
				m.overlayIndex--
			}
			return m, nil
		case "down":
			if m.overlayIndex < len(m.groupMembers)-1 {
				m.overlayIndex++
			}
			return m, nil
		case "pgup":
			if m.groupPage > 1 {
				m.overlayLoading = true
				return m, loadGroupPageCommand(m.client, m.token, m.groupInfo.GroupID, m.groupPage-1)
			}
			return m, nil
		case "pgdown":
			hasMore := m.groupPage*50 < m.groupTotal ||
				(m.groupTotal <= 0 && len(m.groupMembers) == 50)
			if hasMore {
				m.overlayLoading = true
				return m, loadGroupPageCommand(m.client, m.token, m.groupInfo.GroupID, m.groupPage+1)
			}
			return m, nil
		case "r":
			m.overlayLoading = true
			return m, loadGroupCommand(m.client, m.token, m.groupInfo.GroupID)
		}
		return m, nil

	case overlayUserProfile:
		if key == "esc" {
			return m, m.closeOverlay()
		}
		return m, nil
	}
	return m, m.closeOverlay()
}

// handleOverlayMouse handles clicks/wheel while an overlay is open.
func (m mainWindowModel) handleOverlayMouse(msg tea.MouseMsg) (mainWindowModel, tea.Cmd) {
	if isRightClick(msg) {
		return m, m.closeOverlay()
	}
	if delta := wheelDelta(msg); delta != 0 {
		switch m.overlay {
		case overlayForward:
			m.overlayIndex = clamp(m.overlayIndex+delta, 0, maxInt(0, len(m.forwardTargets)-1))
		case overlaySearch:
			m.overlayIndex = clamp(m.overlayIndex+delta, 0, maxInt(0, len(m.searchResults)-1))
		case overlayGroupInfo:
			m.overlayIndex = clamp(m.overlayIndex+delta, 0, maxInt(0, len(m.groupMembers)-1))
		}
		return m, nil
	}
	if !isLeftClick(msg) {
		return m, nil
	}
	// Clicking a list row selects it; double activation is keyboard Enter.
	x, y, ok := mouseBodyLocal(msg, m.width, m.height)
	if !ok {
		return m, nil
	}
	inner := m.innerWidth()
	bodyH := m.bodyHeight()
	row := y - overlayListTop(m.overlay, bodyH)
	if row < 0 {
		return m, nil
	}
	switch m.overlay {
	case overlayForward:
		offset := m.overlayOffset
		if index := offset + row; index >= 0 && index < len(m.forwardTargets) {
			m.overlayIndex = index
		}
	case overlaySearch:
		if index := row; index >= 0 && index < len(m.searchResults) {
			m.overlayIndex = index
		}
	case overlayGroupInfo:
		if index := row; index >= 0 && index < len(m.groupMembers) {
			m.overlayIndex = index
		}
	}
	_ = x
	_ = inner
	return m, nil
}

// overlayListTop returns the body row where clickable list rows start.
func overlayListTop(overlay chatOverlay, bodyH int) int {
	switch overlay {
	case overlayForward:
		return 2
	case overlaySearch:
		return 3
	case overlayGroupInfo:
		return 12
	}
	return bodyH
}

// isRightClick reports whether the event is a right-button press.
func isRightClick(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight
}

// ---------------------------------------------------------------------------
// overlay commands from the chat window

// openForwardOverlay lists conversations to forward the selected message to.
func (m mainWindowModel) openForwardOverlay() (mainWindowModel, tea.Cmd) {
	if m.selectedMessage() == nil {
		m.errText = "请先选择一条消息"
		return m, nil
	}
	m.forwardTargets = append([]api.Conversation(nil), m.conversations...)
	if len(m.forwardTargets) == 0 {
		m.errText = "没有可转发的会话"
		return m, nil
	}
	m.openOverlay(overlayForward)
	m.overlayIndex = 0
	return m, nil
}

// openSearchOverlay opens the in-conversation message search.
func (m mainWindowModel) openSearchOverlay() (mainWindowModel, tea.Cmd) {
	if m.selectedConv() == nil {
		return m, nil
	}
	m.searchResults = nil
	m.openOverlay(overlaySearch)
	return m, m.overlayInput.Focus()
}

// openInfoOverlay opens the group info / user profile page.
func (m mainWindowModel) openInfoOverlay() (mainWindowModel, tea.Cmd) {
	conv := m.selectedConv()
	if conv == nil {
		return m, nil
	}
	m.openOverlay(overlayNone)
	if conv.ChatType == api.ChatTypeGroup {
		m.overlay = overlayGroupInfo
		m.overlayLoading = true
		m.groupInfo = nil
		m.groupMembers = nil
		return m, loadGroupCommand(m.client, m.token, conv.ChatID)
	}
	m.overlay = overlayUserProfile
	m.overlayLoading = true
	m.userProfile = nil
	return m, loadUserProfileCommand(m.client, m.token, conv.ChatID)
}

// openImageOverlay opens the local image sender.
func (m mainWindowModel) openImageOverlay() (mainWindowModel, tea.Cmd) {
	if m.selectedConv() == nil {
		return m, nil
	}
	m.openOverlay(overlayImageSend)
	return m, m.overlayInput.Focus()
}

// ---------------------------------------------------------------------------
// overlay rendering

// overlayView renders the overlay in place of the normal body.
func (m mainWindowModel) overlayView() string {
	width, height := screenSize(m.width, m.height)
	bodyH := maxInt(8, height-4)
	inner := width - 2*chromePadX
	body := m.overlayBody(bodyH, inner)
	return screenChromeTop(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		subtitleStyle.Render(m.overlayTitle()),
		body,
		m.overlayHints(),
	)
}

// overlayTitle returns the header title of the active overlay.
func (m mainWindowModel) overlayTitle() string {
	switch m.overlay {
	case overlayForward:
		return "转发消息"
	case overlaySearch:
		return "消息搜索"
	case overlayGroupInfo:
		return "群资料"
	case overlayUserProfile:
		return "用户主页"
	case overlayImageSend:
		return "发送图片"
	}
	return ""
}

// overlayHints returns the footer hint bar of the active overlay.
func (m mainWindowModel) overlayHints() string {
	switch m.overlay {
	case overlayForward:
		return "↑↓ 选择目标会话 · Enter 转发 · Esc 取消 · 鼠标右键关闭"
	case overlaySearch:
		return "输入关键词 · Enter 搜索 · ↑↓ 选择结果 · Esc 关闭"
	case overlayGroupInfo:
		return "↑↓ 选择成员 · PgUp/PgDn 翻页 · r 刷新 · Esc 返回 · 鼠标右键关闭"
	case overlayUserProfile:
		return "Esc 返回 · 鼠标右键关闭"
	case overlayImageSend:
		return "输入本地图片路径或图片 URL · Enter 上传并发送 · Esc 取消"
	}
	return "Esc 返回"
}

// overlayBody builds the body rows of the active overlay.
func (m mainWindowModel) overlayBody(bodyH, width int) []string {
	var lines []string
	add := func(parts ...string) {
		for _, text := range parts {
			for _, part := range strings.Split(text, "\n") {
				lines = append(lines, padLine(part, width))
			}
		}
	}
	status := ""
	switch {
	case m.overlayLoading:
		status = lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", hintStyle.Render("加载中…"))
	case m.overlayErr != "":
		status = errStyle.Render(truncate(m.overlayErr, width))
	}

	switch m.overlay {
	case overlayForward:
		add(chatTitleStyle.Render(" 选择会话"))
		add(hintStyle.Render(fmt.Sprintf(" 转发消息：%s", truncate(m.selectedMessagePreview(), width-12))))
		if len(m.forwardTargets) == 0 {
			add(hintStyle.Render(" 没有可转发的会话"))
		}
		visible := maxInt(1, bodyH-5)
		offset := clamp(m.overlayIndex-visible+1, 0, maxInt(0, len(m.forwardTargets)-visible))
		for i := offset; i < len(m.forwardTargets) && len(lines) < bodyH-3; i++ {
			conv := m.forwardTargets[i]
			marker := "  "
			style := valueStyle
			if i == m.overlayIndex {
				marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌ ")
				style = convSelectedStyle
			}
			add(marker + style.Render(padLine(conv.DisplayName()+"  "+conv.ChatType.Label(), maxInt(8, width-4))))
		}
		add(status)

	case overlaySearch:
		add(chatTitleStyle.Render(" 在当前会话中搜索"))
		input := m.overlayInput
		input.Width = maxInt(8, width-5)
		style := inputStyle
		if m.overlayInput.Focused() {
			style = inputFocusedStyle
		}
		add(strings.Split(style.Width(width).Render(input.View()), "\n")...)
		if len(m.searchResults) == 0 {
			add(hintStyle.Render(" 输入关键词后按 Enter 搜索"))
		}
		for i, result := range m.searchResults {
			if len(lines) >= bodyH-2 {
				break
			}
			marker := "  "
			style := valueStyle
			if i == m.overlayIndex {
				marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌ ")
				style = convSelectedStyle
			}
			meta := hintStyle.Render(fmt.Sprintf("%s · %s", truncate(result.SenderName, 12), result.TimeText))
			add(marker + style.Render(truncate(result.Content, maxInt(8, width-6))))
			add("    " + meta)
		}
		add(status)

	case overlayGroupInfo:
		if m.groupInfo == nil {
			add(chatTitleStyle.Render(" 群资料"))
			add(hintStyle.Render(" 正在加载群资料…"))
		} else {
			info := m.groupInfo
			add(chatTitleStyle.Render(" " + truncate(info.Name, width-4)))
			add(hintStyle.Render(" 群号 " + info.GroupID + " · " + info.CategoryName))
			add(hintStyle.Render(fmt.Sprintf(" 群主 %s · 人数 %d · 我 %s", truncate(info.Owner, 16), info.Headcount, permissionLabel(info.PermissionLevel))))
			if info.Introduction != "" {
				add(hintStyle.Render(" 简介: " + truncate(info.Introduction, width-8)))
			}
			if info.CommunityName != "" {
				add(hintStyle.Render(" 社区: " + info.CommunityName))
			}
			if info.IsGag {
				add(errStyle.Render(" 当前已被禁言"))
			}
			add("")
			add(hintStyle.Render(fmt.Sprintf(" 成员（%d）  第 %d 页", m.groupTotal, m.groupPage)))
			visible := maxInt(1, bodyH-12)
			offset := clamp(m.overlayIndex-visible+1, 0, maxInt(0, len(m.groupMembers)-visible))
			for i := offset; i < len(m.groupMembers) && len(lines) < bodyH-2; i++ {
				member := m.groupMembers[i]
				marker := "  "
				style := valueStyle
				if i == m.overlayIndex {
					marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌ ")
					style = convSelectedStyle
				}
				label := member.Name
				if label == "" {
					label = member.UserID
				}
				label += "  " + permissionLabel(member.PermissionLevel)
				if member.IsGag {
					label += "（禁言）"
				}
				add(marker + style.Render(padLine(label, width-4)))
			}
		}
		add(status)

	case overlayUserProfile:
		if m.userProfile == nil {
			add(chatTitleStyle.Render(" 用户主页"))
			add(hintStyle.Render(" 正在加载用户资料…"))
		} else {
			profile := m.userProfile
			add(chatTitleStyle.Render(" " + truncate(profile.Name, width-4)))
			rows := [][2]string{
				{"用户 ID", profile.ID},
				{"注册时间", profile.RegisterTime},
				{"在线天数", fmt.Sprintf("%d 天（连续 %d 天）", profile.OnlineDay, profile.ContinuousOnlineDay)},
				{"IP 归属", profile.IPGeo},
				{"VIP", boolLabel(profile.IsVIP)},
			}
			if profile.IsBlocked {
				rows = append(rows, [2]string{"关系", "已拉黑"})
			}
			if profile.BanID != 0 {
				rows = append(rows, [2]string{"封禁", profile.BanReason})
			}
			if len(profile.MedalNames) > 0 {
				rows = append(rows, [2]string{"勋章", strings.Join(profile.MedalNames, "、")})
			}
			for _, row := range rows {
				add(m.detailRowForOverlay(row[0], row[1], width))
			}
		}
		add(status)

	case overlayImageSend:
		add(chatTitleStyle.Render(" 发送图片"))
		add(hintStyle.Render(" 支持本地路径或 http(s) 图片 URL"))
		input := m.overlayInput
		input.Width = maxInt(8, width-5)
		style := inputFocusedStyle
		add(strings.Split(style.Width(width).Render(input.View()), "\n")...)
		add(hintStyle.Render(" Enter 上传并发送 · Esc 取消"))
		add(status)
	}

	// Pad/trim to bodyH.
	out := lines
	for len(out) < bodyH {
		out = append(out, padLine("", width))
	}
	if len(out) > bodyH {
		out = out[:bodyH]
	}
	return out
}

// detailRowForOverlay renders a label/value row for overlay pages.
func (m mainWindowModel) detailRowForOverlay(label, value string, width int) string {
	const labelW = 12
	return "  " + labelStyle.Render(padLine(label, labelW)) +
		valueStyle.Render(truncate(value, maxInt(4, width-labelW-4)))
}

// selectedMessagePreview returns a short preview of the selected message.
func (m mainWindowModel) selectedMessagePreview() string {
	message := m.selectedMessage()
	if message == nil {
		return ""
	}
	return message.Preview()
}

// permissionLabel converts a permission level into a display label.
func permissionLabel(level int32) string {
	switch level {
	case 100:
		return "群主"
	case 2:
		return "管理员"
	default:
		return "成员"
	}
}

// boolLabel renders a boolean as 是/否.
func boolLabel(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

// selectedMessage returns the message currently highlighted in the history.
func (m mainWindowModel) selectedMessage() *api.Message {
	conv := m.selectedConv()
	if conv == nil {
		return nil
	}
	history := m.messages[conv.ChatID]
	if len(history) == 0 {
		return nil
	}
	index := m.selectedMsg
	if index < 0 || index >= len(history) {
		index = len(history) - 1
	}
	return &history[index]
}

// appendLocalSentMessage appends a locally confirmed media message.
func (m *mainWindowModel) appendLocalSentMessage(msgID, preview string, content api.MessageContent, contentType int32) tea.Cmd {
	conv := m.selectedConv()
	if conv == nil {
		return nil
	}
	message := api.Message{
		MsgID:       msgID,
		Direction:   "right",
		ContentType: contentType,
		Content:     content,
		TimestampMs: uint64(timeNow().UnixMilli()),
	}
	if m.user != nil && m.user.Data != nil {
		message.Sender.ID = m.user.Data.ID
		message.Sender.Name = m.user.Data.Name
	}
	m.messages[conv.ChatID] = append(m.messages[conv.ChatID], message)
	m.trimHistory(conv.ChatID)
	m.invalidateMessageLines()
	m.msgFollow = true
	if index := m.conversationIndex(conv.ChatID); index >= 0 {
		updated := m.conversations[index]
		updated.ChatContent = preview
		updated.SendTimestamp = message.TimestampMs / 1000
		m.conversations = append(m.conversations[:index], m.conversations[index+1:]...)
		m.conversations = append([]api.Conversation{updated}, m.conversations...)
		m.applyFilter()
		m.selected = 0
	}
	return nil
}

// mediaCachedMsg reports a media download for the local cache.
type mediaCachedMsg struct {
	path string
	err  error
}

// cacheMediaCommand downloads an image/video into ~/.yhchat-tui/cache.
func cacheMediaCommand(msg api.Message) tea.Cmd {
	url := ""
	video := false
	switch msg.ContentType {
	case 2:
		url = msg.Content.ImageURL
		if url == "" {
			url = msg.Content.Image
		}
	case 11:
		url = msg.Content.AudioURL
	case 12:
		url = msg.Content.VideoURL
		video = true
	default:
		if msg.Content.ImageURL != "" {
			url = msg.Content.ImageURL
		}
	}
	if strings.TrimSpace(url) == "" {
		return func() tea.Msg { return mediaCachedMsg{err: fmt.Errorf("该消息没有可缓存的媒体")} }
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://chat-img.jwznb.com/" + strings.TrimLeft(url, "/")
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*requestTimeout)
		defer cancel()
		path, err := cache.DownloadToCache(ctx, nil, url, video)
		return mediaCachedMsg{path: path, err: err}
	}
}
