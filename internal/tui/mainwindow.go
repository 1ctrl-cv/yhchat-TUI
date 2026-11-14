package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
)

// timeNow and timeLocation are variables so tests can freeze time and use a
// deterministic timezone for snapshot tests.
var (
	timeNow      = time.Now
	timeLocation = time.Local
)

// chatFocus identifies the focused pane of the main window.
type chatFocus int

const (
	focusSidebar chatFocus = iota
	focusSearch
	focusConversations
	focusMessages
	focusInput
)

// Sidebar entries of the main window (the chat page is current).
const sidebarChat = 0

// Message send state used to restore the input on failure.
const (
	sidebarWidth = 4
	panelGap     = 3 // " │ "
)

// mainWindowModel is the post-login chat window: sidebar + conversation list +
// chat panel with message history and input box.
type mainWindowModel struct {
	client *api.Client
	token  string
	user   *api.UserInfo

	width, height int

	conversations []api.Conversation
	filtered      []api.Conversation
	selected      int
	convOffset    int
	sidebarCursor section

	search textinput.Model
	input  textinput.Model

	focus chatFocus

	messages       map[string][]api.Message
	msgOffset      int
	msgFollow      bool
	msgHasMore     map[string]bool
	loadingOlder   bool
	pendingText    string
	lastLoadedChat string

	ws              *api.WSClient
	wsConnected     bool
	wsEverConnected bool

	loadingConversations bool
	loadingMessages      bool
	sending              bool
	errText              string
	okText               string
	spinner              spinner.Model
}

// newMainWindowModel creates the chat window for a logged in account.
func newMainWindowModel(client *api.Client, token string, user *api.UserInfo) mainWindowModel {
	search := newTextInput("search chats", false, 64)
	message := newTextInput("type a message, Enter to send", false, 2000)
	m := mainWindowModel{
		client:     client,
		token:      token,
		user:       user,
		search:     search,
		input:      message,
		focus:      focusConversations,
		messages:   map[string][]api.Message{},
		msgHasMore: map[string]bool{},
		spinner:    newSpinner(),
		msgFollow:  true,
	}
	return m
}

// withUser updates the account profile (used when login could not fetch it).
func (m mainWindowModel) withUser(user *api.UserInfo) mainWindowModel {
	if user != nil {
		m.user = user
	}
	return m
}

// resize records the terminal size.
func (m mainWindowModel) resize(width, height int) mainWindowModel {
	m.width, m.height = width, height
	return m
}

// init starts loading the conversation list.
func (m mainWindowModel) init() (mainWindowModel, tea.Cmd) {
	m.loadingConversations = true
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick, loadConversationsCmd(m.client, m.token))
}

// ---------------------------------------------------------------------------
// commands

// loadConversationsCmd fetches the conversation list.
func loadConversationsCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		conversations, err := client.Conversations(ctx, token)
		return conversationsLoadedMsg{conversations: conversations, err: err}
	}
}

// loadMessagesCmd fetches the message history of one conversation.
func loadMessagesCmd(client *api.Client, token string, conv api.Conversation) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		messages, err := client.Messages(ctx, token, conv.ChatID, conv.ChatType, 30, "")
		return messagesLoadedMsg{chatID: conv.ChatID, chatType: conv.ChatType, messages: messages, err: err}
	}
}

// chatHistoryPageSize is the number of messages fetched per history page.
const chatHistoryPageSize = 30

// loadOlderMessagesCmd fetches history before the given message id.
func loadOlderMessagesCmd(client *api.Client, token string, conv api.Conversation, beforeMsgID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		messages, err := client.Messages(ctx, token, conv.ChatID, conv.ChatType, chatHistoryPageSize, beforeMsgID)
		return olderMessagesLoadedMsg{chatID: conv.ChatID, messages: messages, err: err}
	}
}

// recallMessagesCmd recalls messages sent by the current account.
func recallMessagesCmd(client *api.Client, token, chatID string, chatType api.ChatType, msgIDs []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return messageRecalledMsg{chatID: chatID, msgIDs: msgIDs, err: client.RecallMessages(ctx, token, chatID, chatType, msgIDs...)}
	}
}

// dismissNotificationCmd marks a conversation as read on the server.
func dismissNotificationCmd(client *api.Client, token, chatID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return notificationsDismissedMsg{chatID: chatID, err: client.DismissNotification(ctx, token, chatID)}
	}
}

// waitWSEventCmd waits for the next realtime event. A closed channel yields a
// wsEventMsg with closed=true so the App stops waiting.
func waitWSEventCmd(ws *api.WSClient) tea.Cmd {
	if ws == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-ws.Events()
		if !ok {
			return wsEventMsg{closed: true}
		}
		return wsEventMsg{event: event}
	}
}

// sendTextCmd sends a text message.
func sendTextCmd(client *api.Client, token string, conv api.Conversation, text string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		msgID, err := client.SendTextMessage(ctx, token, conv.ChatID, conv.ChatType, text)
		return messageSentMsg{chatID: conv.ChatID, msgID: msgID, text: text, err: err}
	}
}

// fetchChatUserCmd loads the account profile for the chat window.
func fetchChatUserCmd(client *api.Client, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		user, err := client.UserInfo(ctx, token)
		return chatUserMsg{user: user, err: err}
	}
}

// ---------------------------------------------------------------------------
// update

// Update handles messages for the main window.
func (m mainWindowModel) Update(msg tea.Msg) (mainWindowModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case conversationsLoadedMsg:
		m.loadingConversations = false
		if msg.err != nil {
			return m, m.handleErr(msg.err, "加载会话列表失败")
		}
		m.conversations = msg.conversations
		m.applyFilter()
		m.errText = ""
		if cmd := m.ensureMessagesCmd(); cmd != nil {
			return m, cmd
		}
		return m, nil

	case messagesLoadedMsg:
		m.loadingMessages = false
		if msg.err != nil {
			return m, m.handleErr(msg.err, "加载消息失败")
		}
		m.messages[msg.chatID] = msg.messages
		m.msgHasMore[msg.chatID] = len(msg.messages) >= chatHistoryPageSize
		m.lastLoadedChat = msg.chatID
		m.errText = ""
		m.msgFollow = true
		m.msgOffset = 0
		// Opening a conversation marks it as read.
		return m, m.markConversationRead(msg.chatID)

	case olderMessagesLoadedMsg:
		m.loadingOlder = false
		if msg.err != nil {
			return m, m.handleErr(msg.err, "加载更早消息失败")
		}
		existing := m.messages[msg.chatID]
		seen := make(map[string]bool, len(existing))
		for _, message := range existing {
			seen[message.MsgID] = true
		}
		older := make([]api.Message, 0, len(msg.messages))
		for _, message := range msg.messages {
			if message.MsgID != "" && seen[message.MsgID] {
				continue
			}
			older = append(older, message)
		}
		m.msgHasMore[msg.chatID] = len(msg.messages) >= chatHistoryPageSize
		if len(older) == 0 {
			m.msgHasMore[msg.chatID] = false
			return m, nil
		}
		oldLines := len(m.messageLines())
		m.messages[msg.chatID] = append(older, existing...)
		newLines := len(m.messageLines())
		m.msgFollow = false
		if delta := newLines - oldLines; delta > 0 {
			m.msgOffset += delta
		}
		m.errText = ""
		return m, nil

	case messageRecalledMsg:
		if msg.err != nil {
			m.errText = api.FriendlyMessage(msg.err)
			return m, nil
		}
		ids := make(map[string]bool, len(msg.msgIDs))
		for _, id := range msg.msgIDs {
			ids[id] = true
		}
		history := m.messages[msg.chatID]
		for i := range history {
			if ids[history[i].MsgID] {
				history[i].Recalled = true
				history[i].Content = api.MessageContent{}
			}
		}
		m.okText = "消息已撤回"
		m.errText = ""
		return m, nil

	case notificationsDismissedMsg:
		if msg.err != nil && !api.IsSessionExpired(msg.err) {
			// Not fatal: the badge is already cleared locally.
			m.errText = api.FriendlyMessage(msg.err)
		}
		return m, nil

	case messageSentMsg:
		m.sending = false
		if msg.err != nil {
			m.errText = api.FriendlyMessage(msg.err)
			if m.pendingText != "" {
				m.input.SetValue(m.pendingText)
			}
			m.pendingText = ""
			return m, nil
		}
		m.appendOwnMessage(msg.chatID, msg.msgID, msg.text)
		m.okText = "消息已发送"
		m.errText = ""
		m.pendingText = ""
		return m, nil

	case chatUserMsg:
		if msg.user != nil {
			m.user = msg.user
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		// Cursor blink / paste for the focused text input.
		var cmd tea.Cmd
		switch m.focus {
		case focusSearch:
			m.search, cmd = m.search.Update(msg)
			return m, cmd
		case focusInput:
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

// handleErr shows an error; session expiry is forwarded to the App.
func (m mainWindowModel) handleErr(err error, prefix string) tea.Cmd {
	if api.IsSessionExpired(err) {
		return func() tea.Msg { return expiringTokenMsg{err: err} }
	}
	m.errText = prefix + ": " + api.FriendlyMessage(err)
	return nil
}

// handleKey processes key presses.
func (m mainWindowModel) handleKey(msg tea.KeyMsg) (mainWindowModel, tea.Cmd) {
	key := msg.String()

	// Global navigation.
	switch key {
	case "tab":
		m.setFocus(m.nextFocus(1))
		return m, m.applyFocus()
	case "shift+tab":
		m.setFocus(m.nextFocus(-1))
		return m, m.applyFocus()
	}

	switch m.focus {
	case focusSidebar:
		return m.handleSidebarKey(key, msg)
	case focusSearch:
		return m.handleSearchKey(key, msg)
	case focusConversations:
		return m.handleConversationsKey(key)
	case focusMessages:
		return m.handleMessagesKey(key)
	default:
		return m.handleInputKey(key, msg)
	}
}

// handleSidebarKey handles navigation inside the sidebar.
func (m mainWindowModel) handleSidebarKey(key string, msg tea.KeyMsg) (mainWindowModel, tea.Cmd) {
	switch key {
	case "up":
		m.sidebarCursor = nextSectionCursor(m.sidebarCursor, -1)
		return m, nil
	case "down":
		m.sidebarCursor = nextSectionCursor(m.sidebarCursor, 1)
		return m, nil
	case "enter":
		if m.sidebarCursor == sectionChat {
			m.setFocus(focusConversations)
			return m, m.applyFocus()
		}
		target := m.sidebarCursor
		return m, func() tea.Msg { return switchSectionMsg{to: target} }
	case "r":
		return m.refreshConversations()
	case "right":
		m.setFocus(focusConversations)
		return m, m.applyFocus()
	}
	return m, nil
}

// handleSearchKey handles typing in the search box.
func (m mainWindowModel) handleSearchKey(key string, msg tea.KeyMsg) (mainWindowModel, tea.Cmd) {
	switch key {
	case "esc":
		m.search.SetValue("")
		m.applyFilter()
		m.setFocus(focusConversations)
		return m, m.applyFocus()
	case "enter", "down":
		m.setFocus(focusConversations)
		return m, m.applyFocus()
	case "up":
		m.setFocus(focusSidebar)
		return m, m.applyFocus()
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.applyFilter()
	return m, cmd
}

// handleConversationsKey handles navigation in the conversation list.
func (m mainWindowModel) handleConversationsKey(key string) (mainWindowModel, tea.Cmd) {
	switch key {
	case "up":
		m.moveSelection(-1)
		return m, m.ensureMessagesCmd()
	case "down":
		m.moveSelection(1)
		return m, m.ensureMessagesCmd()
	case "pgup":
		m.moveSelection(-m.conversationPageSize())
		return m, m.ensureMessagesCmd()
	case "pgdown":
		m.moveSelection(m.conversationPageSize())
		return m, m.ensureMessagesCmd()
	case "home", "g":
		m.selected = 0
		m.ensureVisibleSelection()
		return m, m.ensureMessagesCmd()
	case "end", "G":
		if len(m.filtered) > 0 {
			m.selected = len(m.filtered) - 1
		}
		m.ensureVisibleSelection()
		return m, m.ensureMessagesCmd()
	case "enter":
		if conv := m.selectedConv(); conv != nil {
			m.setFocus(focusInput)
			return m, m.applyFocus()
		}
		return m, nil
	case "/":
		m.setFocus(focusSearch)
		return m, m.applyFocus()
	case "r":
		return m.refreshConversations()
	case "esc", "left":
		m.setFocus(focusSidebar)
		return m, m.applyFocus()
	}
	return m, nil
}

// handleMessagesKey handles scrolling in the message history.
func (m mainWindowModel) handleMessagesKey(key string) (mainWindowModel, tea.Cmd) {
	page := m.messagePageSize()
	switch key {
	case "up":
		if m.atMessageTop() {
			return m.loadOlderMessages()
		}
		m.scrollMessages(-1)
	case "down":
		m.scrollMessages(1)
	case "pgup":
		if m.atMessageTop() {
			return m.loadOlderMessages()
		}
		m.scrollMessages(-page)
	case "pgdown":
		m.scrollMessages(page)
	case "end", "G":
		m.msgFollow = true
	case "home", "g":
		m.msgFollow = false
		m.msgOffset = 0
	case "enter":
		m.setFocus(focusInput)
		return m, m.applyFocus()
	case "x":
		return m.recallLastOwnMessage()
	case "esc", "left":
		m.setFocus(focusConversations)
		return m, m.applyFocus()
	case "r":
		if conv := m.selectedConv(); conv != nil {
			m.loadingMessages = true
			return m, tea.Batch(m.spinner.Tick, loadMessagesCmd(m.client, m.token, *conv))
		}
	}
	return m, nil
}

// recallLastOwnMessage recalls the newest own, non-recalled message.
func (m mainWindowModel) recallLastOwnMessage() (mainWindowModel, tea.Cmd) {
	conv := m.selectedConv()
	if conv == nil {
		return m, nil
	}
	history := m.messages[conv.ChatID]
	for i := len(history) - 1; i >= 0; i-- {
		message := history[i]
		if message.Recalled || !message.IsOwn() {
			continue
		}
		return m, recallMessagesCmd(m.client, m.token, conv.ChatID, conv.ChatType, []string{message.MsgID})
	}
	m.errText = "没有可撤回的自己的消息"
	return m, nil
}

// handleInputKey handles the message input box.
func (m mainWindowModel) handleInputKey(key string, msg tea.KeyMsg) (mainWindowModel, tea.Cmd) {
	switch key {
	case "enter":
		return m.sendCurrent()
	case "esc":
		m.setFocus(focusMessages)
		return m, m.applyFocus()
	case "up":
		m.setFocus(focusMessages)
		m.scrollMessages(-1)
		return m, m.applyFocus()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ---------------------------------------------------------------------------
// focus & selection helpers

// setFocus sets the focused pane and blurs the text inputs.
func (m *mainWindowModel) setFocus(focus chatFocus) {
	m.focus = focus
	m.search.Blur()
	m.input.Blur()
}

// applyFocus focuses the active text input and returns its blink command.
func (m *mainWindowModel) applyFocus() tea.Cmd {
	switch m.focus {
	case focusSearch:
		return m.search.Focus()
	case focusInput:
		return m.input.Focus()
	}
	return nil
}

// nextFocus cycles focus panes.
func (m mainWindowModel) nextFocus(delta int) chatFocus {
	order := []chatFocus{focusSidebar, focusSearch, focusConversations, focusMessages, focusInput}
	index := 0
	for i, focus := range order {
		if focus == m.focus {
			index = i
			break
		}
	}
	n := len(order)
	index = ((index+delta)%n + n) % n
	return order[index]
}

// selectedConv returns the selected conversation (or nil).
func (m mainWindowModel) selectedConv() *api.Conversation {
	if len(m.filtered) == 0 {
		return nil
	}
	index := clamp(m.selected, 0, len(m.filtered)-1)
	return &m.filtered[index]
}

// applyFilter recomputes the filtered list from the search box.
func (m *mainWindowModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	if query == "" {
		m.filtered = append([]api.Conversation(nil), m.conversations...)
	} else {
		filtered := make([]api.Conversation, 0, len(m.conversations))
		for _, conv := range m.conversations {
			if strings.Contains(strings.ToLower(conv.DisplayName()), query) ||
				strings.Contains(strings.ToLower(conv.Preview()), query) {
				filtered = append(filtered, conv)
			}
		}
		m.filtered = filtered
	}
	if m.selected >= len(m.filtered) {
		m.selected = maxInt(0, len(m.filtered)-1)
	}
	m.ensureVisibleSelection()
}

// moveSelection moves the conversation selection and keeps it visible.
func (m *mainWindowModel) moveSelection(delta int) {
	if len(m.filtered) == 0 {
		m.selected = 0
		return
	}
	m.selected = clamp(m.selected+delta, 0, len(m.filtered)-1)
	m.ensureVisibleSelection()
}

// ensureVisibleSelection adjusts the list scroll offset so the selection shows.
func (m *mainWindowModel) ensureVisibleSelection() {
	visible := m.conversationPageSize()
	if visible <= 0 {
		return
	}
	if m.selected < m.convOffset {
		m.convOffset = m.selected
	}
	if m.selected >= m.convOffset+visible {
		m.convOffset = m.selected - visible + 1
	}
	if m.convOffset < 0 {
		m.convOffset = 0
	}
	if m.convOffset > m.selected {
		m.convOffset = m.selected
	}
}

// ensureMessagesCmd loads messages for the selected conversation when they were
// never fetched during this session.
func (m *mainWindowModel) ensureMessagesCmd() tea.Cmd {
	conv := m.selectedConv()
	if conv == nil {
		return nil
	}
	if _, ok := m.messages[conv.ChatID]; ok {
		return m.markConversationRead(conv.ChatID)
	}
	m.loadingMessages = true
	return tea.Batch(m.spinner.Tick, loadMessagesCmd(m.client, m.token, *conv))
}

// refreshConversations reloads the conversation list.
func (m mainWindowModel) refreshConversations() (mainWindowModel, tea.Cmd) {
	m.loadingConversations = true
	m.errText = ""
	m.okText = ""
	m.messages = map[string][]api.Message{}
	return m, tea.Batch(m.spinner.Tick, loadConversationsCmd(m.client, m.token))
}

// sendCurrent sends the text in the input box.
func (m mainWindowModel) sendCurrent() (mainWindowModel, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" || m.sending {
		return m, nil
	}
	conv := m.selectedConv()
	if conv == nil {
		m.errText = "请先选择一个会话"
		return m, nil
	}
	m.sending = true
	m.pendingText = text
	m.input.SetValue("")
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick, sendTextCmd(m.client, m.token, *conv, text))
}

// appendOwnMessage appends a locally sent message and updates the conversation
// preview (the WebSocket echo will replace it in a later milestone).
func (m *mainWindowModel) appendOwnMessage(chatID, msgID, text string) {
	nowMs := uint64(timeNow().UnixMilli())
	own := api.Message{
		MsgID:       msgID,
		Direction:   "right",
		ContentType: api.ContentTypeText,
		Content:     api.MessageContent{Text: text},
		TimestampMs: nowMs,
	}
	if m.user != nil && m.user.Data != nil {
		own.Sender.ID = m.user.Data.ID
		own.Sender.Name = m.user.Data.Name
	}
	m.messages[chatID] = append(m.messages[chatID], own)
	m.msgFollow = true

	for i := range m.conversations {
		if m.conversations[i].ChatID != chatID {
			continue
		}
		conv := m.conversations[i]
		conv.ChatContent = text
		conv.SendTimestamp = nowMs / 1000
		conv.Unread = 0
		conv.At = false
		m.conversations = append(m.conversations[:i], m.conversations[i+1:]...)
		m.conversations = append([]api.Conversation{conv}, m.conversations...)
		break
	}
	m.applyFilter()
	for i, conv := range m.filtered {
		if conv.ChatID == chatID {
			m.selected = i
			break
		}
	}
	m.ensureVisibleSelection()
}

// openConversation selects a conversation by chat id (creating a local entry
// when it does not exist yet), loads its history and focuses the input box.
func (m mainWindowModel) openConversation(chatID string, chatType api.ChatType, name, avatarURL string) (mainWindowModel, tea.Cmd) {
	if chatID == "" {
		return m, nil
	}
	m.search.SetValue("")
	m.applyFilter()
	index := -1
	for i := range m.filtered {
		if m.filtered[i].ChatID == chatID {
			index = i
			break
		}
	}
	if index < 0 {
		now := timeNow()
		conv := api.Conversation{
			ChatID:        chatID,
			ChatType:      chatType,
			Name:          name,
			AvatarURL:     avatarURL,
			TimestampMs:   uint64(now.UnixMilli()),
			SendTimestamp: uint64(now.Unix()),
		}
		m.conversations = append([]api.Conversation{conv}, m.conversations...)
		m.applyFilter()
		for i := range m.filtered {
			if m.filtered[i].ChatID == chatID {
				index = i
				break
			}
		}
	}
	if index >= 0 {
		m.selected = index
		m.ensureVisibleSelection()
	}
	m.setFocus(focusInput)
	m.errText = ""
	var cmds []tea.Cmd
	if cmd := m.ensureMessagesCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.applyFocus())
	return m, tea.Batch(cmds...)
}

// markConversationRead clears the local unread badge and asks the server to
// dismiss the notification for that conversation.
func (m *mainWindowModel) markConversationRead(chatID string) tea.Cmd {
	for i := range m.conversations {
		if m.conversations[i].ChatID != chatID {
			continue
		}
		if m.conversations[i].Unread == 0 && !m.conversations[i].At {
			return nil
		}
		m.conversations[i].Unread = 0
		m.conversations[i].At = false
		m.applyFilter()
		return dismissNotificationCmd(m.client, m.token, chatID)
	}
	return nil
}

// loadOlderMessages fetches the previous page of history for the active chat.
func (m mainWindowModel) loadOlderMessages() (mainWindowModel, tea.Cmd) {
	conv := m.selectedConv()
	if conv == nil || m.loadingOlder {
		return m, nil
	}
	if has, ok := m.msgHasMore[conv.ChatID]; ok && !has {
		return m, nil
	}
	messages := m.messages[conv.ChatID]
	if len(messages) == 0 || messages[0].MsgID == "" {
		return m, nil
	}
	m.loadingOlder = true
	return m, tea.Batch(m.spinner.Tick, loadOlderMessagesCmd(m.client, m.token, *conv, messages[0].MsgID))
}

// atMessageTop reports whether the message viewport is scrolled to the top.
func (m mainWindowModel) atMessageTop() bool {
	height := m.messageViewportHeight()
	maxOffset := maxInt(0, len(m.messageLines())-height)
	offset := m.msgOffset
	if m.msgFollow {
		offset = maxOffset
	}
	return offset <= 0
}

// ---------------------------------------------------------------------------
// realtime events

// handleWSEvent applies one realtime WebSocket event to the chat window.
func (m mainWindowModel) handleWSEvent(event api.WSEvent) (mainWindowModel, tea.Cmd) {
	switch event.Kind {
	case api.WSEventConnected:
		m.wsConnected = true
		m.okText = "实时消息已连接"
		m.errText = ""
		if !m.wsEverConnected {
			m.wsEverConnected = true
			return m, nil
		}
		// Reconnected after a drop: resync the list and the open history.
		var resync []tea.Cmd
		if len(m.conversations) > 0 {
			m.loadingConversations = true
			resync = append(resync, m.spinner.Tick, loadConversationsCmd(m.client, m.token))
		}
		if conv := m.selectedConv(); conv != nil {
			m.loadingMessages = true
			resync = append(resync, m.spinner.Tick, loadMessagesCmd(m.client, m.token, *conv))
		}
		return m, tea.Batch(resync...)
	case api.WSEventDisconnected:
		m.wsConnected = false
		if event.Err != nil {
			m.okText = "实时连接已断开，正在重连…"
		} else {
			m.okText = "实时连接已断开"
		}
		return m, nil
	case api.WSEventPush:
		if event.Push != nil {
			return m.applyPush(*event.Push)
		}
	case api.WSEventEdit:
		if event.Push != nil {
			return m.applyEdit(*event.Push)
		}
	case api.WSEventRecall:
		if event.Recall != nil {
			return m.applyRecall(*event.Recall)
		}
	case api.WSEventStream:
		if event.Stream != nil {
			return m.applyStream(event.Stream)
		}
	case api.WSEventInviteApply:
		m.okText = "收到新的好友/群邀请"
	}
	return m, nil
}

// messageFromPush converts a PushMessage into a Message.
func (m mainWindowModel) messageFromPush(p api.PushMessage) api.Message {
	direction := "left"
	if m.user != nil && m.user.Data != nil && p.Sender.ID != "" && p.Sender.ID == m.user.Data.ID {
		direction = "right"
	}
	return api.Message{
		MsgID:       p.MsgID,
		Sender:      p.Sender,
		Direction:   direction,
		ContentType: p.ContentType,
		Content:     p.Content,
		TimestampMs: p.TimestampMs,
		MsgSeq:      p.MsgSeq,
		QuoteMsgID:  p.QuoteMsgID,
		Edited:      p.EditTimestamp > 0,
	}
}

// applyPush appends/replaces a pushed message and updates the conversation list.
func (m *mainWindowModel) applyPush(p api.PushMessage) (mainWindowModel, tea.Cmd) {
	if p.ChatID == "" {
		return *m, nil
	}
	message := m.messageFromPush(p)
	own := message.IsOwn()

	// Update the cached history when it was already loaded.
	if history, loaded := m.messages[p.ChatID]; loaded {
		replaced := false
		for i := range history {
			if history[i].MsgID == message.MsgID {
				history[i] = message
				replaced = true
				break
			}
		}
		if !replaced {
			m.messages[p.ChatID] = append(history, message)
		}
	}

	active := m.selectedConv() != nil && m.selectedConv().ChatID == p.ChatID
	selectedBefore := ""
	if m.selectedConv() != nil {
		selectedBefore = m.selectedConv().ChatID
	}
	index := m.conversationIndex(p.ChatID)
	var cmds []tea.Cmd
	if index < 0 {
		// A message from a conversation we do not know yet: reload the list.
		cmds = append(cmds, loadConversationsCmd(m.client, m.token))
		return *m, tea.Batch(cmds...)
	}
	conv := m.conversations[index]
	previousUnread := conv.Unread
	conv.ChatContent = message.Preview()
	conv.SendTimestamp = p.TimestampMs / 1000
	if own || active {
		conv.Unread = 0
		conv.At = false
	} else {
		conv.Unread++
	}
	// Move the conversation to the top of the list.
	m.conversations = append(m.conversations[:index], m.conversations[index+1:]...)
	m.conversations = append([]api.Conversation{conv}, m.conversations...)
	m.applyFilter()
	if active {
		for i := range m.filtered {
			if m.filtered[i].ChatID == p.ChatID {
				m.selected = i
				break
			}
		}
		if previousUnread > 0 && !own {
			cmds = append(cmds, dismissNotificationCmd(m.client, m.token, p.ChatID))
		}
	} else if selectedBefore != "" {
		// Keep the cursor on the previously selected conversation.
		for i := range m.filtered {
			if m.filtered[i].ChatID == selectedBefore {
				m.selected = i
				break
			}
		}
	}
	m.ensureVisibleSelection()
	return *m, tea.Batch(cmds...)
}

// applyEdit updates an edited message in the cache.
func (m *mainWindowModel) applyEdit(p api.PushMessage) (mainWindowModel, tea.Cmd) {
	for chatID, history := range m.messages {
		for i := range history {
			if history[i].MsgID != p.MsgID {
				continue
			}
			history[i].Content = p.Content
			history[i].ContentType = p.ContentType
			history[i].Edited = true
			if chatID == p.ChatID {
				m.updateConversationPreview(chatID, history[i].Preview())
			}
			return *m, nil
		}
	}
	return *m, nil
}

// applyRecall marks messages as recalled.
func (m *mainWindowModel) applyRecall(recall api.RecallMessage) (mainWindowModel, tea.Cmd) {
	if recall.ChatID == "" {
		return *m, nil
	}
	ids := make(map[string]bool, len(recall.MsgIDs))
	for _, id := range recall.MsgIDs {
		ids[id] = true
	}
	history := m.messages[recall.ChatID]
	anyRecalled := false
	for i := range history {
		match := ids[history[i].MsgID]
		if !match && len(ids) == 0 && recall.MsgSeq > 0 {
			match = history[i].MsgSeq == recall.MsgSeq
		}
		if !match {
			continue
		}
		history[i].Recalled = true
		history[i].Content = api.MessageContent{}
		anyRecalled = true
	}
	if anyRecalled && len(history) > 0 && ids[history[len(history)-1].MsgID] {
		m.updateConversationPreview(recall.ChatID, "撤回了一条消息")
	}
	return *m, nil
}

// applyStream appends streamed text to an existing message.
func (m *mainWindowModel) applyStream(stream *api.StreamMessage) (mainWindowModel, tea.Cmd) {
	history := m.messages[stream.ChatID]
	for i := range history {
		if history[i].MsgID != stream.MsgID {
			continue
		}
		history[i].Content.Text += stream.Content
		if i == len(history)-1 {
			m.updateConversationPreview(stream.ChatID, history[i].Preview())
		}
		return *m, nil
	}
	return *m, nil
}

// updateConversationPreview updates the latest-message preview of a conversation.
func (m *mainWindowModel) updateConversationPreview(chatID, preview string) {
	for i := range m.conversations {
		if m.conversations[i].ChatID == chatID {
			m.conversations[i].ChatContent = preview
			m.applyFilter()
			return
		}
	}
}

// conversationIndex returns the index of a conversation in the full list.
func (m mainWindowModel) conversationIndex(chatID string) int {
	for i := range m.conversations {
		if m.conversations[i].ChatID == chatID {
			return i
		}
	}
	return -1
}

// scrollMessages scrolls the message viewport.
func (m *mainWindowModel) scrollMessages(delta int) {
	lines := m.messageLines()
	height := m.messageViewportHeight()
	maxOffset := maxInt(0, len(lines)-height)
	offset := m.msgOffset
	if m.msgFollow {
		offset = maxOffset
	}
	offset = clamp(offset+delta, 0, maxOffset)
	m.msgOffset = offset
	m.msgFollow = offset >= maxOffset
}

// conversationPageSize returns how many conversations fit on screen.
func (m mainWindowModel) conversationPageSize() int {
	height := m.conversationViewportHeight()
	return maxInt(1, height/2)
}

// messagePageSize returns the message scroll page size.
func (m mainWindowModel) messagePageSize() int {
	return maxInt(1, m.messageViewportHeight()-1)
}

// ---------------------------------------------------------------------------
// layout helpers

// innerWidth is the usable body width (inside the full-screen padding).
func (m mainWindowModel) innerWidth() int {
	width, _ := screenSize(m.width, m.height)
	return width - 2*chromePadX
}

// bodyHeight is the number of body rows used by the chat frame.
func (m mainWindowModel) bodyHeight() int {
	_, height := screenSize(m.width, m.height)
	return maxInt(8, height-4)
}

// conversationViewportHeight returns the number of lines of the list area.
func (m mainWindowModel) conversationViewportHeight() int {
	// search (1) + separator (1) + status (1)
	return maxInt(2, m.bodyHeight()-3)
}

// messageViewportHeight returns the number of visible message lines.
func (m mainWindowModel) messageViewportHeight() int {
	// header (1) + separator (1) + separator (1) + input (1)
	return maxInt(3, m.bodyHeight()-4)
}

// convPanelWidth returns the conversation panel width.
func (m mainWindowModel) convPanelWidth(inner int) int {
	width := clamp(inner/3, 20, 36)
	chatWidth := inner - sidebarWidth - 2*panelGap - width
	if chatWidth < 28 {
		width = maxInt(16, inner-sidebarWidth-2*panelGap-28)
	}
	return width
}

// ---------------------------------------------------------------------------
// view

// View implements tea.Model for the main window.
func (m mainWindowModel) View() string {
	width, height := screenSize(m.width, m.height)
	inner := width - 2*chromePadX
	bodyH := maxInt(8, height-4)

	body := m.bodyLines(inner, bodyH)
	headerRight := okStyle.Render("● 已登录")
	if m.user != nil && m.user.Data != nil && m.user.Data.Name != "" {
		headerRight = okStyle.Render("● " + truncate(m.user.Data.Name, 24))
	}
	return screenChromeTop(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		headerRight,
		body,
		m.viewHints(),
	)
}

// bodyLines composes the three columns of the chat window.
func (m mainWindowModel) bodyLines(inner, bodyH int) []string {
	convW := m.convPanelWidth(inner)
	chatW := inner - sidebarWidth - 2*panelGap - convW
	if chatW < 10 {
		chatW = 10
	}

	sidebar := m.sidebarLines(bodyH)
	conversation := m.conversationLines(bodyH, convW)
	chat := m.chatLines(bodyH, chatW)
	divider := dividerStyle.Render("│")

	lines := make([]string, 0, bodyH)
	for i := 0; i < bodyH; i++ {
		lines = append(lines,
			padLine(sidebar[i], sidebarWidth)+" "+divider+" "+
				padLine(conversation[i], convW)+" "+divider+" "+
				padLine(chat[i], chatW))
	}
	return lines
}

// sidebarLines renders the narrow sidebar column.
func (m mainWindowModel) sidebarLines(bodyH int) []string {
	return renderSidebar(bodyH, sectionChat, m.sidebarCursor, m.focus == focusSidebar)
}

// conversationLines renders the search box, conversation list and status line.
func (m mainWindowModel) conversationLines(bodyH, width int) []string {
	lines := make([]string, 0, bodyH)

	// Search box.
	search := m.search
	search.Width = maxInt(4, width-5)
	searchStyle := inputStyle
	if m.focus == focusSearch {
		searchStyle = inputFocusedStyle
	}
	// The underline style renders two lines (text + rule), which doubles as the
	// separator under the search box.
	lines = append(lines, strings.Split(searchStyle.Width(width).Render(search.View()), "\n")...)

	// List area.
	listH := maxInt(2, bodyH-3)
	rows := make([]string, 0, len(m.filtered)*2)
	for i, conv := range m.filtered {
		rows = append(rows, m.conversationRow(conv, i == m.selected, width)...)
	}
	if len(rows) == 0 {
		empty := "暂无会话，按 r 刷新"
		if m.search.Value() != "" {
			empty = "没有匹配的会话"
		}
		rows = append(rows, hintStyle.Render(truncate(empty, width)))
	}
	if m.convOffset > len(rows)-1 {
		m.convOffset = 0
	}
	offset := clamp(m.convOffset, 0, maxInt(0, len(rows)-1))
	for i := 0; i < listH; i++ {
		index := offset + i
		if index < len(rows) {
			lines = append(lines, padLine(rows[index], width))
		} else {
			lines = append(lines, "")
		}
	}

	// Status line.
	lines = append(lines, m.conversationStatus(width))
	return lines
}

// conversationRow renders one conversation as two lines (name + preview).
func (m mainWindowModel) conversationRow(conv api.Conversation, selected bool, width int) []string {
	timeStr := formatConversationTime(conv)
	marker := " "
	if selected {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌")
	}
	nameWidth := width - 2 - lipgloss.Width(timeStr)
	name := padLine(truncate(conv.DisplayName(), nameWidth), nameWidth)
	var nameRendered string
	if selected {
		nameRendered = convSelectedStyle.Render(name)
	} else {
		nameRendered = convNameStyle.Render(name)
	}
	nameLine := marker + " " + nameRendered + convTimeStyle.Render(timeStr)

	preview := conv.Preview()
	if preview == "" {
		preview = "[消息]"
	}
	badge := ""
	badgeWidth := 0
	if conv.Unread > 0 {
		badge = unreadBadgeStyle.Render(fmt.Sprintf("[%d]", conv.Unread))
		badgeWidth = lipgloss.Width(badge) + 1
	}
	previewWidth := maxInt(4, width-2-badgeWidth)
	previewLine := "  " + convPreviewStyle.Render(truncate(preview, previewWidth))
	if badge != "" {
		gap := width - lipgloss.Width(previewLine) - lipgloss.Width(badge)
		if gap < 1 {
			gap = 1
		}
		previewLine += strings.Repeat(" ", gap) + badge
	}
	return []string{nameLine, previewLine}
}

// conversationStatus renders the bottom status line of the conversation panel.
func (m mainWindowModel) conversationStatus(width int) string {
	switch {
	case m.loadingConversations:
		return truncate(lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ", hintStyle.Render("加载会话…")), width)
	case m.errText != "":
		return errStyle.Render(truncate(m.errText, width))
	case m.okText != "":
		return okStyle.Render(truncate(m.okText, width))
	case len(m.conversations) > 0:
		return hintStyle.Render(truncate(fmt.Sprintf("共 %d 个会话", len(m.conversations)), width))
	}
	return ""
}

// chatLines renders the chat header, message history and input box.
func (m mainWindowModel) chatLines(bodyH, width int) []string {
	conv := m.selectedConv()
	lines := make([]string, 0, bodyH)

	// Header.
	var right []string
	if m.ws != nil {
		if m.wsConnected {
			right = append(right, okStyle.Render("● 实时"))
		} else {
			right = append(right, hintStyle.Render("○ 重连中"))
		}
	}
	if conv == nil {
		lines = append(lines, composeLeftRight(width,
			chatTitleStyle.Render("选择会话开始聊天"),
			strings.Join(right, "  ")))
	} else {
		title := chatTitleStyle.Render(truncate(conv.DisplayName(), maxInt(8, width/2)))
		subtitle := chatSubtitleStyle.Render(conv.ChatType.Label())
		left := title + "  " + subtitle
		switch {
		case m.loadingMessages:
			right = append(right, lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", hintStyle.Render("加载消息…")))
		case m.loadingOlder:
			right = append(right, lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", hintStyle.Render("加载更早消息…")))
		case conv.Unread > 0:
			right = append(right, unreadBadgeStyle.Render(fmt.Sprintf("未读 %d", conv.Unread)))
		}
		lines = append(lines, composeLeftRight(width, left, strings.Join(right, "  ")))
	}
	lines = append(lines, separator(width))

	// Message viewport.
	height := maxInt(3, bodyH-4)
	viewport := m.messageViewport(height, width)
	lines = append(lines, viewport...)

	// Input box: the underline style renders two lines (text + rule).
	input := m.input
	input.Width = maxInt(4, width-5)
	inputStyleToUse := inputStyle
	if m.focus == focusInput {
		inputStyleToUse = inputFocusedStyle
	}
	lines = append(lines, strings.Split(inputStyleToUse.Width(width).Render(input.View()), "\n")...)
	return lines
}

// messageViewport renders the visible slice of the message history.
func (m mainWindowModel) messageViewport(height, width int) []string {
	conv := m.selectedConv()
	if conv == nil {
		return centerBlock(height, width, []string{
			hintStyle.Render("从左侧选择一个会话"),
			hintStyle.Render("Enter 打开，Tab 切换焦点"),
		})
	}
	all := m.messageLines()
	if len(all) == 0 {
		return centerBlock(height, width, []string{hintStyle.Render("还没有消息，发一句打个招呼吧")})
	}
	maxOffset := maxInt(0, len(all)-height)
	offset := m.msgOffset
	if m.msgFollow {
		offset = maxOffset
	}
	offset = clamp(offset, 0, maxOffset)
	visible := make([]string, 0, height)
	for i := 0; i < height; i++ {
		index := offset + i
		if index < len(all) {
			visible = append(visible, all[index])
		} else {
			visible = append(visible, "")
		}
	}
	return visible
}

// messageLines renders the whole message history for the selected chat.
func (m mainWindowModel) messageLines() []string {
	conv := m.selectedConv()
	if conv == nil {
		return nil
	}
	messages := m.messages[conv.ChatID]
	width := m.chatWidth()
	textWidth := maxInt(8, width-8)

	var lines []string
	for _, msg := range messages {
		name := msg.Sender.Name
		if name == "" {
			name = "未知用户"
		}
		own := msg.IsOwn() || (m.user != nil && m.user.Data != nil && msg.Sender.ID != "" &&
			msg.Sender.ID == m.user.Data.ID)
		if msg.Recalled {
			lines = append(lines, lipgloss.PlaceHorizontal(width, lipgloss.Center,
				messageTipStyle.Render(fmt.Sprintf("「%s」撤回了一条消息", truncate(name, 12)))))
			lines = append(lines, "")
			continue
		}

		header := fmt.Sprintf("%s  %s", truncate(name, 16), formatClock(msg.TimestampMs))
		if msg.Edited {
			header += " (已编辑)"
		}
		if own {
			header = fmt.Sprintf("我  %s", formatClock(msg.TimestampMs))
			lines = append(lines, lipgloss.PlaceHorizontal(width, lipgloss.Right,
				messageOwnNameSt.Render(header)))
		} else {
			lines = append(lines, messageNameStyle.Render(header))
		}

		for _, line := range m.messageBodyLines(msg, textWidth) {
			if own {
				lines = append(lines, lipgloss.PlaceHorizontal(width, lipgloss.Right,
					messageOwnTextSt.Render(line)))
			} else {
				lines = append(lines, messageTextStyle.Render("  "+line))
			}
		}
		lines = append(lines, "")
	}
	return lines
}

// messageBodyLines renders the body of a message as plain wrapped lines.
func (m mainWindowModel) messageBodyLines(msg api.Message, width int) []string {
	var lines []string
	if quote := strings.TrimSpace(msg.Content.QuoteMsgText); quote != "" {
		lines = append(lines, "↪ 引用: "+strings.Join(strings.Fields(quote), " "))
	} else if msg.QuoteMsgID != "" {
		lines = append(lines, "↪ 引用了一条消息")
	}
	switch msg.ContentType {
	case api.ContentTypeText, 0:
		lines = append(lines, wrapText(strings.TrimSpace(msg.Content.Text), width)...)
	case 2: // image
		meta := "[图片]"
		if msg.Content.Width > 0 && msg.Content.Height > 0 {
			meta += fmt.Sprintf(" %dx%d", msg.Content.Width, msg.Content.Height)
		}
		if msg.Content.ImageURL != "" {
			meta += " " + truncate(msg.Content.ImageURL, maxInt(8, width/2))
		}
		lines = append(lines, meta)
	case 3: // markdown
		if text := strings.TrimSpace(msg.Content.Text); text != "" {
			lines = append(lines, wrapText(renderInline(text), width)...)
		}
	case 4: // file
		name := strings.TrimSpace(msg.Content.FileName)
		if name == "" {
			name = "文件"
		}
		meta := "[文件] " + truncate(name, maxInt(8, width/2))
		if msg.Content.FileSize > 0 {
			meta += " (" + humanBytes(msg.Content.FileSize) + ")"
		}
		lines = append(lines, meta)
		if msg.Content.FileURL != "" {
			lines = append(lines, truncate(msg.Content.FileURL, maxInt(8, width)))
		}
	case 5:
		lines = append(lines, "[表单]")
	case 6: // article card
		title := strings.TrimSpace(msg.Content.PostTitle)
		if title == "" {
			title = "文章"
		}
		lines = append(lines, "[文章] "+truncate(title, maxInt(8, width/2)))
		if content := strings.TrimSpace(msg.Content.PostContent); content != "" {
			lines = append(lines, wrapText(content, width)...)
		}
	case 7:
		lines = append(lines, "[表情]")
	case 8: // html
		text := strings.TrimSpace(stripHTMLTags(msg.Content.Text))
		if text == "" {
			text = "[HTML 消息]"
		}
		lines = append(lines, wrapText(text, width)...)
	case 11:
		meta := "[语音]"
		if msg.Content.AudioTime > 0 {
			meta += fmt.Sprintf(" %d 秒", msg.Content.AudioTime)
		}
		lines = append(lines, meta)
	case 12:
		lines = append(lines, "[视频]")
	case 13:
		meta := "[语音通话]"
		if msg.Content.CallStatus != "" {
			meta += " " + msg.Content.CallStatus
		}
		lines = append(lines, meta)
	case 14:
		lines = append(lines, "[卡片消息]")
	default:
		preview := msg.Preview()
		if strings.TrimSpace(preview) == "" {
			preview = "[消息]"
		}
		lines = append(lines, wrapText(preview, width)...)
	}
	if tip := strings.TrimSpace(msg.Content.Tip); tip != "" {
		lines = append(lines, "· "+tip)
	}
	if len(lines) == 0 {
		lines = append(lines, msg.Preview())
	}
	return lines
}

// stripHTMLTags removes HTML tags from a message body.
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// humanBytes formats a byte count for display.
func humanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// chatWidth returns the chat panel width.
func (m mainWindowModel) chatWidth() int {
	inner := m.innerWidth()
	convW := m.convPanelWidth(inner)
	width := inner - sidebarWidth - 2*panelGap - convW
	if width < 10 {
		width = 10
	}
	return width
}

// viewHints returns the bottom hint bar for the current focus.
func (m mainWindowModel) viewHints() string {
	switch m.focus {
	case focusSidebar:
		return sidebarHint()
	case focusSearch:
		return "输入关键词筛选会话 · Enter 跳到列表 · Esc 清空 · Tab 切换焦点"
	case focusConversations:
		return "↑↓ 选择会话 · Enter 打开 · / 搜索 · r 刷新会话 · Tab 切换焦点"
	case focusMessages:
		return "↑↓ 滚动 · PgUp/PgDn 翻页 · End 最新 · r 重新加载 · Esc 返回 · Tab 切换焦点"
	default:
		return "输入消息 · Enter 发送 · Esc 返回会话列表 · Tab 切换焦点 · Ctrl+C 退出"
	}
}

// ---------------------------------------------------------------------------
// formatting helpers

// formatConversationTime formats the last-message time of a conversation.
func formatConversationTime(conv api.Conversation) string {
	seconds := int64(conv.SendTimestamp)
	if seconds <= 0 && conv.TimestampMs > 0 {
		seconds = int64(conv.TimestampMs / 1000)
	}
	if seconds <= 0 {
		return ""
	}
	t := time.Unix(seconds, 0).In(timeLocation)
	now := timeNow().In(timeLocation)
	switch {
	case t.Format("2006-01-02") == now.Format("2006-01-02"):
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("01-02")
	default:
		return t.Format("2006-01-02")
	}
}

// formatClock formats a millisecond timestamp as HH:MM.
func formatClock(timestampMs uint64) string {
	if timestampMs == 0 {
		return ""
	}
	return time.UnixMilli(int64(timestampMs)).In(timeLocation).Format("15:04")
}

// composeLeftRight joins a styled left and right string to exactly width cells.
func composeLeftRight(width int, left, right string) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		left = truncate(left, maxInt(1, width-lipgloss.Width(right)-1))
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

// centerBlock vertically centres a small block inside height lines.
func centerBlock(height, width int, block []string) []string {
	lines := make([]string, 0, height)
	if len(block) > height {
		block = block[:height]
	}
	top := (height - len(block)) / 2
	for i := 0; i < top; i++ {
		lines = append(lines, "")
	}
	for _, line := range block {
		lines = append(lines, lipgloss.PlaceHorizontal(width, lipgloss.Center, line))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}
