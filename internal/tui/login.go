package tui

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/captchaimg"
	"yhchat-tui/internal/config"
)

// loginMethod is the selected login tab.
type loginMethod int

const (
	methodEmail loginMethod = iota
	methodSMS
)

// smsStage tracks the multi-step SMS login flow.
type smsStage int

const (
	smsStagePhone   smsStage = iota // enter mobile number, then fetch captcha
	smsStageCaptcha                 // read image captcha, then send SMS
	smsStageCode                    // enter SMS code, then login
)

// focusID identifies the focused widget on the login screen.
type focusID int

const (
	focusTabs focusID = iota
	focusEmail
	focusPassword
	focusPhone
	focusImageCode
	focusSMSCode
	focusSubmit
)

const (
	maxCaptchaRows = 14
	requestTimeout = api.DefaultTimeout + 15*time.Second
)

// loginModel is the login screen.
type loginModel struct {
	client *api.Client

	width, height int

	method loginMethod
	stage  smsStage
	focus  focusID

	email     textinput.Model
	password  textinput.Model
	phone     textinput.Model
	imageCode textinput.Model
	smsCode   textinput.Model
	spinner   spinner.Model

	captcha *api.ImageCaptcha
	capImg  image.Image

	busy     bool
	busyText string
	errText  string
	okText   string
	notice   string // persistent hint from the App (e.g. session expired)

	useColor bool
	deviceID string
}

// newLoginModel builds the login screen, restoring the last used method and
// account names.
func newLoginModel(client *api.Client, prefs config.Config) loginModel {
	m := loginModel{
		client:    client,
		method:    methodEmail,
		stage:     smsStagePhone,
		focus:     focusEmail,
		email:     newTextInput("you@example.com", false, 128),
		password:  newTextInput("password", true, 128),
		phone:     newTextInput("13800000000", false, 11),
		imageCode: newTextInput("captcha text", false, 16),
		smsCode:   newTextInput("SMS code", false, 16),
		spinner:   newSpinner(),
		useColor:  colorEnabled(),
		deviceID:  client.DeviceID,
	}
	if prefs.LoginMethod == "sms" {
		m.method = methodSMS
		m.focus = focusPhone
	}
	if prefs.LastEmail != "" {
		m.email.SetValue(prefs.LastEmail)
	}
	if prefs.LastMobile != "" {
		m.phone.SetValue(prefs.LastMobile)
	}
	return m
}

// newTextInput creates a styled single-line input.
func newTextInput(placeholder string, secret bool, limit int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = limit
	if secret {
		ti.EchoMode = textinput.EchoPassword
	}
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorMuted)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	ti.PromptStyle = lipgloss.NewStyle()
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(colorAccent)
	return ti
}

// colorEnabled reports whether 24-bit ANSI colors are safe to emit.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return true
}

// start focuses the initial widget and starts the spinner.
func (m loginModel) start() (loginModel, tea.Cmd) {
	cmd := m.applyFocus()
	return m, tea.Batch(cmd, m.spinner.Tick)
}

// resize records the terminal size.
func (m loginModel) resize(width, height int) loginModel {
	m.width, m.height = width, height
	return m
}

// input returns a pointer to the text input identified by id (nil for
// non-input focus targets).
func (m *loginModel) input(id focusID) *textinput.Model {
	switch id {
	case focusEmail:
		return &m.email
	case focusPassword:
		return &m.password
	case focusPhone:
		return &m.phone
	case focusImageCode:
		return &m.imageCode
	case focusSMSCode:
		return &m.smsCode
	default:
		return nil
	}
}

// applyFocus blurs every input and focuses the current one.
func (m *loginModel) applyFocus() tea.Cmd {
	for _, id := range []focusID{focusEmail, focusPassword, focusPhone, focusImageCode, focusSMSCode} {
		m.input(id).Blur()
	}
	if ti := m.input(m.focus); ti != nil {
		return ti.Focus()
	}
	return nil
}

// focusRing returns the focusable widgets for the current form state.
func (m loginModel) focusRing() []focusID {
	ring := []focusID{focusTabs}
	if m.method == methodEmail {
		return append(ring, focusEmail, focusPassword, focusSubmit)
	}
	ring = append(ring, focusPhone)
	if m.stage >= smsStageCaptcha {
		ring = append(ring, focusImageCode)
	}
	if m.stage >= smsStageCode {
		ring = append(ring, focusSMSCode)
	}
	return append(ring, focusSubmit)
}

// nextFocus moves the focus through the ring.
func (m loginModel) nextFocus(delta int) focusID {
	ring := m.focusRing()
	idx := 0
	for i, id := range ring {
		if id == m.focus {
			idx = i
			break
		}
	}
	n := len(ring)
	idx = ((idx+delta)%n + n) % n
	return ring[idx]
}

// firstField is the first editable widget of the current method.
func (m loginModel) firstField() focusID {
	if m.method == methodEmail {
		return focusEmail
	}
	return focusPhone
}

// Init implements tea.Model.
func (m loginModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model for the login screen.
func (m loginModel) Update(msg tea.Msg) (loginModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case captchaLoadedMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = api.FriendlyMessage(msg.err)
			m.okText = ""
			return m, nil
		}
		m.captcha = msg.captcha
		m.capImg = msg.img
		m.stage = smsStageCaptcha
		m.imageCode.SetValue("")
		m.errText = ""
		m.okText = "请输入图片中的字符，然后发送短信验证码"
		m.focus = focusImageCode
		return m, m.applyFocus()

	case smsSentMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = api.FriendlyMessage(msg.err)
			m.okText = ""
			m.focus = focusImageCode
			return m, m.applyFocus()
		}
		m.stage = smsStageCode
		m.smsCode.SetValue("")
		m.errText = ""
		m.okText = "短信验证码已发送至 " + maskPhone(strings.TrimSpace(m.phone.Value()))
		m.focus = focusSMSCode
		return m, m.applyFocus()

	case loginFailedMsg:
		m.busy = false
		m.errText = api.FriendlyMessage(msg.err)
		m.okText = ""
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		// Cursor blink / paste events are handled by the focused input.
		if m.busy {
			return m, nil
		}
		ti := m.input(m.focus)
		if ti == nil {
			return m, nil
		}
		updated, cmd := ti.Update(msg)
		*ti = updated
		return m, cmd
	}
}

// handleKey processes key presses, intercepting navigation keys before they
// reach the focused text input.
func (m loginModel) handleKey(msg tea.KeyMsg) (loginModel, tea.Cmd) {
	key := msg.String()

	if m.busy {
		return m, nil
	}

	switch key {
	case "tab", "down", "ctrl+n":
		m.focus = m.nextFocus(1)
		return m, m.applyFocus()
	case "shift+tab", "up", "ctrl+p":
		m.focus = m.nextFocus(-1)
		return m, m.applyFocus()
	case "ctrl+t":
		m.focus = focusTabs
		return m, m.applyFocus()
	case "enter":
		return m.activate()
	case "esc":
		return m.handleBack()
	case "ctrl+r":
		if m.method == methodSMS {
			return m.refreshCaptcha()
		}
		return m, nil
	case "ctrl+s":
		return m.saveCaptcha()
	}

	if m.focus == focusTabs {
		switch key {
		case "left", "h":
			return m.switchMethod(methodEmail)
		case "right", "l":
			return m.switchMethod(methodSMS)
		case "1":
			return m.switchMethod(methodEmail)
		case "2":
			return m.switchMethod(methodSMS)
		}
		return m, nil
	}

	ti := m.input(m.focus)
	if ti == nil {
		return m, nil
	}
	updated, cmd := ti.Update(msg)
	*ti = updated
	return m, cmd
}

// handleBack implements Esc (one step back in the SMS flow).
func (m loginModel) handleBack() (loginModel, tea.Cmd) {
	if m.notice != "" {
		m.notice = ""
		return m, nil
	}
	if m.errText != "" {
		m.errText = ""
		return m, nil
	}
	if m.method == methodSMS && m.stage != smsStagePhone {
		m.stage--
		if m.stage == smsStagePhone {
			m.captcha = nil
			m.capImg = nil
			m.imageCode.SetValue("")
			m.smsCode.SetValue("")
		}
		m.okText = ""
		m.focus = focusPhone
		return m, m.applyFocus()
	}
	return m, nil
}

// switchMethod changes the login tab, fetching a captcha when needed.
func (m loginModel) switchMethod(target loginMethod) (loginModel, tea.Cmd) {
	if m.busy || m.method == target {
		return m, nil
	}
	m.method = target
	m.errText = ""
	m.okText = ""
	m.notice = ""
	m.focus = m.firstField()
	cmd := m.applyFocus()
	if target == methodSMS && m.captcha == nil && m.stage == smsStagePhone {
		var fetchCmd tea.Cmd
		m, fetchCmd = m.fetchCaptcha()
		cmd = tea.Batch(cmd, fetchCmd)
	}
	return m, cmd
}

// activate handles Enter / button presses for the current form state.
func (m loginModel) activate() (loginModel, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	if m.focus == focusTabs {
		m.focus = m.firstField()
		return m, m.applyFocus()
	}
	if m.method == methodEmail {
		return m.submitEmail()
	}
	switch m.stage {
	case smsStagePhone:
		return m.fetchCaptcha()
	case smsStageCaptcha:
		return m.sendSMSCode()
	default:
		return m.submitSMSLogin()
	}
}

// fetchCaptcha requests a new image captcha from the server.
func (m loginModel) fetchCaptcha() (loginModel, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	m.busy = true
	m.busyText = "正在获取图形验证码…"
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick, m.fetchCaptchaCmd())
}

// refreshCaptcha discards the old captcha and loads a new one.
func (m loginModel) refreshCaptcha() (loginModel, tea.Cmd) {
	m.captcha = nil
	m.capImg = nil
	m.smsCode.SetValue("")
	m.stage = smsStagePhone
	return m.fetchCaptcha()
}

// fetchCaptchaCmd performs the captcha request (and decodes the image) in the
// background.
func (m loginModel) fetchCaptchaCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		captcha, err := client.FetchImageCaptcha(ctx)
		if err != nil {
			return captchaLoadedMsg{err: err}
		}
		img, err := captchaimg.DecodeDataURL(captcha.DataURL)
		if err != nil {
			return captchaLoadedMsg{err: fmt.Errorf("验证码图片解析失败: %w", err)}
		}
		return captchaLoadedMsg{captcha: captcha, img: img}
	}
}

// sendSMSCode validates the form and asks the server to send an SMS code.
func (m loginModel) sendSMSCode() (loginModel, tea.Cmd) {
	mobile := strings.TrimSpace(m.phone.Value())
	if err := validateMobile(mobile); err != nil {
		m.errText = err.Error()
		m.focus = focusPhone
		return m, m.applyFocus()
	}
	imageCode := strings.TrimSpace(m.imageCode.Value())
	if imageCode == "" {
		m.errText = "请输入图片中的验证码"
		m.focus = focusImageCode
		return m, m.applyFocus()
	}
	if m.captcha == nil {
		return m.fetchCaptcha()
	}
	m.busy = true
	m.busyText = "正在发送短信验证码…"
	m.errText = ""
	m.okText = ""

	client, captchaID := m.client, m.captcha.ID
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return smsSentMsg{err: client.SendSMSCode(ctx, mobile, imageCode, captchaID)}
	}
	return m, tea.Batch(m.spinner.Tick, cmd)
}

// submitEmail validates the email form and logs in.
func (m loginModel) submitEmail() (loginModel, tea.Cmd) {
	email := strings.TrimSpace(m.email.Value())
	password := m.password.Value()
	if err := validateEmail(email); err != nil {
		m.errText = err.Error()
		m.focus = focusEmail
		return m, m.applyFocus()
	}
	if password == "" {
		m.errText = "请输入密码"
		m.focus = focusPassword
		return m, m.applyFocus()
	}

	m.busy = true
	m.busyText = "正在登录…"
	m.errText = ""
	m.okText = ""

	client := m.client
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		token, err := client.LoginEmail(ctx, email, password)
		if err != nil {
			return loginFailedMsg{err: err}
		}
		user, userErr := client.UserInfo(ctx, token)
		return loginSucceededMsg{token: token, user: user, userErr: userErr, method: "email", account: email}
	}
	return m, tea.Batch(m.spinner.Tick, cmd)
}

// submitSMSLogin validates the SMS code and logs in.
func (m loginModel) submitSMSLogin() (loginModel, tea.Cmd) {
	mobile := strings.TrimSpace(m.phone.Value())
	if err := validateMobile(mobile); err != nil {
		m.errText = err.Error()
		m.focus = focusPhone
		return m, m.applyFocus()
	}
	code := strings.TrimSpace(m.smsCode.Value())
	if len([]rune(code)) < 4 {
		m.errText = "请输入短信验证码"
		m.focus = focusSMSCode
		return m, m.applyFocus()
	}

	m.busy = true
	m.busyText = "正在登录…"
	m.errText = ""
	m.okText = ""

	client := m.client
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		token, err := client.LoginSMS(ctx, mobile, code)
		if err != nil {
			return loginFailedMsg{err: err}
		}
		user, userErr := client.UserInfo(ctx, token)
		return loginSucceededMsg{token: token, user: user, userErr: userErr, method: "sms", account: mobile}
	}
	return m, tea.Batch(m.spinner.Tick, cmd)
}

// saveCaptcha writes the current captcha image to a temp PNG so the user can
// open it in an image viewer if the terminal rendering is hard to read.
func (m loginModel) saveCaptcha() (loginModel, tea.Cmd) {
	if m.capImg == nil {
		m.errText = "当前没有可保存的验证码图片"
		return m, nil
	}
	f, err := os.CreateTemp("", "yhchat-captcha-*.png")
	if err != nil {
		m.errText = "保存验证码图片失败: " + err.Error()
		return m, nil
	}
	path := f.Name()
	encodeErr := png.Encode(f, m.capImg)
	closeErr := f.Close()
	if encodeErr != nil {
		m.errText = "写入验证码图片失败: " + encodeErr.Error()
		return m, nil
	}
	if closeErr != nil {
		m.errText = "保存验证码图片失败: " + closeErr.Error()
		return m, nil
	}
	m.okText = "验证码图片已保存: " + path
	return m, nil
}

// submitLabel is the text of the primary button for the current state.
func (m loginModel) submitLabel() string {
	if m.method == methodEmail {
		return "登 录"
	}
	switch m.stage {
	case smsStagePhone:
		return "获取图形验证码"
	case smsStageCaptcha:
		return "发送短信验证码"
	default:
		return "登 录"
	}
}

// View implements tea.Model for the login screen. It picks the roomiest layout
// that fits the terminal: a full card with section spacing when there is room,
// and progressively denser variants for short terminals.
func (m loginModel) View() string {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	cardW := clamp(width-8, 44, 80)
	inner := cardW - 6

	footer := hintStyle.Render(fmt.Sprintf(
		"yhchat-tui v%s · 设备 ID %s · Ctrl+C 退出", Version, shortID(m.deviceID)))

	padV := 1
	if height < 28 {
		padV = 0
	}
	captchaRows := clamp(height-22, 4, maxCaptchaRows)

	// Candidate layouts, roomiest first.
	type layout struct {
		content []string
		padV    int
	}
	candidates := make([]layout, 0, 16)
	candidates = append(candidates,
		layout{m.cardContent(inner, true, captchaRows), padV},
		layout{m.cardContent(inner, false, captchaRows), padV},
	)
	for rows := captchaRows; rows >= 4; rows-- {
		candidates = append(candidates, layout{m.cardContent(inner, false, rows), 0})
	}

	chosen := candidates[len(candidates)-1]
	found := false
	for _, c := range candidates {
		if m.cardHeight(c.content, c.padV)+1 <= height {
			chosen = c
			found = true
			break
		}
	}
	if !found {
		// Very short terminal: drop the card chrome entirely and show the bare
		// form, which always fits.
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			m.viewBare(inner))
	}

	style := cardStyle
	if chosen.padV == 0 {
		style = compactCardStyle
	}
	card := style.Width(cardW - 2).Render(lipgloss.JoinVertical(lipgloss.Left, chosen.content...))
	block := lipgloss.JoinVertical(lipgloss.Center, card, footer)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// viewBare renders the login form without card chrome for very short
// terminals. It keeps every interactive element and the current message but
// drops decorations and the hint line.
func (m loginModel) viewBare(inner int) string {
	lines := []string{
		titleAccentStyle.Render("◈ 云湖 TUI") + "  " + subtitleStyle.Render("Yunhu Chat · TUI"),
		m.viewTabs(inner),
	}
	if m.method == methodEmail {
		lines = append(lines, m.viewEmailForm(inner, false)...)
	} else {
		lines = append(lines, m.viewSMSForm(inner, false, 4)...)
	}
	if msg := m.statusMessage(); msg != "" {
		lines = append(lines, lipgloss.PlaceHorizontal(inner, lipgloss.Center, msg))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// statusMessage returns the single-line status message for compact layouts.
func (m loginModel) statusMessage() string {
	switch {
	case m.busy:
		return hintStyle.Render(m.busyText)
	case m.errText != "":
		return errStyle.Render(truncate(m.errText, 60))
	case m.okText != "":
		return okStyle.Render(truncate(m.okText, 60))
	case m.notice != "":
		return warnStyle.Render(truncate(m.notice, 60))
	}
	return ""
}

// cardContent assembles the card body for the current form state.
func (m loginModel) cardContent(inner int, roomy bool, captchaRows int) []string {
	lines := make([]string, 0, 32)
	lines = append(lines, m.viewHeader(inner, roomy)...)
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewTabs(inner))
	if roomy {
		lines = append(lines, "")
	}
	if m.method == methodEmail {
		lines = append(lines, m.viewEmailForm(inner, roomy)...)
	} else {
		lines = append(lines, m.viewSMSForm(inner, roomy, captchaRows)...)
	}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewStatus(inner)...)
	return lines
}

// cardHeight is the rendered height of a card with the given body and vertical
// padding (border included, footer excluded). Some body entries are themselves
// multi-line strings (bordered inputs/buttons), so their newlines are counted.
func (m loginModel) cardHeight(content []string, padV int) int {
	lines := 0
	for _, entry := range content {
		lines += strings.Count(entry, "\n") + 1
	}
	return lines + padV*2 + 2
}

// viewHeader renders the app title block.
func (m loginModel) viewHeader(inner int, roomy bool) []string {
	left := titleAccentStyle.Render("◈ 云湖 TUI")
	right := subtitleStyle.Render("Yunhu Chat · TUI")
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	lines := []string{left + strings.Repeat(" ", gap) + right, separator(inner)}
	if roomy {
		lines = append(lines, truncate(
			subtitleStyle.Render("使用云湖账号登录；登录状态会保存在本机配置目录"), inner))
	}
	return lines
}

// viewTabs renders the login method selector.
func (m loginModel) viewTabs(inner int) string {
	emailStyle, smsStyle := tabInactiveStyle, tabInactiveStyle
	if m.method == methodEmail {
		emailStyle = tabActiveStyle
	} else {
		smsStyle = tabActiveStyle
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top,
		emailStyle.Render("邮箱密码"),
		" ",
		smsStyle.Render("短信验证码"),
	)
	if m.focus == focusTabs {
		row = lipgloss.JoinHorizontal(lipgloss.Top,
			titleAccentStyle.Render("‹"),
			" ",
			row,
			" ",
			titleAccentStyle.Render("›"),
		)
	}
	return lipgloss.PlaceHorizontal(inner, lipgloss.Center, row)
}

// viewEmailForm renders the email/password tab.
func (m loginModel) viewEmailForm(inner int, roomy bool) []string {
	lines := []string{
		m.viewLabel("邮箱", m.focus == focusEmail, inner),
		m.viewInput(focusEmail, inner),
	}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewLabel("密码", m.focus == focusPassword, inner))
	lines = append(lines, m.viewInput(focusPassword, inner))
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewButton(m.submitLabel(), inner))
	return lines
}

// viewSMSForm renders the SMS tab, including the captcha image once loaded.
func (m loginModel) viewSMSForm(inner int, roomy bool, captchaRows int) []string {
	lines := []string{
		m.viewLabel("手机号", m.focus == focusPhone, inner),
		m.viewInput(focusPhone, inner),
	}
	if m.stage >= smsStageCaptcha {
		if roomy {
			lines = append(lines, "")
		}
		lines = append(lines, m.viewLabel("图形验证码", m.focus == focusImageCode, inner))
		lines = append(lines, m.viewCaptcha(inner, captchaRows)...)
		lines = append(lines, m.viewInput(focusImageCode, inner))
	}
	if m.stage >= smsStageCode {
		if roomy {
			lines = append(lines, "")
		}
		lines = append(lines, m.viewLabel("短信验证码", m.focus == focusSMSCode, inner))
		lines = append(lines, m.viewInput(focusSMSCode, inner))
	}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewButton(m.submitLabel(), inner))
	return lines
}

// viewLabel renders a field label.
func (m loginModel) viewLabel(label string, focused bool, inner int) string {
	style := labelStyle
	prefix := "  "
	if focused {
		style = labelFocusedStyle
		prefix = "▸ "
	}
	return truncate(style.Render(prefix+label), inner)
}

// viewInput renders one text input line with a focus underline.
func (m loginModel) viewInput(id focusID, inner int) string {
	ti := m.input(id)
	if ti == nil {
		return ""
	}
	focused := m.focus == id && !m.busy
	input := *ti
	input.Width = maxInt(4, inner-3)
	style := inputStyle
	if focused {
		style = inputFocusedStyle
	}
	return style.Width(inner).Render(input.View())
}

// viewButton renders the primary action button.
func (m loginModel) viewButton(label string, inner int) string {
	style := buttonStyle
	switch {
	case m.busy:
		style = buttonDisabledStyle
	case m.focus == focusSubmit:
		style = buttonFocusedStyle
	}
	return lipgloss.PlaceHorizontal(inner, lipgloss.Center, style.Render(label))
}

// viewCaptcha renders the captcha image inside a bordered box.
//
// Note on widths: lipgloss.Style.Width sets the content width including
// padding, and a border adds one cell on each side, so a box of total width
// `inner` uses Width(inner-2); its content area is inner-4, which is the column
// budget handed to the image renderer. Lines wider than that would make
// lipgloss word-wrap the image, so the budget must be exact.
func (m loginModel) viewCaptcha(inner int, maxRows int) []string {
	if m.capImg == nil {
		return []string{hintStyle.Render("（图形验证码加载中…）")}
	}
	cols := maxInt(24, inner-4)
	rendered := captchaimg.Render(m.capImg, captchaimg.Options{
		MaxCols: cols,
		MaxRows: maxRows,
		Color:   m.useColor,
	})
	if rendered == "" {
		return []string{hintStyle.Render("（验证码图片无法渲染，Ctrl+S 保存后查看）")}
	}
	box := captchaBoxStyle.Width(cols + 2).Render(rendered)
	parts := strings.Split(box, "\n")
	for i, p := range parts {
		parts[i] = padLine(p, inner)
	}
	return parts
}

// viewStatus renders the message area and the key hints.
func (m loginModel) viewStatus(inner int) []string {
	var lines []string
	switch {
	case m.busy:
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ", hintStyle.Render(m.busyText)))
	case m.errText != "":
		for _, l := range wrapText(m.errText, inner) {
			lines = append(lines, errStyle.Render(l))
		}
	case m.okText != "":
		for _, l := range wrapText(m.okText, inner) {
			lines = append(lines, okStyle.Render(l))
		}
	case m.notice != "":
		for _, l := range wrapText(m.notice, inner) {
			lines = append(lines, warnStyle.Render(l))
		}
	default:
		lines = append(lines, "")
	}
	if len(lines) > 2 {
		lines = lines[:2]
	}
	lines = append(lines, hintStyle.Render(truncate(m.viewHints(), inner)))
	for i, line := range lines {
		lines[i] = lipgloss.PlaceHorizontal(inner, lipgloss.Center, line)
	}
	return lines
}

// viewHints returns the keyboard shortcut hint line.
func (m loginModel) viewHints() string {
	if m.focus == focusTabs {
		return "←/→ 选择登录方式 · Tab/↑↓ 切换焦点 · Enter 确认"
	}
	if m.method == methodEmail {
		return "Tab/↑↓ 切换 · Enter 登录 · Ctrl+T 切换方式 · Ctrl+C 退出"
	}
	switch m.stage {
	case smsStagePhone:
		return "Enter 获取图形验证码 · Ctrl+T 切换方式 · Ctrl+C 退出"
	case smsStageCaptcha:
		return "Enter 发送短信验证码 · Ctrl+R 刷新 · Ctrl+S 保存图片 · Esc 返回"
	default:
		return "Enter 登录 · Ctrl+R 重新获取验证码 · Esc 返回 · Ctrl+C 退出"
	}
}

// ---------------------------------------------------------------------------
// validation helpers

func validateMobile(mobile string) error {
	if len(mobile) != 11 {
		return fmt.Errorf("请输入 11 位手机号")
	}
	for _, r := range mobile {
		if !unicode.IsDigit(r) {
			return fmt.Errorf("手机号只能包含数字")
		}
	}
	if mobile[0] != '1' {
		return fmt.Errorf("手机号格式不正确")
	}
	return nil
}

func validateEmail(email string) error {
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return fmt.Errorf("请输入有效的邮箱地址")
	}
	domain := email[at+1:]
	if !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("请输入有效的邮箱地址")
	}
	return nil
}

// shortID returns the first 8 characters of id for display.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
