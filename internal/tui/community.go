package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/logging"
)

// communityFocus identifies the focused pane of the community page.
type communityFocus int

const (
	communitySidebar communityFocus = iota
	communityTabs
	communityList
	communityPosts
	communityArticle
)

// communityViewMode distinguishes the list/detail layout from the article view.
type communityViewMode int

const (
	communityListView communityViewMode = iota
	communityArticleView
)

// communityTab describes one top-of-list tab.
type communityTab struct {
	Label string
	Typ   int32 // following-ba-list typ: 1 关注, 2 热门, 3 我的, 4 全部
}

var communityTabList = []communityTab{
	{Label: "热门", Typ: 2},
	{Label: "全部", Typ: 4},
	{Label: "关注", Typ: 1},
	{Label: "我的", Typ: 3},
}

// Messages produced by community commands.

type communitiesLoadedMsg struct {
	tab         int
	page        int
	communities []api.Community
	total       int
	err         error
}

type communityInfoMsg struct {
	id        int64
	community *api.Community
	err       error
}

type postsLoadedMsg struct {
	baID  int64
	page  int
	posts []api.Post
	total int
	err   error
}

type postDetailMsg struct {
	postID   int64
	detail   *api.PostDetail
	comments []api.Comment
	err      error
}

// communityModel is the "社区" page.
type communityModel struct {
	client *api.Client
	token  string
	user   *api.UserInfo

	width, height int

	focus         communityFocus
	sidebarCursor section
	view          communityViewMode

	tab             int
	communities     []api.Community
	communityTot    int
	communitiesPage int
	listIndex       int
	listOffset      int
	selectedBa      *api.Community
	posts           []api.Post
	postsTotal      int
	postsPage       int
	postIndex       int
	postOffset      int
	postDetail      *api.PostDetail
	postComments    []api.Comment
	articleOffset   int

	loading bool
	errText string
	okText  string
	spinner spinner.Model
}

// newCommunityModel creates the community page.
func newCommunityModel(client *api.Client, token string) communityModel {
	return communityModel{
		client:        client,
		token:         token,
		focus:         communityList,
		sidebarCursor: sectionCommunity,
		spinner:       newSpinner(),
	}
}

// withUser attaches the account profile.
func (m communityModel) withUser(user *api.UserInfo) communityModel {
	m.user = user
	return m
}

// resize records the terminal size.
func (m communityModel) resize(width, height int) communityModel {
	m.width, m.height = width, height
	return m
}

// init loads the default (热门) community list.
func (m communityModel) init() (communityModel, tea.Cmd) {
	m.loading = true
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick, loadCommunitiesCmd(m.client, m.token, m.tab, 1))
}

// refresh reloads the current tab.
func (m communityModel) refresh() (communityModel, tea.Cmd) {
	m.loading = true
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick, loadCommunitiesCmd(m.client, m.token, m.tab, 1))
}

// loadCommunitiesCmd fetches one page of communities for a tab.
func loadCommunitiesCmd(client *api.Client, token string, tab, page int) tea.Cmd {
	typ := communityTabList[clamp(tab, 0, len(communityTabList)-1)].Typ
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		communities, total, err := client.Communities(ctx, token, typ, page, 30)
		return communitiesLoadedMsg{tab: tab, page: page, communities: communities, total: total, err: err}
	}
}

// loadCommunityInfoCmd fetches the details of one community.
func loadCommunityInfoCmd(client *api.Client, token string, id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		community, err := client.CommunityInfo(ctx, token, id)
		return communityInfoMsg{id: id, community: community, err: err}
	}
}

// loadPostsCmd fetches the article list of one community.
func loadPostsCmd(client *api.Client, token string, baID int64, page int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		posts, total, err := client.Posts(ctx, token, baID, page, 30)
		return postsLoadedMsg{baID: baID, page: page, posts: posts, total: total, err: err}
	}
}

// loadPostDetailCmd fetches an article and its comments.
func loadPostDetailCmd(client *api.Client, token string, postID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		detail, err := client.PostDetail(ctx, token, postID)
		if err != nil {
			return postDetailMsg{postID: postID, err: err}
		}
		comments, _, commentErr := client.Comments(ctx, token, postID, 1, 20)
		if commentErr != nil {
			comments = nil
		}
		return postDetailMsg{postID: postID, detail: detail, comments: comments}
	}
}

// Update handles messages for the community page.
func (m communityModel) Update(msg tea.Msg) (communityModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case communitiesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			return m.handleLoadError(msg.err, "加载社区列表失败")
		}
		m.communityTot = msg.total
		if msg.page <= 1 {
			m.tab = msg.tab
			m.communities = msg.communities
			m.communitiesPage = 1
			m.listIndex = 0
			m.listOffset = 0
			m.posts = nil
			m.selectedBa = nil
			m.errText = ""
			if cmd := m.ensureCommunityCmd(); cmd != nil {
				return m, cmd
			}
			return m, nil
		}
		seen := make(map[int64]bool, len(m.communities))
		for _, item := range m.communities {
			seen[item.ID] = true
		}
		for _, item := range msg.communities {
			if !seen[item.ID] {
				m.communities = append(m.communities, item)
			}
		}
		m.communitiesPage = msg.page
		return m, nil

	case communityInfoMsg:
		m.loading = false
		if msg.err != nil {
			return m.handleLoadError(msg.err, "加载分区信息失败")
		}
		m.selectedBa = msg.community
		return m, nil

	case postsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			return m.handleLoadError(msg.err, "加载文章列表失败")
		}
		m.postsTotal = msg.total
		if msg.page <= 1 {
			m.posts = msg.posts
			m.postsPage = 1
			m.postIndex = 0
			m.postOffset = 0
			m.errText = ""
			return m, nil
		}
		seen := make(map[int64]bool, len(m.posts))
		for _, post := range m.posts {
			seen[post.ID] = true
		}
		for _, post := range msg.posts {
			if !seen[post.ID] {
				m.posts = append(m.posts, post)
			}
		}
		m.postsPage = msg.page
		return m, nil

	case postDetailMsg:
		m.loading = false
		if msg.err != nil {
			return m.handleLoadError(msg.err, "加载文章失败")
		}
		m.postDetail = msg.detail
		m.postComments = msg.comments
		m.articleOffset = 0
		m.view = communityArticleView
		m.focus = communityArticle
		m.errText = ""
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleMouse handles click and wheel events on the community page.
func (m communityModel) handleMouse(msg tea.MouseMsg) (communityModel, tea.Cmd) {
	x, y, ok := mouseBodyLocal(msg, m.width, m.height)
	if !ok || m.width == 0 {
		return m, nil
	}
	inner := m.innerWidth()
	listW := m.listPanelWidth(inner)
	listStart, detailStart := standardColumns(inner, listW)

	if delta := wheelDelta(msg); delta != 0 {
		if m.view == communityArticleView {
			m.articleOffset += delta * 3
			m.clampArticleOffset()
			return m, nil
		}
		switch {
		case x < listStart:
			return m, nil
		case x < detailStart:
			if len(m.communities) > 0 {
				m.listIndex = clamp(m.listIndex+delta, 0, len(m.communities)-1)
				m.ensureListVisible()
				return m, m.ensureCommunityCmd()
			}
		default:
			if len(m.posts) > 0 {
				m.postIndex = clamp(m.postIndex+delta, 0, len(m.posts)-1)
				m.ensurePostsVisible()
			}
		}
		return m, nil
	}
	if !isLeftClick(msg) {
		return m, nil
	}
	if x < sidebarWidth {
		if sec, hit := sectionAtRow(y); hit {
			m.sidebarCursor = sec
			if sec == sectionCommunity {
				m.focus = communityList
				return m, nil
			}
			return m, func() tea.Msg { return switchSectionMsg{to: sec} }
		}
		return m, nil
	}
	if m.view == communityArticleView {
		if x >= listStart {
			m.focus = communityArticle
		}
		return m, nil
	}
	if x < listStart {
		return m, nil
	}
	if x < detailStart {
		if y == 0 {
			if index := m.tabAt(x-listStart, listW); index >= 0 && index != m.tab {
				m.tab = index
				m.loading = true
				m.focus = communityTabs
				return m, tea.Batch(m.spinner.Tick, loadCommunitiesCmd(m.client, m.token, m.tab, 1))
			}
			return m, nil
		}
		if y < 2 {
			return m, nil
		}
		index := m.listOffset + (y-2)/2
		if index >= 0 && index < len(m.communities) {
			m.listIndex = index
			m.focus = communityList
			return m, m.ensureCommunityCmd()
		}
		return m, nil
	}
	// Article list of the selected community (starts after header rows).
	if y >= 3 {
		index := m.postOffset + (y-3)/2
		if index >= 0 && index < len(m.posts) {
			m.postIndex = index
			m.focus = communityPosts
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, loadPostDetailCmd(m.client, m.token, m.posts[index].ID))
		}
	}
	return m, nil
}

// handleLoadError handles a failed load (session expiry is forwarded).
func (m communityModel) handleLoadError(err error, prefix string) (communityModel, tea.Cmd) {
	m.loading = false
	if api.IsSessionExpired(err) {
		return m, func() tea.Msg { return expiringTokenMsg{err: err} }
	}
	logging.Error("%s: %v", prefix, err)
	m.errText = prefix + ": " + api.FriendlyMessage(err)
	return m, nil
}

// handleKey processes key presses.
func (m communityModel) handleKey(msg tea.KeyMsg) (communityModel, tea.Cmd) {
	key := msg.String()

	if key == "tab" {
		m.focus = m.nextFocus()
		return m, nil
	}
	if key == "shift+tab" {
		m.focus = m.prevFocus()
		return m, nil
	}

	switch m.focus {
	case communitySidebar:
		return m.handleSidebarKey(key)
	case communityTabs:
		return m.handleTabsKey(key)
	case communityList:
		return m.handleListKey(key)
	case communityPosts:
		return m.handlePostsKey(key)
	default:
		return m.handleArticleKey(key)
	}
}

// handleSidebarKey handles sidebar navigation.
func (m communityModel) handleSidebarKey(key string) (communityModel, tea.Cmd) {
	switch key {
	case "up":
		m.sidebarCursor = nextSectionCursor(m.sidebarCursor, -1)
		return m, nil
	case "down":
		m.sidebarCursor = nextSectionCursor(m.sidebarCursor, 1)
		return m, nil
	case "enter":
		if target := m.sidebarCursor; target != sectionCommunity {
			return m, func() tea.Msg { return switchSectionMsg{to: target} }
		}
		m.focus = communityList
		return m, nil
	case "right":
		m.focus = communityTabs
		return m, nil
	case "r":
		return m.refresh()
	case "c":
		return m, func() tea.Msg { return openChatMsg{} }
	}
	return m, nil
}

// handleTabsKey handles the tab bar.
func (m communityModel) handleTabsKey(key string) (communityModel, tea.Cmd) {
	switch key {
	case "left", "up":
		if m.tab > 0 {
			m.tab--
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, loadCommunitiesCmd(m.client, m.token, m.tab, 1))
		}
		return m, nil
	case "right", "down":
		if m.tab < len(communityTabList)-1 {
			m.tab++
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, loadCommunitiesCmd(m.client, m.token, m.tab, 1))
		}
		return m, nil
	case "enter":
		m.focus = communityList
		return m, nil
	case "esc":
		m.focus = communitySidebar
		return m, nil
	case "r":
		return m.refresh()
	}
	return m, nil
}

// handleListKey handles the community list.
func (m communityModel) handleListKey(key string) (communityModel, tea.Cmd) {
	switch key {
	case "up":
		if m.listIndex > 0 {
			m.listIndex--
			m.ensureListVisible()
		}
		return m, m.ensureCommunityCmd()
	case "down":
		if m.listIndex < len(m.communities)-1 {
			m.listIndex++
			m.ensureListVisible()
			return m, m.ensureCommunityCmd()
		}
		if len(m.communities) < m.communityTot && !m.loading {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, loadCommunitiesCmd(m.client, m.token, m.tab, m.communitiesPage+1))
		}
		return m, m.ensureCommunityCmd()
	case "enter":
		if conv := m.selectedCommunity(); conv != nil {
			m.focus = communityPosts
			if cmd := m.ensurePostsCmd(); cmd != nil {
				return m, cmd
			}
		}
		return m, nil
	case "right":
		m.focus = communityTabs
		return m, nil
	case "esc":
		m.focus = communitySidebar
		return m, nil
	case "r":
		return m.refresh()
	}
	return m, nil
}

// handlePostsKey handles the article list of the selected community.
func (m communityModel) handlePostsKey(key string) (communityModel, tea.Cmd) {
	switch key {
	case "up":
		if m.postIndex > 0 {
			m.postIndex--
			m.ensurePostsVisible()
		}
		return m, nil
	case "down":
		if m.postIndex < len(m.posts)-1 {
			m.postIndex++
			m.ensurePostsVisible()
			return m, nil
		}
		if m.selectedBa != nil && len(m.posts) < m.postsTotal && !m.loading {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, loadPostsCmd(m.client, m.token, m.selectedBa.ID, m.postsPage+1))
		}
		return m, nil
	case "enter":
		if post := m.selectedPost(); post != nil {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, loadPostDetailCmd(m.client, m.token, post.ID))
		}
		return m, nil
	case "esc", "left":
		m.focus = communityList
		return m, nil
	case "r":
		if m.selectedBa == nil {
			return m, nil
		}
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, loadPostsCmd(m.client, m.token, m.selectedBa.ID, 1))
	}
	return m, nil
}

// handleArticleKey scrolls the article view.
func (m communityModel) handleArticleKey(key string) (communityModel, tea.Cmd) {
	switch key {
	case "up":
		if m.articleOffset > 0 {
			m.articleOffset--
		}
	case "down":
		m.articleOffset++
		m.clampArticleOffset()
	case "pgup":
		m.articleOffset = maxInt(0, m.articleOffset-m.articleViewportHeight())
	case "pgdown":
		m.articleOffset += m.articleViewportHeight()
		m.clampArticleOffset()
	case "home", "g":
		m.articleOffset = 0
	case "end", "G":
		m.articleOffset = 1 << 20
		m.clampArticleOffset()
	case "esc", "left":
		m.view = communityListView
		m.focus = communityPosts
	}
	return m, nil
}

// clampArticleOffset keeps the article scroll offset in range.
func (m *communityModel) clampArticleOffset() {
	lines := m.articleLines()
	maxOffset := maxInt(0, len(lines)-m.articleViewportHeight())
	if m.articleOffset > maxOffset {
		m.articleOffset = maxOffset
	}
}

// nextFocus cycles focus panes for the current view.
func (m communityModel) nextFocus() communityFocus {
	order := []communityFocus{communitySidebar, communityTabs, communityList, communityPosts}
	if m.view == communityArticleView {
		order = []communityFocus{communitySidebar, communityArticle}
	}
	for i, focus := range order {
		if focus == m.focus {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

// prevFocus is the reverse of nextFocus.
func (m communityModel) prevFocus() communityFocus {
	order := []communityFocus{communitySidebar, communityTabs, communityList, communityPosts}
	if m.view == communityArticleView {
		order = []communityFocus{communitySidebar, communityArticle}
	}
	for i, focus := range order {
		if focus == m.focus {
			return order[(i-1+len(order))%len(order)]
		}
	}
	return order[0]
}

// selectedCommunity returns the highlighted community.
func (m communityModel) selectedCommunity() *api.Community {
	if len(m.communities) == 0 {
		return nil
	}
	return &m.communities[clamp(m.listIndex, 0, len(m.communities)-1)]
}

// selectedPost returns the highlighted article.
func (m communityModel) selectedPost() *api.Post {
	if len(m.posts) == 0 {
		return nil
	}
	return &m.posts[clamp(m.postIndex, 0, len(m.posts)-1)]
}

// ensureCommunityCmd loads community info + posts when a new community is selected.
func (m *communityModel) ensureCommunityCmd() tea.Cmd {
	community := m.selectedCommunity()
	if community == nil {
		return nil
	}
	if m.selectedBa != nil && m.selectedBa.ID == community.ID && len(m.posts) > 0 {
		return nil
	}
	m.selectedBa = community
	m.posts = nil
	m.loading = true
	return tea.Batch(m.spinner.Tick,
		loadCommunityInfoCmd(m.client, m.token, community.ID),
		loadPostsCmd(m.client, m.token, community.ID, 1))
}

// ensurePostsCmd loads the article list when missing.
func (m *communityModel) ensurePostsCmd() tea.Cmd {
	if m.selectedBa == nil || len(m.posts) > 0 {
		return nil
	}
	m.loading = true
	return tea.Batch(m.spinner.Tick, loadPostsCmd(m.client, m.token, m.selectedBa.ID, 1))
}

// ensureListVisible keeps the selected community visible.
func (m *communityModel) ensureListVisible() {
	height := m.listViewportHeight()
	if height <= 0 {
		return
	}
	if m.listIndex < m.listOffset {
		m.listOffset = m.listIndex
	}
	if m.listIndex >= m.listOffset+height {
		m.listOffset = m.listIndex - height + 1
	}
}

// ensurePostsVisible keeps the selected article visible.
func (m *communityModel) ensurePostsVisible() {
	height := m.postsViewportHeight()
	if height <= 0 {
		return
	}
	if m.postIndex < m.postOffset {
		m.postOffset = m.postIndex
	}
	if m.postIndex >= m.postOffset+height {
		m.postOffset = m.postIndex - height + 1
	}
}

// ---------------------------------------------------------------------------
// layout

// innerWidth is the usable body width.
func (m communityModel) innerWidth() int {
	width, _ := screenSize(m.width, m.height)
	return width - 2*chromePadX
}

// bodyHeight is the number of body rows.
func (m communityModel) bodyHeight() int {
	_, height := screenSize(m.width, m.height)
	return maxInt(8, height-4)
}

// listPanelWidth returns the width of the community list column.
func (m communityModel) listPanelWidth(inner int) int {
	width := clamp(inner/3, 24, 34)
	if inner-width-2*panelGap-26 < 26 {
		width = maxInt(18, inner-2*panelGap-26)
	}
	return width
}

// listViewportHeight returns the visible community rows (2 lines each).
func (m communityModel) listViewportHeight() int {
	return maxInt(2, (m.bodyHeight()-3)/2)
}

// postsViewportHeight returns the visible article rows (2 lines each).
func (m communityModel) postsViewportHeight() int {
	return maxInt(2, (m.bodyHeight()-2)/2)
}

// articleViewportHeight returns the number of visible article lines.
func (m communityModel) articleViewportHeight() int {
	return maxInt(3, m.bodyHeight()-1)
}

// View implements tea.Model for the community page.
func (m communityModel) View() string {
	width, height := screenSize(m.width, m.height)
	inner := width - 2*chromePadX
	bodyH := maxInt(8, height-4)

	sidebar := renderSidebar(bodyH, sectionCommunity, m.sidebarCursor, m.focus == communitySidebar)
	var body []string
	if m.view == communityArticleView {
		infoW := clamp(inner/4, 20, 30)
		articleW := inner - sidebarWidth - 2*panelGap - infoW
		if articleW < 20 {
			articleW = maxInt(20, inner-sidebarWidth-2*panelGap-infoW)
		}
		body = joinColumns(sidebar, m.articleColumn(bodyH, articleW), m.articleInfoColumn(bodyH, infoW))
	} else {
		listW := m.listPanelWidth(inner)
		detailW := inner - sidebarWidth - 2*panelGap - listW
		if detailW < 20 {
			detailW = maxInt(20, inner-sidebarWidth-2*panelGap-listW)
		}
		body = joinColumns(sidebar, m.listColumn(bodyH, listW), m.communityDetailColumn(bodyH, detailW))
	}

	return screenChromeTop(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		subtitleStyle.Render("社区"),
		body,
		m.viewHints(),
	)
}

// listColumn renders the tab bar + community list.
func (m communityModel) listColumn(bodyH, width int) []string {
	lines := make([]string, 0, bodyH)

	// Tabs (compact style so they fit narrow list columns).
	tabLine := m.tabsLine()
	if m.focus == communityTabs {
		tabLine = titleAccentStyle.Render("‹ ") + tabLine + titleAccentStyle.Render(" ›")
	}
	lines = append(lines, padLine(lipgloss.PlaceHorizontal(width, lipgloss.Center, tabLine), width))
	lines = append(lines, separator(width))

	height := maxInt(1, (bodyH-4)/2)
	offset := clamp(m.listOffset, 0, maxInt(0, len(m.communities)-1))
	for i := 0; i < height; i++ {
		index := offset + i
		if index < len(m.communities) {
			row := m.renderCommunityRow(m.communities[index], index == m.listIndex, width)
			for _, line := range row {
				lines = append(lines, padLine(line, width))
			}
		} else {
			lines = append(lines, padLine("", width), padLine("", width))
		}
	}
	if len(m.communities) == 0 {
		lines = append(lines, padLine(hintStyle.Render(" 暂无分区数据，按 r 刷新"), width), padLine("", width))
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

// tabsLine renders the compact tab bar.
func (m communityModel) tabsLine() string {
	var parts []string
	for i, tab := range communityTabList {
		if len(parts) > 0 {
			parts = append(parts, " ")
		}
		style := tabInactiveCompactStyle
		if i == m.tab {
			style = tabActiveCompactStyle
		}
		parts = append(parts, style.Render(tab.Label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// tabAt maps a list-column x coordinate to a tab index (-1 when outside).
func (m communityModel) tabAt(relX, width int) int {
	widths := make([]int, 0, len(communityTabList))
	total := 0
	for _, tab := range communityTabList {
		w := lipgloss.Width(tabInactiveCompactStyle.Render(tab.Label))
		widths = append(widths, w)
		total += w
	}
	total += len(communityTabList) - 1
	pos := (width - total) / 2
	for i, w := range widths {
		if relX >= pos && relX < pos+w {
			return i
		}
		pos += w + 1
	}
	return -1
}

// renderCommunityRow renders one community as two lines.
func (m communityModel) renderCommunityRow(community api.Community, selected bool, width int) []string {
	marker := " "
	if selected {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌")
	}
	nameStyle := convNameStyle
	if selected {
		nameStyle = convSelectedStyle
	}
	name := nameStyle.Render(truncate(community.Name, maxInt(4, width-3)))
	stats := hintStyle.Render(truncate(fmt.Sprintf("成员 %d · 文章 %d", community.MemberNum, community.PostNum), maxInt(4, width-3)))
	return []string{marker + " " + name, "   " + stats}
}

// statusLine renders the loading/error/ok line.
func (m communityModel) statusLine(width int) string {
	switch {
	case m.loading:
		return truncate(lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ", hintStyle.Render("加载中…")), width)
	case m.errText != "":
		return errStyle.Render(truncate(m.errText, width))
	case m.okText != "":
		return okStyle.Render(truncate(m.okText, width))
	}
	if m.communityTot > 0 {
		return hintStyle.Render(truncate(fmt.Sprintf("共 %d 个分区", m.communityTot), width))
	}
	return ""
}

// communityDetailColumn renders the selected community info and article list.
func (m communityModel) communityDetailColumn(bodyH, width int) []string {
	lines := make([]string, 0, bodyH)
	community := m.selectedBa
	if community == nil {
		lines = append(lines, "", hintStyle.Render(" 从左侧选择一个分区"), "")
		lines = append(lines, hintStyle.Render(" ↑↓ 选择 · Enter 查看文章"))
		return padLines(lines, bodyH, width)
	}
	title := community.Name
	if community.IsFollowed == "1" {
		title += "  " + okStyle.Render("已关注")
	}
	lines = append(lines, chatTitleStyle.Render(truncate(title, maxInt(4, width-2))))
	lines = append(lines, hintStyle.Render(truncate(fmt.Sprintf("成员 %d · 文章 %d · 绑定群 %d",
		community.MemberNum, community.PostNum, community.GroupNum), maxInt(4, width-2))))
	lines = append(lines, separator(width))

	postsH := maxInt(1, (bodyH-5)/2)
	offset := clamp(m.postOffset, 0, maxInt(0, len(m.posts)-1))
	if len(m.posts) == 0 {
		lines = append(lines, hintStyle.Render(" 暂无文章"))
	}
	for i := 0; i < postsH; i++ {
		index := offset + i
		if index >= len(m.posts) {
			break
		}
		post := m.posts[index]
		selected := index == m.postIndex
		marker := " "
		titleStyle := convNameStyle
		if selected && m.focus == communityPosts {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▌")
			titleStyle = convSelectedStyle
		} else if selected {
			titleStyle = convSelectedStyle
		}
		pin := ""
		if post.IsSticky > 0 {
			pin = "置顶 "
		}
		lines = append(lines, marker+" "+titleStyle.Render(truncate(pin+post.Title, maxInt(4, width-3))))
		meta := fmt.Sprintf("%s · %s · 赞 %d · 评 %d", post.SenderNickname, post.CreateTimeText, post.LikeNum, post.CommentNum)
		lines = append(lines, "   "+hintStyle.Render(truncate(meta, maxInt(4, width-4))))
	}
	lines = append(lines, "")
	return padLines(lines, bodyH, width)
}

// articleColumn renders the article content.
func (m communityModel) articleColumn(bodyH, width int) []string {
	lines := m.articleLines()
	height := m.articleViewportHeight()
	offset := clamp(m.articleOffset, 0, maxInt(0, len(lines)-height))
	view := make([]string, 0, bodyH)
	for i := 0; i < height; i++ {
		index := offset + i
		if index < len(lines) {
			view = append(view, padLine(lines[index], width))
		} else {
			view = append(view, "")
		}
	}
	view = append(view, m.statusLine(width))
	return padLines(view, bodyH, width)
}

// articleLines renders the whole scrollable article (content + comments).
func (m communityModel) articleLines() []string {
	if m.postDetail == nil {
		return []string{hintStyle.Render(" 正在加载文章…")}
	}
	post := m.postDetail.Post
	width := m.articleWidth()
	var lines []string
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	for _, line := range wrapText(post.Title, width) {
		lines = append(lines, titleStyle.Render(line))
	}
	lines = append(lines, "")
	lines = append(lines, hintStyle.Render(truncate(fmt.Sprintf("作者 %s · %s · 赞 %d · 评论 %d · 收藏 %d",
		post.SenderNickname, post.CreateTimeText, post.LikeNum, post.CommentNum, post.CollectNum), maxInt(8, width))))
	lines = append(lines, separator(width))
	lines = append(lines, renderMarkdown(post.Content, width-2)...)
	lines = append(lines, "")

	// Comments.
	if len(m.postComments) > 0 {
		lines = append(lines, separator(width))
		lines = append(lines, titleAccentStyle.Render("评论"))
		for i, comment := range m.postComments {
			if i >= 10 {
				break
			}
			header := fmt.Sprintf("%s · %s", comment.SenderNickname, comment.CreateTimeText)
			lines = append(lines, hintStyle.Render("  "+truncate(header, maxInt(4, width-4))))
			for _, line := range wrapText(comment.Content, maxInt(8, width-6)) {
				lines = append(lines, "    "+line)
			}
			lines = append(lines, "")
		}
	}
	lines = append(lines, hintStyle.Render("按 Esc 返回分区"))
	return lines
}

// articleInfoColumn renders the community/author info sidebar of the article view.
func (m communityModel) articleInfoColumn(bodyH, width int) []string {
	lines := make([]string, 0, bodyH)
	lines = append(lines, titleAccentStyle.Render("文章信息"), "")
	if m.postDetail != nil {
		post := m.postDetail.Post
		lines = append(lines, m.infoRow(width, "分区", m.postDetail.Ba.Name))
		lines = append(lines, m.infoRow(width, "作者", post.SenderNickname))
		lines = append(lines, m.infoRow(width, "作者 ID", post.SenderID))
		lines = append(lines, m.infoRow(width, "发布时间", post.CreateTimeText))
		lines = append(lines, m.infoRow(width, "点赞", fmt.Sprintf("%d", post.LikeNum)))
		lines = append(lines, m.infoRow(width, "评论", fmt.Sprintf("%d", post.CommentNum)))
		lines = append(lines, m.infoRow(width, "收藏", fmt.Sprintf("%d", post.CollectNum)))
		if m.selectedBa != nil {
			lines = append(lines, "")
			lines = append(lines, titleAccentStyle.Render("分区信息"), "")
			lines = append(lines, m.infoRow(width, "名称", m.selectedBa.Name))
			lines = append(lines, m.infoRow(width, "成员", fmt.Sprintf("%d", m.selectedBa.MemberNum)))
			lines = append(lines, m.infoRow(width, "文章", fmt.Sprintf("%d", m.selectedBa.PostNum)))
			lines = append(lines, m.infoRow(width, "绑定群", fmt.Sprintf("%d", m.selectedBa.GroupNum)))
		}
	}
	lines = append(lines, "", hintStyle.Render("↑↓ 滚动 · Esc 返回"))
	return padLines(lines, bodyH, width)
}

// infoRow renders a label/value pair in the info column.
func (m communityModel) infoRow(width int, label, value string) string {
	const labelW = 8
	return labelStyle.Render(padLine(label, labelW)) +
		valueStyle.Render(truncate(value, maxInt(4, width-labelW-1)))
}

// articleWidth returns the width of the article column.
func (m communityModel) articleWidth() int {
	inner := m.innerWidth()
	infoW := clamp(inner/4, 20, 30)
	width := inner - sidebarWidth - 2*panelGap - infoW
	if width < 20 {
		width = 20
	}
	return width - 2
}

// viewHints returns the footer hint bar.
func (m communityModel) viewHints() string {
	if m.focus == communitySidebar {
		return sidebarHint()
	}
	if m.view == communityArticleView {
		return "↑↓ 滚动 · PgUp/PgDn 翻页 · End 最新 · Esc 返回分区"
	}
	switch m.focus {
	case communityTabs:
		return "←→ 切换分类（热门/全部/关注/我的）· Enter 进入列表 · Esc 返回侧边栏"
	case communityList:
		return "↑↓ 选择分区 · Enter 查看文章 · Tab 切换焦点 · r 刷新 · Esc 返回侧边栏"
	default:
		return "↑↓ 选择文章 · Enter 阅读全文 · Esc 返回分区列表 · r 刷新"
	}
}

// ---------------------------------------------------------------------------
// markdown rendering

var (
	mdImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]*)\)`)
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	mdBoldRe  = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdCodeRe  = regexp.MustCompile("`([^`]+)`")
)

// renderMarkdown renders article content as plain terminal lines with light
// markdown handling (headings/lists/links/images; code fences preserved).
func renderMarkdown(content string, width int) []string {
	if width < 8 {
		width = 8
	}
	var lines []string
	codeBlock := false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			codeBlock = !codeBlock
			continue
		}
		if codeBlock {
			for _, part := range wrapText("  "+line, width) {
				lines = append(lines, hintStyle.Render(part))
			}
			continue
		}
		if trimmed == "" {
			lines = append(lines, "")
			continue
		}
		if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "***") {
			lines = append(lines, separator(width))
			continue
		}
		heading := 0
		for heading < len(trimmed) && trimmed[heading] == '#' {
			heading++
		}
		if heading > 0 && heading < len(trimmed) && trimmed[heading] == ' ' {
			text := strings.TrimSpace(trimmed[heading:])
			text = renderInline(text)
			style := lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
			for _, part := range wrapText(text, width) {
				lines = append(lines, style.Render(part))
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			text := renderInline(strings.TrimSpace(trimmed[2:]))
			wrapped := wrapText("• "+text, width)
			for _, part := range wrapped {
				lines = append(lines, part)
			}
			continue
		}
		if matched := regexp.MustCompile(`^(\d+)[.、]\s*`).FindStringSubmatch(trimmed); matched != nil {
			text := renderInline(strings.TrimSpace(trimmed[len(matched[0]):]))
			for _, part := range wrapText(matched[1]+". "+text, width) {
				lines = append(lines, part)
			}
			continue
		}
		text := renderInline(trimmed)
		for _, part := range wrapText(text, width) {
			lines = append(lines, part)
		}
	}
	return lines
}

// renderInline strips inline markdown syntax for terminal display.
func renderInline(text string) string {
	text = mdImageRe.ReplaceAllString(text, "[图片: $1]")
	text = mdLinkRe.ReplaceAllString(text, "$1 ($2)")
	text = mdBoldRe.ReplaceAllString(text, "$1")
	text = mdCodeRe.ReplaceAllString(text, "$1")
	text = strings.ReplaceAll(text, "**", "")
	return text
}
